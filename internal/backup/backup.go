// Package backup 定时给 SQLite 库导出一份快照并控制保留份数（ADR-0025）。
package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"memora/internal/store"
)

const (
	filePrefix = "memora-"
	fileSuffix = ".db"
	partSuffix = ".db.part"
	timeLayout = "20060102T150405Z"

	// 每小时查一次、满 24 小时才做一份。判断依据是"最新一份多旧"而不是
	// "开机跑了多久"：频繁重启不会多出快照，也不会把保留份数挤成几小时的窗口。
	sweepInterval    = time.Hour
	snapshotInterval = 24 * time.Hour
)

type Snapshotter struct {
	st       *store.Store
	dir      string
	keep     int
	log      *slog.Logger
	now      func() time.Time
	interval time.Duration
}

func New(st *store.Store, dir string, keep int, log *slog.Logger) *Snapshotter {
	return &Snapshotter{st: st, dir: dir, keep: keep, log: log, now: time.Now, interval: sweepInterval}
}

// Run 启动时先查一次（覆盖服务停了两天再起的场景），之后按小时循环，直到 ctx 取消。
func (b *Snapshotter) Run(ctx context.Context) {
	t := time.NewTicker(b.interval)
	defer t.Stop()

	b.maybeSnapshot(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.maybeSnapshot(ctx)
		}
	}
}

// maybeSnapshot 距最新一份快照满 24 小时才动手；目录还没有快照时立即做。
func (b *Snapshotter) maybeSnapshot(ctx context.Context) {
	if newest, ok := newestSnapshot(b.dir); ok && b.now().Sub(newest) < snapshotInterval {
		return
	}
	name, err := b.snapshot(ctx)
	if err != nil {
		// 备份失败不该把 serve 带下去；留日志等下一轮。单机部署没有别的观测面。
		b.log.Error("备份快照失败", "dir", b.dir, "err", err.Error())
		return
	}
	removed, err := prune(b.dir, b.keep)
	if err != nil {
		b.log.Error("清理旧快照失败", "dir", b.dir, "err", err.Error())
	}
	b.log.Info("备份快照完成", "file", name, "removed", removed, "keep", b.keep)
}

// snapshot 先写 .part 再改名，中途失败或进程被杀都不会留下一个看着像快照的文件。
func (b *Snapshotter) snapshot(ctx context.Context) (string, error) {
	// 0750 而不是 0700：盘外拉取的登录账号在服务运行的组里，需要能进目录读文件；
	// 组外仍然看不到。
	if err := os.MkdirAll(b.dir, 0o750); err != nil {
		return "", fmt.Errorf("backup: 建目录 %s: %w", b.dir, err)
	}
	ts := b.now().UTC().Format(timeLayout)
	part := filepath.Join(b.dir, filePrefix+ts+partSuffix)
	final := filepath.Join(b.dir, filePrefix+ts+fileSuffix)
	if err := os.Remove(part); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("backup: 清理残留 %s: %w", part, err)
	}
	if err := b.st.SnapshotTo(ctx, part); err != nil {
		_ = os.Remove(part)
		return "", err
	}
	if err := os.Rename(part, final); err != nil {
		_ = os.Remove(part)
		return "", fmt.Errorf("backup: 改名 %s: %w", final, err)
	}
	return filepath.Base(final), nil
}

// newestSnapshot 只认得出时间戳的文件名。解析不出的文件不算快照，也就永远不会被 prune 删掉。
func newestSnapshot(dir string) (time.Time, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, false
	}
	var newest time.Time
	for _, e := range entries {
		ts, ok := parseName(e.Name())
		if ok && ts.After(newest) {
			newest = ts
		}
	}
	return newest, !newest.IsZero()
}

func parseName(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, fileSuffix) {
		return time.Time{}, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(name, filePrefix), fileSuffix)
	ts, err := time.Parse(timeLayout, raw)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

// prune 删最旧的几份，顺带清掉崩溃留下的 .part——按定义它不是有效快照。
func prune(dir string, keep int) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var snaps, parts []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, filePrefix) && strings.HasSuffix(name, partSuffix) {
			parts = append(parts, name)
			continue
		}
		if _, ok := parseName(name); ok {
			snaps = append(snaps, name)
		}
	}

	removed := 0
	for _, name := range parts {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, err
		}
		removed++
	}
	// 时间戳定宽，字典序即时间序。
	sort.Strings(snaps)
	for len(snaps) > keep {
		if err := os.Remove(filepath.Join(dir, snaps[0])); err != nil {
			return removed, err
		}
		snaps = snaps[1:]
		removed++
	}
	return removed, nil
}

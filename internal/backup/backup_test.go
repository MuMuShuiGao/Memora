package backup

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"memora/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "memora.db"))
	if err != nil {
		t.Fatalf("打开库: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func newTestSnapshotter(t *testing.T, st *store.Store, dir string, keep int, now *time.Time) *Snapshotter {
	t.Helper()
	b := New(st, dir, keep, slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.now = func() time.Time { return *now }
	return b
}

func snapshotNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("读目录: %v", err)
	}
	var names []string
	for _, e := range entries {
		if _, ok := parseName(e.Name()); ok {
			names = append(names, e.Name())
		}
	}
	return names
}

// 24 小时内不重复做；超过后每天一份；保留份数到顶删最旧；不碰不相干的文件。
func TestMaybeSnapshotCadenceAndPrune(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	now := time.Date(2026, time.November, 27, 10, 0, 0, 0, time.UTC)
	b := newTestSnapshotter(t, st, dir, 2, &now)

	foreign := filepath.Join(dir, "别动我.txt")
	if err := os.WriteFile(foreign, []byte("keep"), 0o600); err != nil {
		t.Fatalf("写无关文件: %v", err)
	}

	b.maybeSnapshot(ctx) // 还没有快照：立即做一份
	names := snapshotNames(t, dir)
	if len(names) != 1 {
		t.Fatalf("首轮后快照数 = %d, 期望 1", len(names))
	}
	// 快照必须是能打开的库，而不是一个改了名的半成品。
	snapStore, err := store.Open(filepath.Join(dir, names[0]))
	if err != nil {
		t.Fatalf("打开快照 %s: %v", names[0], err)
	}
	snapStore.Close()

	now = now.Add(23 * time.Hour)
	b.maybeSnapshot(ctx) // 不满 24 小时，跳过
	if got := len(snapshotNames(t, dir)); got != 1 {
		t.Fatalf("23 小时后快照数 = %d, 期望仍是 1", got)
	}

	now = now.Add(1 * time.Hour)
	b.maybeSnapshot(ctx) // 满 24 小时，第二份
	if got := len(snapshotNames(t, dir)); got != 2 {
		t.Fatalf("48 小时点快照数 = %d, 期望 2", got)
	}

	now = now.Add(24 * time.Hour)
	b.maybeSnapshot(ctx) // 第三份，保留 2 份删最旧
	names = snapshotNames(t, dir)
	if len(names) != 2 {
		t.Fatalf("保留上限生效后快照数 = %d, 期望 2", len(names))
	}
	if strings.Contains(strings.Join(names, ","), "20261127T100000Z") {
		t.Errorf("最旧的一份应被删除, 实得 %v", names)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("无关文件被误删: %v", err)
	}
}

// 崩溃留下的 .part 会被清掉，而且不算一次已完成的快照。
func TestPruneRemovesStaleParts(t *testing.T) {
	dir := t.TempDir()
	part := filepath.Join(dir, filePrefix+"20261127T100000Z"+partSuffix)
	if err := os.WriteFile(part, []byte("half"), 0o600); err != nil {
		t.Fatalf("写 .part: %v", err)
	}
	if newest, ok := newestSnapshot(dir); ok {
		t.Fatalf(".part 不该被当成快照: %v", newest)
	}
	removed, err := prune(dir, 14)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, 期望 1", removed)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatalf(".part 没被清掉: %v", err)
	}
}

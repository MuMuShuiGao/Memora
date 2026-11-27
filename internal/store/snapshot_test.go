package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// 快照必须能被独立打开且内容与源库一致——WAL 下直接复制库文件做不到这一点
// （最近的提交可能还在 -wal 里），所以这里断言的是 VACUUM INTO 的语义（ADR-0025）。
func TestSnapshotToIsOpenableAndComplete(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.November, 27, 9, 0, 0, 0, time.UTC)
	if _, err := s.CreateNote(ctx, "备份这条", now, "2026-11-23", "2026-11-01"); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if _, err := s.CreateTask(ctx, "备份那条", "2026-11-27", now); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	snap := filepath.Join(t.TempDir(), "snap.db")
	if err := s.SnapshotTo(ctx, snap); err != nil {
		t.Fatalf("SnapshotTo: %v", err)
	}

	copied, err := Open(snap)
	if err != nil {
		t.Fatalf("打开快照: %v", err)
	}
	defer copied.Close()

	n, err := copied.CountNotes(ctx)
	if err != nil || n != 1 {
		t.Fatalf("快照里 notes = %d, err=%v, 期望 1", n, err)
	}
	tasks, err := copied.ListTasks(ctx)
	if err != nil || len(tasks) != 1 || tasks[0].Body != "备份那条" {
		t.Fatalf("快照里 tasks = %+v, err=%v", tasks, err)
	}
}

package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestReportLifecycle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)

	// 从没生成过：必须是 ErrNotFound，而不是"有一行空正文"。
	if _, err := s.GetReport(ctx, "week", "2026-11-23"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("初始 GetReport = %v, 期望 ErrNotFound", err)
	}

	run, err := s.BeginRun(ctx, "week", "2026-11-23", "2026-11-29", now)
	if err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if run.Status != StatusRunning {
		t.Errorf("状态 = %s, 期望 running", run.Status)
	}
	if run.Body != nil {
		t.Errorf("生成中不该有正文: %v", *run.Body)
	}

	ready, err := s.FinishRun(ctx, "week", "2026-11-23", StatusReady, "本周做了三件事", 3, "", now, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("FinishRun ready: %v", err)
	}
	if ready.Status != StatusReady || *ready.Body != "本周做了三件事" || *ready.SourceCount != 3 {
		t.Errorf("收尾结果不对: %+v", ready)
	}
	if ready.FailReason != nil {
		t.Errorf("成功之后不该留着失败原因: %v", *ready.FailReason)
	}

	// 重跑同一周期：还是那一行，状态从 running 起走（ADR-0004 的一份当前报表）。
	if _, err := s.BeginRun(ctx, "week", "2026-11-23", "2026-11-29", now.Add(time.Hour)); err != nil {
		t.Fatalf("二次 BeginRun: %v", err)
	}
	all, err := s.ListReports(ctx, "week", 10)
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("重跑后应有 1 行, 实得 %d", len(all))
	}
	if all[0].Status != StatusRunning {
		t.Errorf("重跑应把状态打回 running, 实得 %s", all[0].Status)
	}

	// 失败收尾：正文清空，原因留下。
	failed, err := s.FinishRun(ctx, "week", "2026-11-23", StatusFailed, "", 0, "模型接口 HTTP 500", now, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("FinishRun failed: %v", err)
	}
	if failed.Status != StatusFailed || failed.Body != nil || failed.SourceCount != nil {
		t.Errorf("失败态不该有正文或条数: %+v", failed)
	}
	if failed.FailReason == nil || *failed.FailReason != "模型接口 HTTP 500" {
		t.Errorf("失败原因没落库: %+v", failed.FailReason)
	}

	if _, err := s.FinishRun(ctx, "week", "2026-11-23", StatusPending, "", 0, "", now, now); err == nil {
		t.Error("收尾状态只认 ready/failed, 给 pending 应该拒绝")
	}
}

func TestListReportsKindAndOrder(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)

	weeks := []string{"2026-10-05", "2026-10-12", "2026-10-19"}
	for i, w := range weeks {
		if _, err := s.BeginRun(ctx, "week", w, w, base.AddDate(0, 0, i)); err != nil {
			t.Fatalf("BeginRun %s: %v", w, err)
		}
	}
	if _, err := s.BeginRun(ctx, "month", "2026-10-01", "2026-10-31", base); err != nil {
		t.Fatalf("BeginRun month: %v", err)
	}

	got, err := s.ListReports(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("两种 kind 共 4 行, 实得 %d", len(got))
	}
	if got[0].PeriodStart != "2026-10-19" {
		t.Errorf("列表应按起始日倒序, 首行 = %s", got[0].PeriodStart)
	}

	only, err := s.ListReports(ctx, "month", 10)
	if err != nil {
		t.Fatalf("ListReports month: %v", err)
	}
	if len(only) != 1 || only[0].Kind != "month" {
		t.Errorf("kind 过滤没生效: %+v", only)
	}

	limited, err := s.ListReports(ctx, "week", 2)
	if err != nil {
		t.Fatalf("ListReports limit: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("limit 没生效: %d 行", len(limited))
	}
}

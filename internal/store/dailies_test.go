package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDailyUpsertOneRowPerDay(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 25, 9, 0, 0, 0, time.UTC)

	first, err := s.UpsertDaily(ctx, "2026-11-25", "优先级甲", "做成了甲", "卡在甲", base)
	if err != nil {
		t.Fatalf("UpsertDaily 新建: %v", err)
	}
	if first.EntryDay != "2026-11-25" || first.Priorities != "优先级甲" || first.Done != "做成了甲" || first.Risk != "卡在甲" {
		t.Errorf("新建日报 = %+v", first)
	}

	// 同一天再写是覆盖，不是第二条（ADR-0031）；id 与 created_at 保持原样。
	second, err := s.UpsertDaily(ctx, "2026-11-25", "优先级乙", "", "", base.Add(time.Hour))
	if err != nil {
		t.Fatalf("UpsertDaily 覆盖: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("同一天写了两条: id %d -> %d", first.ID, second.ID)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) || !second.UpdatedAt.After(first.UpdatedAt) {
		t.Errorf("覆盖后时刻不对: first=%+v second=%+v", first, second)
	}
	if second.Priorities != "优先级乙" || second.Done != "" || second.Risk != "" {
		t.Errorf("覆盖应整条换三节: %+v", second)
	}

	if _, err := s.GetDaily(ctx, "2026-11-24"); !errors.Is(err, ErrNotFound) {
		t.Errorf("没写过的一天应回 ErrNotFound, 实得 %v", err)
	}
}

func TestDailiesBetweenBoundsAndOrder(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 25, 9, 0, 0, 0, time.UTC)

	for _, day := range []string{"2026-11-22", "2026-11-23", "2026-11-29"} {
		if _, err := s.UpsertDaily(ctx, day, "写点东西", "", "", base); err != nil {
			t.Fatalf("UpsertDaily %s: %v", day, err)
		}
	}
	// 区间内的全空行不算素材，不该出现在结果里。
	if _, err := s.UpsertDaily(ctx, "2026-11-25", "", "", "", base); err != nil {
		t.Fatalf("UpsertDaily 空: %v", err)
	}
	if _, err := s.UpsertDaily(ctx, "2026-11-30", "下一周", "", "", base); err != nil {
		t.Fatalf("UpsertDaily 下周一: %v", err)
	}

	// [周一, 下周一) 左闭右开：周日的日报在内，下周一的在外面。
	got, err := s.DailiesBetween(ctx, "2026-11-23", "2026-11-30")
	if err != nil {
		t.Fatalf("DailiesBetween: %v", err)
	}
	if len(got) != 2 || got[0].EntryDay != "2026-11-23" || got[1].EntryDay != "2026-11-29" {
		t.Errorf("区间与顺序 = %+v, 期望 11-23 / 11-29 升序", got)
	}
}

func TestDistinctDailyDaysSkipsAllEmpty(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 25, 9, 0, 0, 0, time.UTC)

	if _, err := s.UpsertDaily(ctx, "2026-11-23", "有优先级", "", "", base); err != nil {
		t.Fatalf("UpsertDaily: %v", err)
	}
	// 三节全空表示这天没有素材，不参与补跑候选（ADR-0031）。
	if _, err := s.UpsertDaily(ctx, "2026-11-24", "", "", "", base); err != nil {
		t.Fatalf("UpsertDaily 空: %v", err)
	}

	days, err := s.DistinctDailyDays(ctx)
	if err != nil {
		t.Fatalf("DistinctDailyDays: %v", err)
	}
	if len(days) != 1 || days[0] != "2026-11-23" {
		t.Errorf("候选日 = %v, 期望只有 11-23", days)
	}
}

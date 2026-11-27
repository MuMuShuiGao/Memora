package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStageLifecycleAndTaskCascade(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "写方案", "买牛奶")

	first, err := s.CreateStage(ctx, ids[0], "调研现状", base)
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	second, err := s.CreateStage(ctx, ids[0], "写初稿", base.Add(time.Minute))
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	if first.Done || first.Takeaway != "" || first.TaskID != ids[0] {
		t.Errorf("新阶段 = %+v, 期望未完成、空心得的阶段", first)
	}

	// 列表和单条都按加入顺序带出阶段，没挂阶段的那条是空数组而不是 null。
	list, err := s.ListTasks(ctx)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	for _, task := range list {
		if task.ID == ids[0] {
			if len(task.Stages) != 2 || task.Stages[0].ID != first.ID || task.Stages[1].ID != second.ID {
				t.Errorf("待办的阶段 = %+v, 期望按加入顺序两条", task.Stages)
			}
		}
		if task.ID == ids[1] && task.Stages == nil {
			t.Error("没挂阶段的待办也应返回空数组")
		}
	}
	got, err := s.GetTask(ctx, ids[0])
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.Stages) != 2 {
		t.Errorf("GetTask 的阶段 = %+v, 期望两条", got.Stages)
	}

	// 删待办时阶段同事务删掉，不留孤儿。
	if err := s.DeleteTask(ctx, ids[0], base.Add(time.Hour)); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if _, err := s.GetStage(ctx, first.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("父任务删掉后阶段还在: %v", err)
	}
}

func TestStageLimitFive(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "长活")

	for i := 0; i < maxStagesPerTask; i++ {
		if _, err := s.CreateStage(ctx, ids[0], "阶段", base); err != nil {
			t.Fatalf("第 %d 个阶段: %v", i+1, err)
		}
	}
	if _, err := s.CreateStage(ctx, ids[0], "第六个", base); !errors.Is(err, ErrTooManyStages) {
		t.Errorf("第六个阶段 = %v, 期望 ErrTooManyStages", err)
	}
	if _, err := s.CreateStage(ctx, 9999, "不存在", base); !errors.Is(err, ErrNotFound) {
		t.Errorf("挂在未知待办上 = %v, 期望 ErrNotFound", err)
	}
}

func TestSetStageDoneStampsAndKeepsTakeaway(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "长活")
	stage, err := s.CreateStage(ctx, ids[0], "调研", base)
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}

	if _, err := s.UpdateStageTakeaway(ctx, stage.ID, "外部依赖下周才给", base.Add(time.Minute)); err != nil {
		t.Fatalf("UpdateStageTakeaway: %v", err)
	}
	done, err := s.SetStageDone(ctx, stage.ID, true, base.Add(2*time.Hour), "2026-11-23", "2026-11-01")
	if err != nil {
		t.Fatalf("SetStageDone: %v", err)
	}
	if !done.Done || done.DoneAt == nil || done.DoneWeekStart != "2026-11-23" || done.DoneMonthStart != "2026-11-01" {
		t.Errorf("勾掉后 = %+v", done)
	}

	// 重复勾不改归属列，免得一次网络重放把报表归属挪走。
	again, err := s.SetStageDone(ctx, stage.ID, true, base.Add(3*time.Hour), "2026-11-30", "2026-12-01")
	if err != nil {
		t.Fatalf("重复勾: %v", err)
	}
	if again.DoneWeekStart != "2026-11-23" {
		t.Errorf("重放改了归属周: %q", again.DoneWeekStart)
	}

	// 取消勾选清归属、保留心得（与待办同规）。
	back, err := s.SetStageDone(ctx, stage.ID, false, base.Add(4*time.Hour), noPeriod, noPeriod)
	if err != nil {
		t.Fatalf("取消勾选: %v", err)
	}
	if back.Done || back.DoneAt != nil || back.DoneWeekStart != "" || back.DoneMonthStart != "" {
		t.Errorf("取消后 = %+v", back)
	}
	if back.Takeaway != "外部依赖下周才给" {
		t.Errorf("取消勾选把心得清掉了: %q", back.Takeaway)
	}

	if err := s.DeleteStage(ctx, stage.ID); err != nil {
		t.Fatalf("DeleteStage: %v", err)
	}
	if err := s.DeleteStage(ctx, stage.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复删除 = %v, 期望 ErrNotFound", err)
	}
}

func TestStagesDoneFeedReportAndBackfill(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "交方案", "买牛奶")

	weekly, err := s.CreateStage(ctx, ids[0], "调研", base)
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	lastWeek, err := s.CreateStage(ctx, ids[0], "写初稿", base.Add(time.Minute))
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	open, err := s.CreateStage(ctx, ids[0], "评审", base.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	if _, err := s.SetStageDone(ctx, weekly.ID, true, base.Add(time.Hour), "2026-11-23", "2026-11-01"); err != nil {
		t.Fatalf("勾本周阶段: %v", err)
	}
	if _, err := s.SetStageDone(ctx, lastWeek.ID, true, base.Add(2*time.Hour), "2026-11-16", "2026-11-01"); err != nil {
		t.Fatalf("勾上周阶段: %v", err)
	}
	if _, err := s.UpdateStageTakeaway(ctx, weekly.ID, "三份访谈整理完", base.Add(time.Hour)); err != nil {
		t.Fatalf("写心得: %v", err)
	}

	week, err := s.StagesDoneForWeek(ctx, "2026-11-23")
	if err != nil {
		t.Fatalf("StagesDoneForWeek: %v", err)
	}
	if len(week) != 1 || week[0].Body != "调研" || week[0].TaskBody != "交方案" || week[0].Takeaway != "三份访谈整理完" {
		t.Errorf("本周阶段素材 = %+v, 期望带上父任务正文与心得", week)
	}
	if week[0].DoneAt == nil {
		t.Error("阶段素材缺 done_at，提示词没法编日期")
	}
	month, err := s.StagesDoneForMonth(ctx, "2026-11-01")
	if err != nil {
		t.Fatalf("StagesDoneForMonth: %v", err)
	}
	if len(month) != 2 || month[0].Body != "调研" || month[1].Body != "写初稿" {
		t.Errorf("本月阶段素材 = %+v, 期望按勾掉顺序两条", month)
	}

	weeks, err := s.DistinctStageDoneWeekStarts(ctx)
	if err != nil {
		t.Fatalf("DistinctStageDoneWeekStarts: %v", err)
	}
	if len(weeks) != 2 || weeks[0] != "2026-11-23" || weeks[1] != "2026-11-16" {
		t.Errorf("阶段候选周 = %v", weeks)
	}
	months, err := s.DistinctStageDoneMonthStarts(ctx)
	if err != nil {
		t.Fatalf("DistinctStageDoneMonthStarts: %v", err)
	}
	if len(months) != 1 || months[0] != "2026-11-01" {
		t.Errorf("阶段候选月 = %v", months)
	}

	// 没勾掉的阶段不进任何取数——它不代表任何进展（FR-11 同理）。
	for _, got := range [][]DoneStage{week, month} {
		for _, st := range got {
			if st.ID == open.ID {
				t.Errorf("未勾的阶段混进了报表取数: %+v", got)
			}
		}
	}
}

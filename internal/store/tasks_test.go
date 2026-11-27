package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// seedTasks 按给定顺序建好待办，返回的 id 下标就是 rank 顺序。CreateTask 是插到队首，
// 所以建完还要按提交顺序整体重排一次——顺带也把"重排"这条路在别的测试之前先跑通了。
// noPeriod 是"这条此刻没有归属周期"的写法。归属列在存储层没有语义——它既不校验
// 格式也不自己算日期，那是 period 包和调用方的事（design §2）。
const noPeriod = ""

func seedTasks(t *testing.T, s *Store, ctx context.Context, bodies ...string) []int64 {
	t.Helper()
	var ids []int64
	for _, b := range bodies {
		task, err := s.CreateTask(ctx, b, "2026-11-24", time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("CreateTask %q: %v", b, err)
		}
		ids = append(ids, task.ID)
	}
	if _, err := s.ResequenceTasks(ctx, ids, time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Resequence 初始顺序: %v", err)
	}
	return ids
}

func openRanks(t *testing.T, s *Store, ctx context.Context) map[string]int {
	t.Helper()
	list, err := s.ListTasks(ctx)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	out := map[string]int{}
	for _, task := range list {
		if task.Done {
			continue
		}
		out[task.Body] = task.Rank
	}
	return out
}

func TestCreateTaskGoesToHead(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)

	first, err := s.CreateTask(ctx, "写周报", "2026-11-24", now)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if first.Rank != 1 || first.Done {
		t.Errorf("首条 = rank %d done=%v, 期望 1/false", first.Rank, first.Done)
	}
	if first.DoneAt != nil {
		t.Errorf("未勾掉的不该有 done_at: %v", *first.DoneAt)
	}

	second, err := s.CreateTask(ctx, "约评审", "2026-11-25", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// 新建排到队首，原来那条顺延：ADR-0014 的连带决定。
	if second.Rank != 1 {
		t.Errorf("新建 rank = %d, 期望 1", second.Rank)
	}
	ranks := openRanks(t, s, ctx)
	if ranks["约评审"] != 1 || ranks["写周报"] != 2 {
		t.Errorf("顺延后的顺序 = %v, 期望 约评审=1 写周报=2", ranks)
	}
}

func TestCompleteTaskWritesDoneAtAndTightensRanks(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "甲", "乙", "丙")

	doneAt := base.Add(2 * time.Hour)
	got, err := s.SetTaskDone(ctx, ids[0], true, doneAt, "2026-11-23", "2026-11-01")
	if err != nil {
		t.Fatalf("SetTaskDone: %v", err)
	}
	if !got.Done || got.DoneAt == nil || !got.DoneAt.Equal(doneAt) {
		t.Fatalf("勾掉后 = done=%v done_at=%v, 期望 true/%s", got.Done, got.DoneAt, doneAt)
	}
	if got.DoneWeekStart != "2026-11-23" || got.DoneMonthStart != "2026-11-01" {
		t.Errorf("归属列没存进去: %q / %q", got.DoneWeekStart, got.DoneMonthStart)
	}

	// 勾掉的那条离开未完成序列，后面的名次自动收紧——"前 5" 靠的就是这件事。
	ranks := openRanks(t, s, ctx)
	if len(ranks) != 2 || ranks["乙"] != 1 || ranks["丙"] != 2 {
		t.Errorf("未完成序列 = %v, 期望 乙=1 丙=2", ranks)
	}

	list, err := s.ListTasks(ctx)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(list) != 3 || !list[2].Done || list[2].Body != "甲" {
		t.Errorf("列表尾部应是已完成的甲，实际 %v", list)
	}
	for _, task := range list[:2] {
		if task.Done {
			t.Errorf("已完成的排到了未完成前面: %v", task.Body)
		}
	}
}

func TestUncompleteGoesToTailAndClearsDoneAt(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "甲", "乙", "丙")

	if _, err := s.SetTaskDone(ctx, ids[0], true, base.Add(time.Hour), "2026-11-23", "2026-11-01"); err != nil {
		t.Fatalf("勾掉: %v", err)
	}
	back, err := s.SetTaskDone(ctx, ids[0], false, base.Add(3*time.Hour), noPeriod, noPeriod)
	if err != nil {
		t.Fatalf("取消勾选: %v", err)
	}
	// done_at 与归属两列必须一起清空：留着上一次的完成时刻，报表会把这条当成
	// "已完成且属于那一期"，而它此刻明明还挂在当前清单上。
	if back.Done || back.DoneAt != nil || back.DoneWeekStart != "" || back.DoneMonthStart != "" {
		t.Fatalf("取消后 = done=%v done_at=%v week=%q month=%q",
			back.Done, back.DoneAt, back.DoneWeekStart, back.DoneMonthStart)
	}
	ranks := openRanks(t, s, ctx)
	if ranks["甲"] != 3 {
		t.Errorf("取消勾选后 rank = %v, 期望甲排到队尾", ranks)
	}
}

func TestSetTaskDoneIdempotent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "甲", "乙")

	if _, err := s.SetTaskDone(ctx, ids[0], true, base, "2026-11-23", "2026-11-01"); err != nil {
		t.Fatalf("首次勾掉: %v", err)
	}
	// 重复勾同一条不该改写 done_at 和归属列——否则一次网络重放就把报表归属挪走了。
	again, err := s.SetTaskDone(ctx, ids[0], true, base.Add(time.Hour), "2026-11-30", "2026-12-01")
	if err != nil {
		t.Fatalf("重复勾掉: %v", err)
	}
	if again.DoneAt == nil || !again.DoneAt.Equal(base) {
		t.Errorf("done_at = %v, 期望保持 %s", again.DoneAt, base)
	}
	if again.DoneWeekStart != "2026-11-23" {
		t.Errorf("归属周被重放改写了: %q", again.DoneWeekStart)
	}

	if _, err := s.SetTaskDone(ctx, 9999, true, base, "2026-11-23", "2026-11-01"); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在的 id = %v, 期望 ErrNotFound", err)
	}
}

func TestResequenceRejectsStaleOrder(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "甲", "乙", "丙")

	if _, err := s.ResequenceTasks(ctx, []int64{ids[2], ids[0], ids[1]}, now); err != nil {
		t.Fatalf("正常重排: %v", err)
	}
	ranks := openRanks(t, s, ctx)
	if ranks["丙"] != 1 || ranks["甲"] != 2 || ranks["乙"] != 3 {
		t.Errorf("重排结果 = %v, 期望 丙甲乙", ranks)
	}

	before := map[string]int{"甲": 2, "乙": 3, "丙": 1}
	cases := map[string][]int64{
		"漏一条":      {ids[0], ids[1]},
		"重复一个":     {ids[0], ids[0], ids[0]},
		"掺一个未知 id": {ids[0], ids[1], 424242},
	}
	for name, submit := range cases {
		if _, err := s.ResequenceTasks(ctx, submit, now); !errors.Is(err, ErrBadOrder) {
			t.Errorf("%s: err = %v, 期望 ErrBadOrder", name, err)
		}
	}
	// 被拒的三次必须一点没生效，否则"拒绝"只是报错不回滚。
	got := openRanks(t, s, ctx)
	for body, want := range before {
		if got[body] != want {
			t.Errorf("拒绝后 %s 的 rank = %d, 期望仍是 %d", body, got[body], want)
		}
	}
}

func TestResequenceIgnoresDoneTasks(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "甲", "乙", "丙")
	if _, err := s.SetTaskDone(ctx, ids[2], true, now, "2026-11-23", "2026-11-01"); err != nil {
		t.Fatalf("勾掉: %v", err)
	}

	// 已完成的不在未完成集合里，把它塞进 ids 应当整笔拒绝而不是静默收下。
	if _, err := s.ResequenceTasks(ctx, []int64{ids[1], ids[0], ids[2]}, now); !errors.Is(err, ErrBadOrder) {
		t.Fatalf("提交含已完成项 = %v, 期望 ErrBadOrder", err)
	}
	if _, err := s.ResequenceTasks(ctx, []int64{ids[1], ids[0]}, now); err != nil {
		t.Fatalf("只提交未完成的两条: %v", err)
	}
	ranks := openRanks(t, s, ctx)
	if ranks["乙"] != 1 || ranks["甲"] != 2 {
		t.Errorf("重排结果 = %v, 期望 乙甲", ranks)
	}
}

func TestDeleteTaskTightensOpenRanks(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "甲", "乙", "丙")

	if err := s.DeleteTask(ctx, ids[0], now); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	ranks := openRanks(t, s, ctx)
	if len(ranks) != 2 || ranks["乙"] != 1 || ranks["丙"] != 2 {
		t.Errorf("删除后 = %v, 期望 乙=1 丙=2 且不留空洞", ranks)
	}
	if err := s.DeleteTask(ctx, ids[0], now); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复删除 = %v, 期望 ErrNotFound", err)
	}
}

func TestTasksDoneForWeekUsesStampedColumns(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "本周做的", "上周做的", "记着没做")

	// 归属来自勾掉那一刻盖上的列，跟 created_at 无关：本周做完的就归本周，
	// 哪怕它是上周记下的（需求 §4 完成时刻、FR-11）。
	if _, err := s.SetTaskDone(ctx, ids[0], true, base, "2026-11-23", "2026-11-01"); err != nil {
		t.Fatalf("勾掉本周那条: %v", err)
	}
	if _, err := s.SetTaskDone(ctx, ids[1], true, base.Add(time.Hour), "2026-11-16", "2026-11-01"); err != nil {
		t.Fatalf("勾掉上周那条: %v", err)
	}

	week, err := s.TasksDoneForWeek(ctx, "2026-11-23")
	if err != nil {
		t.Fatalf("TasksDoneForWeek: %v", err)
	}
	if len(week) != 1 || week[0].Body != "本周做的" {
		t.Errorf("本周待办 = %v, 期望只有『本周做的』", week)
	}

	month, err := s.TasksDoneForMonth(ctx, "2026-11-01")
	if err != nil {
		t.Fatalf("TasksDoneForMonth: %v", err)
	}
	// 同一月里的两周都算月报素材，按完成时刻升序。
	if len(month) != 2 || month[0].Body != "本周做的" || month[1].Body != "上周做的" {
		t.Errorf("本月待办 = %v, 期望按完成时刻排的两条", month)
	}

	// 没勾掉的那条任何时候都不该出现在取数里——它不代表交付（FR-11）。
	for _, got := range [][]Task{week, month} {
		for _, task := range got {
			if task.Body == "记着没做" {
				t.Errorf("未完成的待办混进了报表取数: %v", got)
			}
		}
	}

	// 取消勾选之后归属列清空，这条从两个周期里一起消失。
	if _, err := s.SetTaskDone(ctx, ids[0], false, base.Add(2*time.Hour), noPeriod, noPeriod); err != nil {
		t.Fatalf("取消勾选: %v", err)
	}
	if again, err := s.TasksDoneForWeek(ctx, "2026-11-23"); err != nil || len(again) != 0 {
		t.Errorf("取消后本周仍有 %v 条 (err %v), 期望 0", again, err)
	}
}

func TestDistinctDoneStartsFeedsBackfillCandidates(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "甲", "乙", "丙")
	if _, err := s.SetTaskDone(ctx, ids[0], true, base, "2026-11-23", "2026-11-01"); err != nil {
		t.Fatalf("勾掉: %v", err)
	}
	if _, err := s.SetTaskDone(ctx, ids[1], true, base, "2026-11-16", "2026-11-01"); err != nil {
		t.Fatalf("勾掉: %v", err)
	}

	weeks, err := s.DistinctDoneWeekStarts(ctx)
	if err != nil {
		t.Fatalf("DistinctDoneWeekStarts: %v", err)
	}
	// 补跑候选按由新到旧排，一轮上限留的是最近的几个（design §7）。
	if len(weeks) != 2 || weeks[0] != "2026-11-23" || weeks[1] != "2026-11-16" {
		t.Errorf("周候选 = %v, 期望 [2026-11-23 2026-11-16]", weeks)
	}
	months, err := s.DistinctDoneMonthStarts(ctx)
	if err != nil {
		t.Fatalf("DistinctDoneMonthStarts: %v", err)
	}
	if len(months) != 1 || months[0] != "2026-11-01" {
		t.Errorf("月候选 = %v, 期望 [2026-11-01]", months)
	}
}

func TestTasksUpdateBodyKeepsRank(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "甲", "乙")

	got, err := s.UpdateTaskBody(ctx, ids[1], "乙改过的正文", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("UpdateTaskBody: %v", err)
	}
	if got.Rank != 2 || got.Body != "乙改过的正文" {
		t.Errorf("改完 = rank %d body %q, 期望 rank 2 正文已改", got.Rank, got.Body)
	}
	if _, err := s.UpdateTaskBody(ctx, 9999, "不存在", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("未知 id = %v, 期望 ErrNotFound", err)
	}
}

func TestUpdateTaskDueOnKeepsRank(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
	ids := seedTasks(t, s, ctx, "甲", "乙")

	got, err := s.UpdateTaskDueOn(ctx, ids[1], "2026-12-01", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("UpdateTaskDueOn: %v", err)
	}
	if got.DueOn != "2026-12-01" || got.Rank != 2 || got.Done {
		t.Errorf("改日期后 = due_on %q rank %d done %v, 期望 2026-12-01/2/false", got.DueOn, got.Rank, got.Done)
	}
	if _, err := s.UpdateTaskDueOn(ctx, 9999, "2026-12-01", now); !errors.Is(err, ErrNotFound) {
		t.Errorf("未知 id = %v, 期望 ErrNotFound", err)
	}
}

func TestBackfillTaskDueOnUsesLocalDay(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	plus8 := time.FixedZone("UTC+8", 8*3600)
	// UTC 11-24 23:30 在 UTC+8 已经是 11-25，回填要取本地那一天。
	at := time.Date(2026, time.November, 24, 23, 30, 0, 0, time.UTC)
	task, err := s.CreateTask(ctx, "历史待办", "2026-11-24", at)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE tasks SET due_on = '' WHERE id = ?", task.ID); err != nil {
		t.Fatalf("抹掉日期: %v", err)
	}

	n, err := s.BackfillTaskDueOn(ctx, plus8)
	if err != nil {
		t.Fatalf("BackfillTaskDueOn: %v", err)
	}
	if n != 1 {
		t.Errorf("回填 %d 条, 期望 1", n)
	}
	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.DueOn != "2026-11-25" {
		t.Errorf("回填后的日期 = %q, 期望 2026-11-25（UTC+8 的本地日）", got.DueOn)
	}
	if again, err := s.BackfillTaskDueOn(ctx, plus8); err != nil || again != 0 {
		t.Errorf("第二次回填 = %d, %v；期望 0, nil（幂等）", again, err)
	}
}

// 升级路径：ADR-0021 之前建的库没有 due_on 列，Open 要能补列，回填按创建时刻的本地日。
func TestOpenAddsDueOnToLegacyTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open(driverName, "file:"+path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	const stamp = "2026-11-24T23:30:00Z" // UTC+8 已是 11-25
	if _, err := raw.Exec(`CREATE TABLE tasks (
		id INTEGER PRIMARY KEY,
		body TEXT NOT NULL,
		done INTEGER NOT NULL DEFAULT 0,
		rank INTEGER NOT NULL,
		done_at TEXT,
		done_week_start TEXT,
		done_month_start TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("建旧表: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO tasks (body, done, rank, created_at, updated_at) VALUES ('旧行', 0, 1, ?, ?)`, stamp, stamp); err != nil {
		t.Fatalf("插旧行: %v", err)
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open 旧库: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	list, err := s.ListTasks(ctx)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(list) != 1 || list[0].DueOn != "" {
		t.Fatalf("旧库读完 = %+v, 期望一条且 due_on 为空", list)
	}

	plus8 := time.FixedZone("UTC+8", 8*3600)
	if n, err := s.BackfillTaskDueOn(ctx, plus8); err != nil || n != 1 {
		t.Fatalf("回填 = %d, %v；期望 1, nil", n, err)
	}
	got, err := s.GetTask(ctx, list[0].ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.DueOn != "2026-11-25" {
		t.Errorf("旧行回填 = %q, 期望 2026-11-25", got.DueOn)
	}
}

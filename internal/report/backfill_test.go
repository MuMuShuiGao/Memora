package report

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"memora/internal/store"
)

func newTestScheduler(g *Generator, st *store.Store) *Scheduler {
	return NewScheduler(g, st, plus8, slog.New(slog.NewTextHandler(discard{}, nil)))
}

// 补跑判据是"这个已结束周期有没有 ready 报表"，不是"现在是不是周一早上八点"。
// 所以候选要同时满足两条：周期已经过去，且库里那条不是 ready。
//
// 周报和月报分开测：任何一条记录同时落进一个周和一个周月，混在一起种会让
// 候选列表掺进别的周期，看不出判据到底作用在哪一个上。
func TestMissingWeeksOnlyFinishedAndNotReady(t *testing.T) {
	// 2026-11-25 是周三：本周还没结束，上一周已结束。
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)
	ctx := context.Background()

	seed(t, st, "上周的事", now.AddDate(0, 0, -8)) // 归属 2026-11-16，已结束
	seed(t, st, "本周的事", now)                   // 归属 2026-11-23，未结束

	sched := newTestScheduler(g, st)
	got, err := sched.missingPeriods(ctx, Week, now)
	if err != nil {
		t.Fatalf("missingPeriods week: %v", err)
	}
	if len(got) != 1 || got[0] != "2026-11-16" {
		t.Fatalf("周报候选 = %v, 期望只有 [2026-11-16]", got)
	}

	// 补一份 ready 之后，同一个周期不该再出现在候选里。
	if _, err := g.Generate(ctx, Week, "2026-11-16"); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	got, err = sched.missingPeriods(ctx, Week, now)
	if err != nil {
		t.Fatalf("missingPeriods 复查: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("已有 ready 报表还列进候选 = %v, 期望空", got)
	}
}

func TestMissingMonthsOnlyFinishedAndNotReady(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)
	ctx := context.Background()

	seed(t, st, "上个月的事", time.Date(2026, time.October, 15, 9, 0, 0, 0, plus8)) // 已结束
	seed(t, st, "本月的事", time.Date(2026, time.November, 15, 9, 0, 0, 0, plus8)) // 未结束

	got, err := newTestScheduler(g, st).missingPeriods(ctx, Month, now)
	if err != nil {
		t.Fatalf("missingPeriods month: %v", err)
	}
	if len(got) != 1 || got[0] != "2026-10-01" {
		t.Errorf("月报候选 = %v, 期望只有 [2026-10-01]", got)
	}

	if _, err := g.Generate(ctx, Month, "2026-10-01"); err != nil {
		t.Fatalf("Generate month: %v", err)
	}
	got, err = newTestScheduler(g, st).missingPeriods(ctx, Month, now)
	if err != nil {
		t.Fatalf("missingPeriods month 复查: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("月报补好后仍有候选 = %v", got)
	}
}

// 失败的那一份下次扫描要能被重新捞起来——这是 status 列存在的理由（design §1）。
func TestFailedReportIsRetriedBySweep(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	boom := errors.New("模型限流")
	llm := &fakeCompleter{errs: []error{boom, boom}}
	g, st := newTestGenerator(t, llm, &now)
	sched := newTestScheduler(g, st)
	ctx := context.Background()

	seed(t, st, "上周记的", now.AddDate(0, 0, -8))

	if res := sched.sweep(ctx); res.failed != 1 {
		t.Fatalf("首轮扫描账目 = %+v, 期望 failed 1", res)
	}
	rep, err := st.GetReport(ctx, "week", "2026-11-16")
	if err != nil {
		t.Fatalf("失败也要有行: %v", err)
	}
	if rep.Status != store.StatusFailed {
		t.Errorf("状态 = %s, 期望 failed", rep.Status)
	}

	// 模型恢复后再扫一次，同一周期应当补成 ready。
	llm.errs = nil
	if res := sched.sweep(ctx); res.done != 1 {
		t.Fatalf("二轮扫描账目 = %+v, 期望 done 1", res)
	}
	rep, err = st.GetReport(ctx, "week", "2026-11-16")
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	if rep.Status != store.StatusReady || rep.FailReason != nil {
		t.Errorf("补跑没把失败态治好: %+v", rep)
	}

	// 第三轮无事可做，也不该再打模型。
	before := llm.calls
	if res := sched.sweep(ctx); res.done+res.failed+res.skipped != 0 {
		t.Errorf("三轮扫描不该有动作: %+v", res)
	}
	if llm.calls != before {
		t.Errorf("已 ready 的周期被重复生成了")
	}
}

// 卡在 running 的行（进程被杀在生成中途）必须能被发现，否则 FR-05 就是空话。
func TestStaleRunningIsPickedUp(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)
	ctx := context.Background()

	seed(t, st, "上周记的", now.AddDate(0, 0, -8))
	if _, err := st.BeginRun(ctx, "week", "2026-11-16", "2026-11-22", now); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}

	// 模拟上一次进程死在这里：库里留着 running。
	got, err := newTestScheduler(g, st).missingPeriods(ctx, Week, now)
	if err != nil {
		t.Fatalf("missingPeriods: %v", err)
	}
	if len(got) != 1 || got[0] != "2026-11-16" {
		t.Fatalf("running 的行应被重新捞起, 实得 %v", got)
	}

	if res := newTestScheduler(g, st).sweep(ctx); res.done != 1 {
		t.Errorf("sweep 应补上这一份, 实得 %+v", res)
	}
}

// 关机几个月后起来，候选会有几十周。一轮只补最近若干份，其余留给下一轮，
// 否则服务重启的头几分钟全花在排队打模型上。
func TestSweepCapsPerRound(t *testing.T) {
	now := time.Date(2027, time.January, 4, 10, 0, 0, 0, plus8) // 周一上午
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)
	ctx := context.Background()

	// 造 9 个已结束周期：从 2026-10 月起每周一条记录。
	for i := range 9 {
		at := now.AddDate(0, 0, -7*(i+1))
		seed(t, st, "某周的事", at)
	}

	sched := newTestScheduler(g, st)
	ids, err := sched.missingPeriods(ctx, Week, now)
	if err != nil {
		t.Fatalf("missingPeriods: %v", err)
	}
	if len(ids) != 9 {
		t.Fatalf("候选应有 9 个已结束周期, 实得 %d: %v", len(ids), ids)
	}
	if ids[0] < ids[len(ids)-1] {
		t.Errorf("候选应由新到旧排, 实得 %v", ids)
	}

	res := sched.sweep(ctx)
	if res.done == 0 {
		t.Fatal("一轮下来什么都没补")
	}
	// 上限是按种类各算的：周报这轮应该只补了最近的 maxPerSweep 份。
	var readyWeeks int
	for _, id := range ids {
		if r, err := st.GetReport(ctx, "week", id); err == nil && r.Status == store.StatusReady {
			readyWeeks++
		}
	}
	if readyWeeks != maxPerSweep {
		t.Errorf("一轮补了 %d 份周报, 期望上限 %d", readyWeeks, maxPerSweep)
	}
	// 最新的周期先被治好。
	latest, err := st.GetReport(ctx, "week", ids[0])
	if err != nil || latest.Status != store.StatusReady {
		t.Errorf("最新周期 %s 没被补上: %v %+v", ids[0], err, latest)
	}
}

// 只勾掉过待办、一条笔记没记的那一周也必须进候选：漏掉它正是 FR-05 说的静默缺失。
// 候选来源从一路变两路之后，这条是那个并集唯一被盯着的地方。
func TestTaskOnlyWeekEntersBackfillCandidates(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)
	ctx := context.Background()

	// 上周（2026-11-16）里勾掉一条待办，那一周一条记录都没有。
	lastWeek := now.AddDate(0, 0, -8)
	seedTask(t, st, "上周清掉的待办", lastWeek.Add(-time.Hour), lastWeek)
	// 本周（未结束）里也勾掉一条，它不该进候选。
	seedTask(t, st, "本周就做完的", now.Add(-time.Hour), now)

	got, err := newTestScheduler(g, st).missingPeriods(ctx, Week, now)
	if err != nil {
		t.Fatalf("missingPeriods: %v", err)
	}
	if len(got) != 1 || got[0] != "2026-11-16" {
		t.Errorf("待办撑起来的候选 = %v, 期望只有 [2026-11-16]", got)
	}
}

func TestDailyOnlyWeekEntersBackfillCandidates(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)
	ctx := context.Background()

	// 上周只写了一天日报，没有记录、待办、阶段。
	seedDaily(t, st, "2026-11-18", "上周的优先级", "", "", now.AddDate(0, 0, -7))
	// 本周（未结束）也写了一条，它不该进候选。
	seedDaily(t, st, "2026-11-25", "本周的优先级", "", "", now)

	got, err := newTestScheduler(g, st).missingPeriods(ctx, Week, now)
	if err != nil {
		t.Fatalf("missingPeriods: %v", err)
	}
	if len(got) != 1 || got[0] != "2026-11-16" {
		t.Errorf("日报撑起来的候选 = %v, 期望只有 [2026-11-16]", got)
	}
}

func TestDailyOnlyMonthEntersBackfillCandidates(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)
	ctx := context.Background()

	seedDaily(t, st, "2026-10-15", "上月的优先级", "", "", now.AddDate(0, -1, 0))

	got, err := newTestScheduler(g, st).missingPeriods(ctx, Month, now)
	if err != nil {
		t.Fatalf("missingPeriods month: %v", err)
	}
	if len(got) != 1 || got[0] != "2026-10-01" {
		t.Errorf("日报撑起来的月报候选 = %v, 期望只有 [2026-10-01]", got)
	}
}

func TestMergeDescUnionsTwoCandidateSources(t *testing.T) {
	notes := []string{"2026-11-16", "2026-11-02"}
	tasks := []string{"2026-11-23", "2026-11-16"}

	got := mergeDesc(notes, tasks)
	want := []string{"2026-11-23", "2026-11-16", "2026-11-02"}
	if len(got) != len(want) {
		t.Fatalf("合并结果 = %v, 期望 %v", got, want)
	}
	for i := range want {
		// 两路共有的 2026-11-16 只出现一次，整体保持由新到旧。
		if got[i] != want[i] {
			t.Fatalf("第 %d 位 = %s, 期望 %s（整个列表 %v）", i, got[i], want[i], got)
		}
	}
}

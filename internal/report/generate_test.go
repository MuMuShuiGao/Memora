package report

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"memora/internal/period"
	"memora/internal/store"
)

var plus8 = time.FixedZone("UTC+8", 8*3600)

// fakeCompleter 是有记录的假模型：报表逻辑只认 Completer 接口，
// 所以这些测试不联网、也不要 API key。
type fakeCompleter struct {
	calls  int
	errs   []error // 第 i 次调用返回 errs[i]；用完即视为成功
	text   string
	system string
	user   string
}

func (f *fakeCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	f.calls++
	f.system = system
	f.user = user
	if f.calls <= len(f.errs) && f.errs[f.calls-1] != nil {
		return "", f.errs[f.calls-1]
	}
	if f.text == "" {
		return "本周期完成\n- 写了报表切片", nil
	}
	return f.text, nil
}

func newTestGenerator(t *testing.T, llm Completer, clock *time.Time) (*Generator, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	g, err := NewGenerator(st, llm, plus8, slog.New(slog.NewTextHandler(discard{}, nil)))
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	if clock != nil {
		g.now = func() time.Time { return *clock }
	}
	return g, st
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// seed 按 design §2 的口径写入记录：归属列由 period 包算好，存进来就固化。
func seed(t *testing.T, st *store.Store, body string, at time.Time) store.Note {
	t.Helper()
	n, err := st.CreateNote(context.Background(), body, at,
		period.WeekStartID(at, plus8), period.MonthStartID(at, plus8))
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	return n
}

func TestGenerateSuccess(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	g, st := newTestGenerator(t, &fakeCompleter{}, &now)
	ctx := context.Background()

	// 先记一条、后记一条，提示词里的顺序必须跟时间一致。
	seed(t, st, "开始写报表模块的提示词", now.AddDate(0, 0, -2))
	seed(t, st, "报表切片跑通了本机冒烟", now.AddDate(0, 0, -1))

	rep, err := g.Generate(ctx, Week, "2026-11-23")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if rep.Status != store.StatusReady {
		t.Errorf("状态 = %s, 期望 ready", rep.Status)
	}
	if rep.Kind != "week" || rep.PeriodStart != "2026-11-23" || rep.PeriodEnd != "2026-11-29" {
		t.Errorf("周期不对: %+v", rep)
	}
	if *rep.SourceCount != 2 {
		t.Errorf("source_count = %d, 期望 2", *rep.SourceCount)
	}
	if !strings.Contains(*rep.Body, "本周期完成") {
		t.Errorf("正文没落库: %q", *rep.Body)
	}
	if rep.GeneratedAt == nil {
		t.Error("ready 却沒有 generated_at")
	}

	// 提示词三件事：规则正文、两条原文都在、条数写明。
	if !strings.Contains(g.prompt, "只准使用给你的素材内容") {
		t.Error("嵌入的提示词没读进来")
	}
	for _, want := range []string{"开始写报表模块的提示词", "报表切片跑通了本机冒烟", "共 2 条素材"} {
		if !strings.Contains(fake(g).user, want) {
			t.Errorf("用户提示词缺 %q:\n%s", want, fake(g).user)
		}
	}
	if strings.Index(fake(g).user, "开始写报表模块的提示词") > strings.Index(fake(g).user, "报表切片跑通了本机冒烟") {
		t.Error("记录应按时间升序拼进提示词")
	}
	if !strings.Contains(fake(g).user, "2026-11-23 至 2026-11-29") {
		t.Errorf("提示词该写清覆盖哪一段:\n%s", fake(g).user)
	}
}

// seedTask 建一条待办；doneAt 非零就顺手勾掉，归属按勾掉那一刻算。
func seedTask(t *testing.T, st *store.Store, body string, at, doneAt time.Time) store.Task {
	t.Helper()
	ctx := context.Background()
	task, err := st.CreateTask(ctx, body, period.DayID(at, plus8), at)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if doneAt.IsZero() {
		return task
	}
	done, err := st.SetTaskDone(ctx, task.ID, true, doneAt,
		period.WeekStartID(doneAt, plus8), period.MonthStartID(doneAt, plus8))
	if err != nil {
		t.Fatalf("SetTaskDone: %v", err)
	}
	return done
}

func TestCompletedTaskJoinsThePromptAsItsOwnSection(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)

	seed(t, st, "随手记的一条想法", now.Add(-3*time.Hour))
	seedTask(t, st, "联调完待办接口", now.Add(-2*time.Hour), now.Add(-time.Hour))
	// 没勾掉的那条不代表任何交付，一个字都不该出现在提示词里（FR-11）。
	seedTask(t, st, "下周再说的备份脚本", now.Add(-time.Hour), time.Time{})

	rep, err := g.Generate(context.Background(), Week, "2026-11-23")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// source_count 是喂给模型的素材条数：1 条记录 + 1 条已完成待办，未完成那条不计。
	if rep.Status != store.StatusReady || *rep.SourceCount != 2 {
		t.Fatalf("报表 = %s / %v 条, 期望 ready 且 2", rep.Status, *rep.SourceCount)
	}

	user := fake(g).user
	if !strings.Contains(user, "联调完待办接口") {
		t.Errorf("已完成待办没进提示词:\n%s", user)
	}
	if strings.Contains(user, "下周再说的备份脚本") {
		t.Errorf("未完成的待办混进了提示词:\n%s", user)
	}
	// 分段而不是混排：模型要靠分段知道"哪条是勾掉的、哪条只是当时想到的"（ADR-0014）。
	noteAt := strings.Index(user, "想法记录按时间升序")
	taskAt := strings.Index(user, "本周期内已勾掉的待办")
	if noteAt < 0 || taskAt < 0 || noteAt > taskAt {
		t.Errorf("两段素材缺失或顺序颠倒: note=%d task=%d\n%s", noteAt, taskAt, user)
	}
	if !strings.Contains(user, "1 条已完成待办") {
		t.Errorf("条数没拆成两路报:\n%s", user)
	}
}

// 心得跟在它那条待办后面进提示词，写法固定是「（心得：…）」；没写心得的不许出现空括号，
// 条数也仍按待办算，心得不是独立素材（ADR-0027、design §6 1.3）。
func TestTakeawayJoinsPromptWithMarker(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	g, st := newTestGenerator(t, &fakeCompleter{}, &now)
	ctx := context.Background()

	with := seedTask(t, st, "联调完待办接口", now.Add(-2*time.Hour), now.Add(-time.Hour))
	if _, err := st.UpdateTaskTakeaway(ctx, with.ID, "比计划多花半天，联调环境已就绪", now.Add(-30*time.Minute)); err != nil {
		t.Fatalf("UpdateTaskTakeaway: %v", err)
	}
	seedTask(t, st, "买牛奶", now.Add(-2*time.Hour), now.Add(-time.Hour))

	rep, err := g.Generate(ctx, Week, "2026-11-23")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if *rep.SourceCount != 2 {
		t.Errorf("source_count = %d, 期望 2（心得不单算一条素材）", *rep.SourceCount)
	}
	user := fake(g).user
	if !strings.Contains(user, "联调完待办接口（心得：比计划多花半天，联调环境已就绪）") {
		t.Errorf("心得没跟在待办后面进提示词:\n%s", user)
	}
	if !strings.Contains(user, "买牛奶\n") || strings.Contains(user, "买牛奶（心得") {
		t.Errorf("没写心得的待办不该出现心得括号:\n%s", user)
	}
}

func TestOnlyTasksStillGenerates(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)

	seedTask(t, st, "把这周该交付的东西清掉", now.Add(-4*time.Hour), now.Add(-time.Hour))

	rep, err := g.Generate(context.Background(), Week, "2026-11-23")
	if err != nil {
		t.Fatalf("一条笔记都没记、只勾掉一条待办的周期也该出报: %v", err)
	}
	if *rep.SourceCount != 1 {
		t.Errorf("source_count = %d, 期望 1", *rep.SourceCount)
	}
	// 记录那一节要显式落成"（无）"，不能整个空着——空着模型会以为格式坏了。
	if !strings.Contains(fake(g).user, "（无）") {
		t.Errorf("空的记录段没有占位:\n%s", fake(g).user)
	}
}

func TestGenerateFailureLandsFailed(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	boom := errors.New("模型接口 HTTP 503")
	llm := &fakeCompleter{errs: []error{boom, boom}}
	g, st := newTestGenerator(t, llm, &now)
	ctx := context.Background()

	seed(t, st, "随便一条", now)

	rep, err := g.Generate(ctx, Week, "2026-11-23")
	if !errors.Is(err, boom) {
		t.Fatalf("Generate 应把模型错误原样带回, 实得 %v", err)
	}
	if rep.Status != store.StatusFailed {
		t.Fatalf("失败也要落库, 状态 = %s", rep.Status)
	}
	if rep.FailReason == nil || !strings.Contains(*rep.FailReason, "503") {
		t.Errorf("失败原因没落库: %+v", rep.FailReason)
	}
	if rep.Body != nil {
		t.Errorf("失败态不该有正文: %v", *rep.Body)
	}
	// design §6 第 3 条：重试恰好 1 次。
	if llm.calls != 2 {
		t.Errorf("调用次数 = %d, 期望 2（一次失败重试一次）", llm.calls)
	}
	// 补跑判据靠这一行存在，不能因为失败就把行删掉。
	if _, err := st.GetReport(ctx, "week", "2026-11-23"); err != nil {
		t.Errorf("失败报表应查得到: %v", err)
	}
}

func TestGenerateRecoversOnRetry(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	llm := &fakeCompleter{errs: []error{errors.New("限流")}}
	g, st := newTestGenerator(t, llm, &now)
	ctx := context.Background()

	seed(t, st, "第一次撞限流, 重试就好了", now)

	rep, err := g.Generate(ctx, Week, "2026-11-23")
	if err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if rep.Status != store.StatusReady || llm.calls != 2 {
		t.Errorf("状态 %s / 调用 %d 次", rep.Status, llm.calls)
	}
}

// 进行中的周期也能生成（FR-06），但两个时刻分工不同：cutoff 是"记录读到哪一刻为止"，
// 取在读完记录之后；generated_at 是"模型答完的那一刻"，必须晚于它。
// 这里用一个每问一次就走 30 秒的时钟，把两个时刻钉成可断言的差值。
func TestGenerateInProgressPeriodCutoffIsNow(t *testing.T) {
	steps := []time.Time{
		time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8),  // 读完记录，划截止
		time.Date(2026, time.November, 25, 10, 30, 0, 0, plus8), // 模型答完
	}
	g, st := newTestGenerator(t, &fakeCompleter{}, nil)
	var i int
	g.now = func() time.Time {
		defer func() { i++ }()
		if i < len(steps) {
			return steps[i]
		}
		t.Errorf("时钟被多问了 %d 次，生成路径上的时刻调用变了", i+1)
		return steps[len(steps)-1]
	}

	// 本周还没结束（今天是周三），照样能出一份"截至现在"的报表。
	seed(t, st, "本周三上午记的", time.Date(2026, time.November, 25, 9, 0, 0, 0, plus8))

	rep, err := g.Generate(context.Background(), Week, "2026-11-23")
	if err != nil {
		t.Fatalf("进行中的周期应能生成: %v", err)
	}
	if rep.Status != store.StatusReady {
		t.Fatalf("状态 = %s", rep.Status)
	}
	if !rep.CreatedAt.Equal(steps[0]) {
		t.Errorf("cutoff 应是读完记录那一刻 %v, 实得 %v", steps[0], rep.CreatedAt)
	}
	if rep.GeneratedAt == nil {
		t.Fatal("generated_at 没落库")
	}
	if !rep.GeneratedAt.Equal(steps[1]) {
		t.Errorf("generated_at = %v, 期望模型答完那一刻 %v", rep.GeneratedAt, steps[1])
	}
	if !rep.GeneratedAt.After(rep.CreatedAt) {
		t.Errorf("generated_at 不该早于 cutoff: %v vs %v", rep.GeneratedAt, rep.CreatedAt)
	}
	// period_end 仍写自然终点，不因周期没结束而改成"截至今天"。
	if rep.PeriodEnd != "2026-11-29" {
		t.Errorf("period_end = %s, 期望自然终点 2026-11-29", rep.PeriodEnd)
	}
}

func TestGenerateEmptyPeriod(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)
	ctx := context.Background()

	// 上一周一条都没有。
	if _, err := g.Generate(ctx, Week, "2026-11-16"); !errors.Is(err, ErrNoRecords) {
		t.Fatalf("空周期应回 ErrNoRecords, 实得 %v", err)
	}
	if llm.calls != 0 {
		t.Errorf("空周期不该去打模型, 调了 %d 次", llm.calls)
	}
	if _, err := st.GetReport(ctx, "week", "2026-11-16"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("空周期不该留下报表行: %v", err)
	}
}

func TestGenerateWithoutModelConfigured(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	g, st := newTestGenerator(t, nil, &now)
	ctx := context.Background()

	seed(t, st, "有记录但没配模型", now)

	if _, err := g.Generate(ctx, Week, "2026-11-23"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("未配置模型应回 ErrNotConfigured, 实得 %v", err)
	}
	// 没配模型不是"生成失败"，不该把它写成一条 failed 报表——那样补跑会以为已经试过。
	if _, err := st.GetReport(ctx, "week", "2026-11-23"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("未配置时不该落报表行: %v", err)
	}
}

func TestGenerateRejectsWrongPeriodStart(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, _ := newTestGenerator(t, llm, &now)

	if _, err := g.Generate(context.Background(), Week, "2026-11-24"); err == nil {
		t.Error("周二当周报起始日应被拒绝")
	} else if !errors.Is(err, period.ErrNotMonday) {
		t.Errorf("应回 ErrNotMonday, 实得 %v", err)
	}
	if _, err := g.Generate(context.Background(), Month, "2026-11-23"); !errors.Is(err, period.ErrNotMonthStart) {
		t.Errorf("月报给非月初应回 ErrNotMonthStart, 实得 %v", err)
	}
	if llm.calls != 0 {
		t.Errorf("非法周期不该打到模型, 调了 %d 次", llm.calls)
	}
}

// 月报覆盖的是自然月，取记录靠 month_start 列——跨周的记录要一起进来。
func TestGenerateMonthSpansWeeks(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	g, st := newTestGenerator(t, &fakeCompleter{}, &now)

	seed(t, st, "11 月第一周的事", time.Date(2026, time.November, 3, 9, 0, 0, 0, plus8))
	seed(t, st, "11 月第三周的事", time.Date(2026, time.November, 17, 9, 0, 0, 0, plus8))
	seed(t, st, "10 月的事，不该进来", time.Date(2026, time.October, 30, 9, 0, 0, 0, plus8))
	seed(t, st, "12 月的事，也不该进来", time.Date(2026, time.December, 1, 9, 0, 0, 0, plus8))

	rep, err := g.Generate(context.Background(), Month, "2026-11-01")
	if err != nil {
		t.Fatalf("Generate month: %v", err)
	}
	if *rep.SourceCount != 2 {
		t.Errorf("月报应只含 11 月两条, 实得 %d", *rep.SourceCount)
	}
	if rep.PeriodEnd != "2026-11-30" {
		t.Errorf("period_end = %s, 期望 2026-11-30", rep.PeriodEnd)
	}
	for _, unwanted := range []string{"10 月的事", "12 月的事"} {
		if strings.Contains(fake(g).user, unwanted) {
			t.Errorf("提示词串进了别的月份的记录: %s", unwanted)
		}
	}
}

func TestGenerateNoRetryAfterCancel(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	g, st := newTestGenerator(t, nil, &now)
	seed(t, st, "记一条", now)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 第一次调用就把 ctx 掐掉，模拟客户端断开或进程收到退出信号。
	llm := &cancellingCompleter{cancel: cancel, err: errors.New("连接断了")}
	g.llm = llm

	rep, err := g.Generate(ctx, Week, "2026-11-23")
	if err == nil {
		t.Fatal("取消后应回错误")
	}
	if llm.calls != 1 {
		t.Errorf("取消后不该再重试, 调了 %d 次", llm.calls)
	}
	if rep.Status != store.StatusFailed {
		t.Errorf("取消也要把状态从 running 收掉, 实得 %s", rep.Status)
	}
	if rep.FailReason == nil || !strings.Contains(*rep.FailReason, "cancel") {
		t.Errorf("失败原因看不出是被取消: %v", rep.FailReason)
	}
}

type cancellingCompleter struct {
	cancel func()
	err    error
	calls  int
}

func (c *cancellingCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	c.calls++
	c.cancel()
	return "", c.err
}

func fake(g *Generator) *fakeCompleter {
	c, _ := g.llm.(*fakeCompleter)
	return c
}

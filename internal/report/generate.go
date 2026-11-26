// Package report 负责把某个周期的记录拼成提示词、调外部模型、把结果落成快照。
// 生成只发生在服务端（ADR-0003），手机只读结果。
package report

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"memora/internal/period"
	"memora/internal/store"
)

// 提示词随二进制发布，改它要重新编译——它和报表形状是同一件事的两半，
// 不该出现"二进制升了、提示词还是旧的"这种状态。
//
//go:embed prompt.md
var promptFS embed.FS

// Kind 是报表种类，取值与数据库 CHECK 一致。
type Kind string

const (
	Week  Kind = "week"
	Month Kind = "month"
)

func ParseKind(s string) (Kind, error) {
	switch Kind(s) {
	case Week:
		return Week, nil
	case Month:
		return Month, nil
	default:
		return "", fmt.Errorf("report: 未知报表种类 %q，只认 week 或 month", s)
	}
}

// ErrNoRecords 表示该周期一条记录都没有。design §6 第 4 条：不生成，
// 免得让模型憋出一份空话——也让"这个周期没东西"和"生成失败了"区分开。
var ErrNoRecords = errors.New("report: 该周期没有记录")

// 重试 1 次是 design §6 第 3 条定死的，不做成配置项：可调的参数只会让人以为
// 调大就能治本，而模型侧的抖动本来就得靠手动重跑兜住。
const (
	attempts     = 2
	callTimeout  = 60 * time.Second
	betweenRetry = 2 * time.Second
)

type Generator struct {
	st     *store.Store
	llm    Completer
	loc    *time.Location
	log    *slog.Logger
	mu     sync.Mutex // 手动生成与补跑撞上同一周期时，只让一个真跑
	prompt string
	// now 让测试能把"生成完成时刻"钉住；生产用 time.Now。
	now func() time.Time
}

func NewGenerator(st *store.Store, llm Completer, loc *time.Location, log *slog.Logger) (*Generator, error) {
	prompt, err := promptFS.ReadFile("prompt.md")
	if err != nil {
		return nil, fmt.Errorf("report: 读提示词: %w", err)
	}
	return &Generator{
		st:     st,
		llm:    llm,
		loc:    loc,
		log:    log,
		prompt: string(prompt),
		now:    time.Now,
	}, nil
}

// Generate 同步跑完一个周期的报表，返回落库后的快照。
// 失败也会落库（status=failed）：调用方拿到错误的同时，库里那条不再是 running，
// 这是 FR-05"不静默缺失"的落点——任何时刻都能从 status 看出走到了哪一步。
func (g *Generator) Generate(ctx context.Context, kind Kind, periodStart string) (store.Report, error) {
	periodEnd, err := g.endID(kind, periodStart)
	if err != nil {
		return store.Report{}, err
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.llm == nil {
		return store.Report{}, ErrNotConfigured
	}

	notes, err := g.notesIn(ctx, kind, periodStart)
	if err != nil {
		return store.Report{}, err
	}
	// 待办按"什么时候勾掉"归属，和记录按创建时间归属是两条独立的路（design §2）。
	// 两路都为空才判"这个周期没东西"：只勾掉待办、一条笔记没记的一周，那几条恰恰就是
	// 本周的交付，不能因为没有笔记就不出报（design §6 1.2）。
	tasks, err := g.tasksIn(ctx, kind, periodStart)
	if err != nil {
		return store.Report{}, err
	}
	if len(notes) == 0 && len(tasks) == 0 {
		return store.Report{}, ErrNoRecords
	}
	// 截止时刻取"读完素材的那一刻"：手动生成未结束的周期时，这就是 design §2
	// 说的实际截止，之后新记的东西归下一次生成。
	cutoff := g.now()

	if _, err := g.st.BeginRun(ctx, string(kind), periodStart, periodEnd, cutoff); err != nil {
		return store.Report{}, err
	}

	userPrompt := g.buildUserPrompt(kind, periodStart, periodEnd, notes, tasks)
	sources := len(notes) + len(tasks)

	// 60s 是"这一次生成"的总预算，覆盖所有尝试。每次调用重新计时的话，
	// "重试一次"实际会变成"最多 120s"，手动生成那个同步请求就等得没道理了。
	budget, cancelBudget := context.WithTimeout(ctx, callTimeout)
	defer cancelBudget()

	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			// 重试前等一会儿。模型侧多半是限流或瞬时抖动，立刻重打只会再撞一次。
			select {
			case <-budget.Done():
				lastErr = fmt.Errorf("%w（重试前预算已耗尽，上次错误: %v）", budget.Err(), lastErr)
			case <-time.After(betweenRetry):
			}
			if budget.Err() != nil {
				break
			}
		}

		text, err := g.llm.Complete(budget, g.prompt, userPrompt)

		if err == nil {
			// 正文已经拿到了，落库这一下不该因为客户端断开而丢掉——用一个脱离
			// 请求的上下文，状态才不会出现"跑完了但库里还是 running"。
			doneCtx, cancelWrite := g.detached(ctx)
			defer cancelWrite()
			// generated_at 是"模型答完的那一刻"，跟 cutoff 分工明确：后者划记录范围，
			// 前者标结果何时可用。混用一个时刻会让一次 40s 的生成看起来在 0s 就完成了。
			at := g.now()
			ready, ferr := g.st.FinishRun(doneCtx, string(kind), periodStart, store.StatusReady,
				text, sources, "", at, at)
			if ferr != nil {
				return store.Report{}, ferr
			}
			g.log.Info("报表生成完成", "kind", string(kind), "period", periodStart,
				"notes", len(notes), "tasks", len(tasks), "sources", sources)
			return ready, nil
		}

		if ctx.Err() != nil {
			// 客户端断开或进程在退出——结果已经没人要了，不再打第二次。
			// 注意这里看的是调用方上下文；60s 预算耗尽（budget.Err）是超时而不是取消，
			// 它该走下面的重试一次，不能被误判成"客户端走了"。
			lastErr = fmt.Errorf("%w（调用失败: %v）", ctx.Err(), err)
			break
		}
		lastErr = err
		g.log.Warn("生成失败", "kind", string(kind), "period", periodStart, "attempt", i+1, "err", err.Error())
	}

	failedCtx, cancelFailed := g.detached(ctx)
	defer cancelFailed()
	attemptedAt := g.now()
	failed, ferr := g.st.FinishRun(failedCtx, string(kind), periodStart, store.StatusFailed,
		"", 0, lastErr.Error(), attemptedAt, attemptedAt)
	if ferr != nil {
		return store.Report{}, fmt.Errorf("%w（写失败状态也失败: %v）", lastErr, ferr)
	}
	return failed, lastErr
}

// detached 给"结果必须落库"的那两次写操作用：请求上下文可能已经取消，
// 但库里的状态不能停在 running。上限 10s，避免退出时被一次写卡死。
func (g *Generator) detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
}

// SetClock 换掉时间来源。测试用它把 generated_at 钉成确定值；生产不调。
func (g *Generator) SetClock(now func() time.Time) { g.now = now }

func (g *Generator) endID(kind Kind, periodStart string) (string, error) {
	if kind == Week {
		return period.WeekEndID(periodStart, g.loc)
	}
	return period.MonthEndID(periodStart, g.loc)
}

func (g *Generator) notesIn(ctx context.Context, kind Kind, periodStart string) ([]store.Note, error) {
	if kind == Week {
		return g.st.NotesForWeek(ctx, periodStart)
	}
	return g.st.NotesForMonth(ctx, periodStart)
}

func (g *Generator) tasksIn(ctx context.Context, kind Kind, periodStart string) ([]store.Task, error) {
	if kind == Week {
		return g.st.TasksDoneForWeek(ctx, periodStart)
	}
	return g.st.TasksDoneForMonth(ctx, periodStart)
}

// buildUserPrompt 按时间升序列出素材，每行前缀本地日期。给日期是为了让模型分得清
// "同一件事被记了两次"和"两个不同周期的同名事项"；它不许凭日期编造内容，那条写在提示词里。
//
// 两路素材分成两段、各自标出语义，不混成一段流水：记录是"当时想到的"，勾掉的待办是
// "确实做完了的"。混起来模型就分不开这两件事，而需求 §5 第 4 条要求正文不得出现素材里
// 没有的事实——来源糊掉之后那条约束连核查的抓手都没有（ADR-0014）。
func (g *Generator) buildUserPrompt(kind Kind, start, end string, notes []store.Note, tasks []store.Task) string {
	kindName := "周报"
	if kind == Month {
		kindName = "月报"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "请生成 %s 覆盖的自然周期：%s 至 %s（含两端，按 %s 的日历）。\n", kindName, start, end, g.loc)
	b.WriteString("下面两类素材彼此独立：一类是随手记下的想法，一类是在这个周期内勾掉的待办。\n")

	b.WriteString("\n想法记录按时间升序，每行一条，方括号内是记录日期：\n\n")
	if len(notes) == 0 {
		b.WriteString("（无）\n")
	}
	for _, n := range notes {
		fmt.Fprintf(&b, "[%s] %s\n", n.CreatedAt.In(g.loc).Format("2006-01-02"), n.Body)
	}

	b.WriteString("\n本周期内已勾掉的待办，按完成时间升序，方括号内是完成日期：\n\n")
	if len(tasks) == 0 {
		b.WriteString("（无）\n")
	}
	for _, t := range tasks {
		fmt.Fprintf(&b, "[%s] %s\n", t.DoneAt.In(g.loc).Format("2006-01-02"), t.Body)
	}

	fmt.Fprintf(&b, "\n共 %d 条素材：%d 条记录 + %d 条已完成待办。", len(notes)+len(tasks), len(notes), len(tasks))
	return b.String()
}

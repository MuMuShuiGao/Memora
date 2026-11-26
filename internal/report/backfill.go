package report

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"memora/internal/period"
	"memora/internal/store"
)

// 补跑间隔取 1 小时。判断依据是"这个已结束周期有没有 ready 报表"，
// 不是"现在是不是周一早上八点"——于是关机两天后起来会自动把漏的补上（design §7）。
const defaultSweepInterval = time.Hour

type Scheduler struct {
	g        *Generator
	st       *store.Store
	loc      *time.Location
	log      *slog.Logger
	interval time.Duration
}

func NewScheduler(g *Generator, st *store.Store, loc *time.Location, log *slog.Logger) *Scheduler {
	return &Scheduler{g: g, st: st, loc: loc, log: log, interval: defaultSweepInterval}
}

// Run 起来先补一次（覆盖"服务重启"这个最常见的漏跑场景），之后按小时循环，
// 直到 ctx 取消。放在进程内是 ADR-0006 的决定，不引 crontab。
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(s.interval)
	defer t.Stop()

	s.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sweep(ctx)
		}
	}
}

// sweepResult 一次扫描的账目，日志和冒烟测试都靠它核对。
type sweepResult struct {
	candidates int // 判据选出的待补周期数，与实际生成数分开数：判据坏了和没东西可补是两回事
	done       int
	failed     int
	skipped    int
}

// 一轮、每种报表最多补这么多份，理由写在 generateAll 上。
const maxPerSweep = 6

func (s *Scheduler) sweep(ctx context.Context) sweepResult {
	var out sweepResult
	now := s.g.now()

	// 候选数在进入生成之前就先打出去。一份报表可能要打两次外部模型、等到 120s，
	// 把这一行压到最后等于"补跑要么没跑、要么跑了半天才留证据"——单机部署没有
	// 别的观测面，静默的空转和静默的坏掉长一模一样。
	weekly, weekErr := s.missingPeriods(ctx, Week, now)
	monthly, monthErr := s.missingPeriods(ctx, Month, now)
	out.candidates = len(weekly) + len(monthly)
	s.log.Info("补跑扫描", "待补周期", out.candidates, "周报", len(weekly), "月报", len(monthly))

	if weekErr != nil {
		s.log.Error("列出周报待补周期失败", "err", weekErr.Error())
	}
	if monthErr != nil {
		s.log.Error("列出月报待补周期失败", "err", monthErr.Error())
	}

	for _, batch := range []struct {
		kind Kind
		ids  []string
	}{
		{Week, weekly},
		{Month, monthly},
	} {
		s.generateAll(ctx, batch.kind, batch.ids, &out)
	}

	if out.done+out.failed+out.skipped > 0 {
		s.log.Info("补跑结束", "生成", out.done, "失败", out.failed, "跳过", out.skipped)
	}
	return out
}

// generateAll 一轮里每种报表最多补 maxPerSweep 份。关了几个月的机器重新起来时
// 候选可能有几十周，一次全打完会把重启后的头几分钟全占在排队等模型上。
// 列表按周期由新到旧排，所以上限留下的是最近的几个，剩下的下一轮继续。
func (s *Scheduler) generateAll(ctx context.Context, kind Kind, ids []string, out *sweepResult) {
	if len(ids) > maxPerSweep {
		s.log.Info("待补周期过多，本轮只补最近的", "kind", string(kind),
			"total", len(ids), "本轮", maxPerSweep)
		ids = ids[:maxPerSweep]
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		res, err := s.g.Generate(ctx, kind, id)
		switch {
		case err == nil:
			out.done++
			s.log.Info("补跑生成", "kind", string(kind), "period", id,
				"sources", derefCount(res.SourceCount))
		case errors.Is(err, ErrNoRecords):
			// 候选来自素材本身，走到这里只可能是记录或待办刚被删掉。
			out.skipped++
		default:
			out.failed++
			s.log.Error("补跑失败", "kind", string(kind), "period", id, "err", err.Error())
		}
	}
}

// missingPeriods 列出"已经结束、且库里没有 ready 报表"的周期。
// 这是 design §7 那个形状的唯一实现点：范围来自记录，判定来自状态。
func (s *Scheduler) missingPeriods(ctx context.Context, kind Kind, now time.Time) ([]string, error) {
	ids, err := s.finishedPeriods(ctx, kind)
	if err != nil {
		return nil, err
	}

	var missing []string
	for _, id := range ids {
		ready, err := s.isReady(ctx, kind, id)
		if err != nil {
			return nil, err
		}
		if ready {
			continue
		}
		if s.periodEnded(kind, id, now) {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// periodEnded 判断该周期是否已经结束，结束才需要报表。返回 false 表示还在进行中。
func (s *Scheduler) periodEnded(kind Kind, id string, now time.Time) bool {
	var (
		end time.Time
		err error
	)
	if kind == Week {
		_, end, err = period.WeekRange(id, s.loc)
	} else {
		_, end, err = period.MonthRange(id, s.loc)
	}
	if err != nil {
		// 库里出现了不合法的起始日，说明归属列被写坏了。跳过并留下日志，
		// 不让一条脏数据把整个扫描带崩。
		s.log.Error("周期起始日不合法，跳过", "kind", string(kind), "period", id, "err", err.Error())
		return false
	}
	return !end.After(now)
}

func (s *Scheduler) finishedPeriods(ctx context.Context, kind Kind) ([]string, error) {
	// 候选来自两路：记过笔记的周期，和勾掉过待办的周期。只靠前者会漏掉"这周啥也没记、
	// 但勾掉了三件事"那一周——而那一周的报表恰恰最该自动生成（FR-05 不静默缺失）。
	var fromNotes, fromTasks []string
	var err error
	if kind == Week {
		fromNotes, err = s.st.DistinctWeekStarts(ctx)
		if err != nil {
			return nil, err
		}
		fromTasks, err = s.st.DistinctDoneWeekStarts(ctx)
	} else {
		fromNotes, err = s.st.DistinctMonthStarts(ctx)
		if err != nil {
			return nil, err
		}
		fromTasks, err = s.st.DistinctDoneMonthStarts(ctx)
	}
	if err != nil {
		return nil, err
	}
	return mergeDesc(fromNotes, fromTasks), nil
}

// mergeDesc 把两段各自倒序的周期 id 合成一段倒序去重的列表。
// 顺序有意义：一轮最多补 maxPerSweep 份，排在前面的先拿到。
func mergeDesc(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] >= b[j] {
			out, i = appendUnique(out, seen, a[i]), i+1
		} else {
			out, j = appendUnique(out, seen, b[j]), j+1
		}
	}
	for ; i < len(a); i++ {
		out = appendUnique(out, seen, a[i])
	}
	for ; j < len(b); j++ {
		out = appendUnique(out, seen, b[j])
	}
	return out
}

func appendUnique(out []string, seen map[string]bool, id string) []string {
	if seen[id] {
		return out
	}
	seen[id] = true
	return append(out, id)
}

func (s *Scheduler) isReady(ctx context.Context, kind Kind, id string) (bool, error) {
	r, err := s.st.GetReport(ctx, string(kind), id)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("report: 查 %s/%s 状态: %w", kind, id, err)
	}
	return r.Status == store.StatusReady, nil
}

func derefCount(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

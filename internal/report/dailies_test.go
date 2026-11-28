package report

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"memora/internal/store"
)

func seedDaily(t *testing.T, st *store.Store, day, priorities, done, risk string, at time.Time) store.Daily {
	t.Helper()
	d, err := st.UpsertDaily(context.Background(), day, priorities, done, risk, at)
	if err != nil {
		t.Fatalf("UpsertDaily %s: %v", day, err)
	}
	return d
}

// 日报是第四路素材，提示词里必须标明它的语义：优先级是计划、完成是原话、卡点进风险。
func TestDailyJoinsPromptAsItsOwnSection(t *testing.T) {
	now := time.Date(2026, time.November, 25, 21, 0, 0, 0, plus8)
	g, st := newTestGenerator(t, &fakeCompleter{}, &now)

	// 本周三写的一条日报，以及上周（不该进来）的一条。
	seedDaily(t, st, "2026-11-25", "1. 写完日报接口\n2. 配好证书", "日报屏和接口都通了", "等需求方给模型 key",
		time.Date(2026, time.November, 25, 21, 0, 0, 0, plus8))
	seedDaily(t, st, "2026-11-18", "上周的优先级", "上周的完成", "", now.AddDate(0, 0, -7))

	rep, err := g.Generate(context.Background(), Week, "2026-11-23")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if rep.Status != store.StatusReady || *rep.SourceCount != 1 {
		t.Fatalf("报表 = %s / %v 条, 期望 ready 且 1（上周那条不算）", rep.Status, *rep.SourceCount)
	}

	user := fake(g).user
	for _, want := range []string{
		"本人手写的日报",
		"[2026-11-25]",
		"优先级（是计划，不是完成）：1. 写完日报接口",
		"做成了什么：日报屏和接口都通了",
		"卡点与没做完的：等需求方给模型 key",
		"1 天日报",
	} {
		if !strings.Contains(user, want) {
			t.Errorf("日报没按约定进提示词，缺 %q:\n%s", want, user)
		}
	}
	if strings.Contains(user, "上周的优先级") {
		t.Errorf("别的周的日报混进了提示词:\n%s", user)
	}
	// 四路分段：日报段排在阶段段之后，且明确独立。
	stageAt := strings.Index(user, "本周期内已勾掉的阶段")
	dailyAt := strings.Index(user, "本人手写的日报")
	if stageAt < 0 || dailyAt < 0 || stageAt > dailyAt {
		t.Errorf("日报段缺失或顺序不对: stage=%d daily=%d\n%s", stageAt, dailyAt, user)
	}
}

func TestOnlyDailyStillGenerates(t *testing.T) {
	now := time.Date(2026, time.November, 25, 21, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)

	seedDaily(t, st, "2026-11-24", "只写了优先级", "", "", now)

	rep, err := g.Generate(context.Background(), Week, "2026-11-23")
	if err != nil {
		t.Fatalf("只写了一天日报的一周也该出报: %v", err)
	}
	if *rep.SourceCount != 1 {
		t.Errorf("source_count = %d, 期望 1", *rep.SourceCount)
	}
	// 空的另外三路各自要有（无）占位，模型才不会以为格式坏了。
	if strings.Count(fake(g).user, "（无）") < 3 {
		t.Errorf("空素材段没有占位:\n%s", fake(g).user)
	}
}

func TestAllEmptyDailyIsNotMaterial(t *testing.T) {
	now := time.Date(2026, time.November, 25, 21, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)

	seedDaily(t, st, "2026-11-24", "", "", "", now)

	if _, err := g.Generate(context.Background(), Week, "2026-11-23"); !errors.Is(err, ErrNoRecords) {
		t.Fatalf("三节全空不该算素材, 实得 %v", err)
	}
	if llm.calls != 0 {
		t.Errorf("空周期不该打模型, 调了 %d 次", llm.calls)
	}
}

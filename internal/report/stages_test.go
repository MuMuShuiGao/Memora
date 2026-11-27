package report

import (
	"context"
	"strings"
	"testing"
	"time"
)

// 阶段素材单独成段，写成「父任务 · 阶段名（心得：…）」；source_count 把它算一条，
// 但它不许被写成整件事已交付（ADR-0030）。
func TestStageOnlyWeekShowsInPromptAsProgress(t *testing.T) {
	now := time.Date(2026, time.November, 25, 10, 0, 0, 0, plus8)
	llm := &fakeCompleter{}
	g, st := newTestGenerator(t, llm, &now)
	ctx := context.Background()

	task, err := st.CreateTask(ctx, "交付方案", "2026-11-24", now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	stage, err := st.CreateStage(ctx, task.ID, "调研完成", now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	if _, err := st.SetStageDone(ctx, stage.ID, true, now.AddDate(0, 0, -1), "2026-11-23", "2026-11-01"); err != nil {
		t.Fatalf("SetStageDone: %v", err)
	}
	if _, err := st.UpdateStageTakeaway(ctx, stage.ID, "三份访谈整理完", now.AddDate(0, 0, -1)); err != nil {
		t.Fatalf("UpdateStageTakeaway: %v", err)
	}

	rep, err := g.Generate(ctx, Week, "2026-11-23")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if *rep.SourceCount != 1 {
		t.Errorf("source_count = %d, 期望 1（阶段算一条素材）", *rep.SourceCount)
	}
	for _, want := range []string{
		"交付方案 · 调研完成",
		"（心得：三份访谈整理完）",
		"1 条已完成阶段",
		"只代表那件事推进了一段",
	} {
		if !strings.Contains(llm.user, want) {
			t.Errorf("用户提示词缺 %q:\n%s", want, llm.user)
		}
	}
	// 提示词正文本身也要告诉模型：阶段完成不等于整件事交付。
	if !strings.Contains(g.prompt, "勾掉的阶段只代表那件事推进了一段") {
		t.Error("嵌入的提示词没有阶段口径")
	}
}

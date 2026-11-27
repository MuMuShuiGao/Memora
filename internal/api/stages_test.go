package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"memora/internal/period"
	"memora/internal/store"
)

func (h *harness) createStage(t *testing.T, taskID int64, body string) store.Stage {
	t.Helper()
	res, raw := h.do(t, "POST", "/v1/tasks/"+strconv.FormatInt(taskID, 10)+"/stages", `{"body":"`+body+`"}`, true)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST 阶段 %q = %d %s", body, res.StatusCode, raw)
	}
	var out store.Stage
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("解析阶段: %v — %s", err, raw)
	}
	return out
}

func TestStagesRequirePassphrase(t *testing.T) {
	h := newHarness(t)
	task := h.createTask(t, "挂阶段的待办")
	stage := h.createStage(t, task.ID, "第一步")

	for _, c := range []struct{ method, path, body string }{
		{"POST", "/v1/tasks/" + strconv.FormatInt(task.ID, 10) + "/stages", `{"body":"x"}`},
		{"PATCH", "/v1/stages/" + strconv.FormatInt(stage.ID, 10), `{"done":true}`},
		{"DELETE", "/v1/stages/" + strconv.FormatInt(stage.ID, 10), ""},
	} {
		res, body := h.do(t, c.method, c.path, c.body, false)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("无口令 %s %s = %d, 期望 401", c.method, c.path, res.StatusCode)
		}
		if strings.Contains(body, "第一步") {
			t.Errorf("401 泄露了阶段内容: %s", body)
		}
	}
}

func TestStageCRUDAndTaskList(t *testing.T) {
	h := newHarness(t)
	task := h.createTask(t, "写方案")
	first := h.createStage(t, task.ID, "调研现状")
	second := h.createStage(t, task.ID, "写初稿")
	if first.Done || first.Takeaway != "" || first.TaskID != task.ID {
		t.Fatalf("新阶段 = %+v", first)
	}

	// 阶段随任务列表一起回来，按加入顺序。
	list := h.listTasks(t)
	if len(list.Tasks[0].Stages) != 2 || list.Tasks[0].Stages[0].ID != first.ID || list.Tasks[0].Stages[1].ID != second.ID {
		t.Fatalf("列表里的阶段 = %+v", list.Tasks[0].Stages)
	}

	id := strconv.FormatInt(first.ID, 10)
	res, raw := h.do(t, "PATCH", "/v1/stages/"+id, `{"body":"调研完成，结论待整理"}`, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("改阶段名 = %d %s", res.StatusCode, raw)
	}
	var patched store.Stage
	if err := json.Unmarshal([]byte(raw), &patched); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if patched.Body != "调研完成，结论待整理" || patched.Done {
		t.Errorf("改阶段名后被牵连: %+v", patched)
	}

	// 心得与勾掉各走各的字段，勾掉写归属列，取消勾选保留心得。
	if res, raw := h.do(t, "PATCH", "/v1/stages/"+id, `{"takeaway":"三份访谈整理完"}`, true); res.StatusCode != http.StatusOK {
		t.Fatalf("写心得 = %d %s", res.StatusCode, raw)
	}
	res, raw = h.do(t, "PATCH", "/v1/stages/"+id, `{"done":true}`, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("勾阶段 = %d %s", res.StatusCode, raw)
	}
	var done store.Stage
	if err := json.Unmarshal([]byte(raw), &done); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if !done.Done || done.DoneAt == nil || done.DoneWeekStart == "" || done.Takeaway != "三份访谈整理完" {
		t.Errorf("勾掉后 = %+v", done)
	}

	res, raw = h.do(t, "PATCH", "/v1/stages/"+id, `{"done":false}`, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("取消勾选 = %d %s", res.StatusCode, raw)
	}
	var back store.Stage
	if err := json.Unmarshal([]byte(raw), &back); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if back.Done || back.DoneAt != nil || back.Takeaway != "三份访谈整理完" {
		t.Errorf("取消勾选后 = %+v", back)
	}

	if res, _ := h.do(t, "DELETE", "/v1/stages/"+id, "", true); res.StatusCode != http.StatusNoContent {
		t.Errorf("删阶段 = %d, 期望 204", res.StatusCode)
	}
	if res, body := h.do(t, "DELETE", "/v1/stages/"+id, "", true); res.StatusCode != http.StatusNotFound ||
		!strings.Contains(body, "not_found") {
		t.Errorf("重复删除 = %d %s", res.StatusCode, body)
	}

	// 删待办连带删阶段：再查这条待办已经 404，阶段 id 也不该能改。
	if res, _ := h.do(t, "DELETE", "/v1/tasks/"+strconv.FormatInt(task.ID, 10), "", true); res.StatusCode != http.StatusNoContent {
		t.Fatalf("删待办失败")
	}
	if res, _ := h.do(t, "PATCH", "/v1/stages/"+strconv.FormatInt(second.ID, 10), `{"done":true}`, true); res.StatusCode != http.StatusNotFound {
		t.Errorf("父任务删掉后阶段还能改: %d", res.StatusCode)
	}
}

func TestStageValidationAndLimit(t *testing.T) {
	h := newHarness(t)
	task := h.createTask(t, "长活")
	id := strconv.FormatInt(task.ID, 10)

	if res, body := h.do(t, "POST", "/v1/tasks/"+id+"/stages", `{"body":"   "}`, true); res.StatusCode != http.StatusBadRequest ||
		!strings.Contains(body, "不能为空") {
		t.Errorf("空阶段名 = %d %s", res.StatusCode, body)
	}
	long := strings.Repeat("字", 31)
	if res, body := h.do(t, "POST", "/v1/tasks/"+id+"/stages", `{"body":"`+long+`"}`, true); res.StatusCode != http.StatusBadRequest ||
		!strings.Contains(body, "30") {
		t.Errorf("超长阶段名 = %d %s", res.StatusCode, body)
	}
	justFit := strings.Repeat("字", 30)
	if res, body := h.do(t, "POST", "/v1/tasks/"+id+"/stages", `{"body":"`+justFit+`"}`, true); res.StatusCode != http.StatusCreated {
		t.Errorf("正好 30 字 = %d %s, 期望通过", res.StatusCode, body)
	}

	for i := 1; i < 5; i++ {
		h.createStage(t, task.ID, "阶段"+strconv.Itoa(i))
	}
	res, body := h.do(t, "POST", "/v1/tasks/"+id+"/stages", `{"body":"第六个"}`, true)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "5") {
		t.Errorf("第六个阶段 = %d %s, 期望 400 且提到上限", res.StatusCode, body)
	}

	if res, body := h.do(t, "POST", "/v1/tasks/9999/stages", `{"body":"没有父任务"}`, true); res.StatusCode != http.StatusNotFound ||
		!strings.Contains(body, "待办不存在") {
		t.Errorf("未知待办 = %d %s, 期望 404", res.StatusCode, body)
	}
	if res, body := h.do(t, "PATCH", "/v1/stages/9999", `{}`, true); res.StatusCode != http.StatusBadRequest ||
		!strings.Contains(body, "PATCH 要带") {
		t.Errorf("空 PATCH = %d %s", res.StatusCode, body)
	}
}

// seedStageDone 直接在存储层把阶段勾到指定周期，报表测试要的是"落进哪一期"。
func (h *harness) seedStageDone(t *testing.T, taskID int64, body string, at time.Time) store.Stage {
	t.Helper()
	ctx := context.Background()
	stage, err := h.st.CreateStage(ctx, taskID, body, at)
	if err != nil {
		t.Fatalf("CreateStage: %v", err)
	}
	done, err := h.st.SetStageDone(ctx, stage.ID, true, at,
		period.WeekStartID(at, h.loc), period.MonthStartID(at, h.loc))
	if err != nil {
		t.Fatalf("SetStageDone: %v", err)
	}
	return done
}

func TestStageOnlyWeekGeneratesReport(t *testing.T) {
	h := newHarness(t)
	at := time.Date(2026, time.November, 24, 9, 0, 0, 0, testTZ)
	task, err := h.st.CreateTask(context.Background(), "分阶段的大活", period.DayID(at, h.loc), at)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// 父任务没勾掉、一条笔记也没有，只勾了一个阶段：这周照样该有报表（ADR-0030）。
	h.seedStageDone(t, task.ID, "调研完成", at)

	weekStart := period.WeekStartID(at, h.loc)
	res, body := h.do(t, "POST", "/v1/reports/week/"+weekStart+"/generate", "", true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("只有阶段的周期出不了报: %d %s", res.StatusCode, body)
	}
	var rep store.Report
	if err := json.Unmarshal([]byte(body), &rep); err != nil {
		t.Fatalf("解析报表: %v", err)
	}
	if rep.Status != store.StatusReady || rep.SourceCount == nil || *rep.SourceCount != 1 {
		t.Fatalf("报表 = %s source_count %v, 期望 ready 且 1", rep.Status, rep.SourceCount)
	}
}

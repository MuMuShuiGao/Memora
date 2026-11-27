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

type taskEnvelope struct {
	Tasks     []store.Task `json:"tasks"`
	OpenCount int          `json:"open_count"`
	DoneCount int          `json:"done_count"`
}

func (h *harness) listTasks(t *testing.T) taskEnvelope {
	t.Helper()
	res, body := h.do(t, "GET", "/v1/tasks", "", true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/tasks = %d %s", res.StatusCode, body)
	}
	var out taskEnvelope
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("解析待办列表: %v — %s", err, body)
	}
	return out
}

func (h *harness) createTask(t *testing.T, body string) store.Task {
	t.Helper()
	res, raw := h.do(t, "POST", "/v1/tasks", `{"body":"`+body+`"}`, true)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /v1/tasks %q = %d %s", body, res.StatusCode, raw)
	}
	var out store.Task
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("解析新建待办: %v — %s", err, raw)
	}
	return out
}

func idsParam(ids ...int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return `{"ids":[` + strings.Join(parts, ",") + `]}`
}

func TestTasksRequirePassphrase(t *testing.T) {
	h := newHarness(t)
	h.createTask(t, "一条已经存在的待办")

	// FR-07 对每一类业务数据都要成立，待办不是例外；401 的响应体里一个字都不能有。
	cases := []struct{ method, path, body string }{
		{"GET", "/v1/tasks", ""},
		{"POST", "/v1/tasks", `{"body":"新的一条"}`},
		{"PATCH", "/v1/tasks/1", `{"done":true}`},
		{"DELETE", "/v1/tasks/1", ""},
		{"POST", "/v1/tasks/reorder", idsParam(1)},
	}
	for _, c := range cases {
		res, body := h.do(t, c.method, c.path, c.body, false)
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("无口令 %s %s = %d, 期望 401", c.method, c.path, res.StatusCode)
		}
		if strings.Contains(body, "一条已经存在的待办") {
			t.Errorf("%s %s 的 401 泄露了待办正文: %s", c.method, c.path, body)
		}
	}
}

func TestTaskCreateGoesToHeadAndCounts(t *testing.T) {
	h := newHarness(t)
	first := h.createTask(t, "写周报")
	second := h.createTask(t, "约评审")
	if second.Rank != 1 {
		t.Fatalf("新建 rank = %d, 期望 1", second.Rank)
	}

	list := h.listTasks(t)
	if list.OpenCount != 2 || list.DoneCount != 0 {
		t.Fatalf("计数 = open %d done %d, 期望 2/0", list.OpenCount, list.DoneCount)
	}
	if list.Tasks[0].ID != second.ID || list.Tasks[1].ID != first.ID {
		t.Errorf("顺序 = %v, 期望新的在前", list.Tasks)
	}
}

func TestTaskPatchDoneMovesItToDoneSection(t *testing.T) {
	h := newHarness(t)
	a := h.createTask(t, "甲")
	b := h.createTask(t, "乙")

	res, raw := h.do(t, "PATCH", "/v1/tasks/"+strconv.FormatInt(a.ID, 10), `{"done":true}`, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PATCH done = %d %s", res.StatusCode, raw)
	}
	var got store.Task
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if !got.Done || got.DoneAt == nil {
		t.Fatalf("勾掉后 = done=%v done_at=%v", got.Done, got.DoneAt)
	}

	list := h.listTasks(t)
	if list.OpenCount != 1 || list.DoneCount != 1 {
		t.Fatalf("计数 = %d/%d, 期望 1/1", list.OpenCount, list.DoneCount)
	}
	// "第 6 名顶上"在接口层的形状：未完成收紧成一条，已完成的排到列表末尾。
	if list.Tasks[0].ID != b.ID || list.Tasks[0].Rank != 1 || !list.Tasks[1].Done {
		t.Errorf("列表形状不对: %v", list.Tasks)
	}
}

func TestTaskPatchBodyLimits(t *testing.T) {
	h := newHarness(t)
	created := h.createTask(t, "短的一条")
	path := "/v1/tasks/" + strconv.FormatInt(created.ID, 10)

	if res, body := h.do(t, "PATCH", path, `{"body":"   "}`, true); res.StatusCode != http.StatusBadRequest ||
		!strings.Contains(body, "不能为空") {
		t.Errorf("空正文 = %d %s", res.StatusCode, body)
	}
	// harness 里 MaxNoteChars = 20，超了回 413 而不是截断（design §5）。
	long := strings.Repeat("字", 21)
	res, body := h.do(t, "PATCH", path, `{"body":"`+long+`"}`, true)
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("超限 = %d %s, 期望 413", res.StatusCode, body)
	}
	if again := h.listTasks(t); again.Tasks[0].Body != "短的一条" {
		t.Errorf("超限的那次改了正文: %q", again.Tasks[0].Body)
	}
}

func TestTaskPatchNeedsSomething(t *testing.T) {
	h := newHarness(t)
	created := h.createTask(t, "改什么都不给")
	res, body := h.do(t, "PATCH", "/v1/tasks/"+strconv.FormatInt(created.ID, 10), `{}`, true)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "PATCH 要带") {
		t.Errorf("空 PATCH = %d %s", res.StatusCode, body)
	}
}

func TestTaskReorderRoundTrip(t *testing.T) {
	h := newHarness(t)
	jia := h.createTask(t, "甲").ID
	yi := h.createTask(t, "乙").ID
	bing := h.createTask(t, "丙").ID

	// 建完的实际 rank 是 丙乙甲，提交 甲丙乙 之后应当照单落地。
	res, raw := h.do(t, "POST", "/v1/tasks/reorder", idsParam(jia, bing, yi), true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reorder = %d %s", res.StatusCode, raw)
	}
	var out taskEnvelope
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("解析: %v", err)
	}
	for i, want := range []int64{jia, bing, yi} {
		if out.Tasks[i].ID != want || out.Tasks[i].Rank != i+1 {
			t.Errorf("第 %d 位 = id %d rank %d, 期望 id %d rank %d",
				i, out.Tasks[i].ID, out.Tasks[i].Rank, want, i+1)
		}
	}
}

func TestTaskReorderRejectsStaleList(t *testing.T) {
	h := newHarness(t)
	a := h.createTask(t, "甲")
	b := h.createTask(t, "乙")
	h.createTask(t, "丙")

	before := h.listTasks(t)

	// 只提交两条，第三条是另一台设备刚加的——服务端整笔拒绝，而不是把没提交的那条丢在旧位上。
	res, body := h.do(t, "POST", "/v1/tasks/reorder", idsParam(a.ID, b.ID), true)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("过期列表 = %d %s, 期望 400", res.StatusCode, body)
	}
	if !strings.Contains(body, "刷新后重拖") {
		t.Errorf("提示不说人话: %s", body)
	}
	if _, raw := h.do(t, "POST", "/v1/tasks/reorder", idsParam(999, 998), true); !strings.Contains(raw, "bad_request") {
		t.Fatalf("未知 id 没被拒: %s", raw)
	}

	after := h.listTasks(t)
	for i := range before.Tasks {
		if before.Tasks[i].ID != after.Tasks[i].ID || before.Tasks[i].Rank != after.Tasks[i].Rank {
			t.Errorf("被拒之后顺序变了: %v → %v", before.Tasks, after.Tasks)
			break
		}
	}
}

func TestTaskReorderEmptyAndBadID(t *testing.T) {
	h := newHarness(t)
	if res, body := h.do(t, "POST", "/v1/tasks/reorder", `{"ids":[]}`, true); res.StatusCode != http.StatusBadRequest {
		t.Errorf("空 ids = %d %s", res.StatusCode, body)
	}
	if res, body := h.do(t, "PATCH", "/v1/tasks/abc", `{"done":true}`, true); res.StatusCode != http.StatusBadRequest ||
		!strings.Contains(body, "正整数") {
		t.Errorf("非数字 id = %d %s", res.StatusCode, body)
	}
}

func TestTaskDelete(t *testing.T) {
	h := newHarness(t)
	a := h.createTask(t, "甲")
	h.createTask(t, "乙")

	res, _ := h.do(t, "DELETE", "/v1/tasks/"+strconv.FormatInt(a.ID, 10), "", true)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("删除 = %d, 期望 204", res.StatusCode)
	}
	// 幂等：已删除再删回 404，界面按"已经不在了"处理。
	res, body := h.do(t, "DELETE", "/v1/tasks/"+strconv.FormatInt(a.ID, 10), "", true)
	if res.StatusCode != http.StatusNotFound || !strings.Contains(body, "not_found") {
		t.Errorf("重复删除 = %d %s", res.StatusCode, body)
	}
	if got := h.listTasks(t); got.OpenCount != 1 {
		t.Errorf("删完剩 %d 条未完成, 期望 1", got.OpenCount)
	}
}

// seedTaskDone 直接走存储层勾掉一条待办，把完成时刻钉成调用方给的值。
// 走 HTTP 的话 done_at 只能是 s.now()，而报表测试要的是"落在某个指定周期里"。
func (h *harness) seedTaskDone(t *testing.T, body string, at time.Time) store.Task {
	t.Helper()
	ctx := context.Background()
	task, err := h.st.CreateTask(ctx, body, period.DayID(at, h.loc), at)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	done, err := h.st.SetTaskDone(ctx, task.ID, true, at,
		period.WeekStartID(at, h.loc), period.MonthStartID(at, h.loc))
	if err != nil {
		t.Fatalf("SetTaskDone: %v", err)
	}
	return done
}

func TestCompletedTaskCountsIntoWeekReport(t *testing.T) {
	h := newHarness(t)
	// harness 把生成侧的钟钉在 2026-11-25 10:00（UTC+8），所以素材都得落在那一刻之前，
	// 否则"生成中的周期取到此刻为止"这条规则会把刚勾掉的待办判给下一次生成。
	inWeek := time.Date(2026, time.November, 24, 9, 0, 0, 0, testTZ)

	h.seedNote(t, "把报表接口切到分页", inWeek)
	h.seedTaskDone(t, "联调完待办接口", inWeek)
	// 没勾掉的待办不代表任何交付，一条都不该进（FR-11）。
	h.createTask(t, "下周要补的备份脚本")
	// 上周勾掉的归上周。
	h.seedTaskDone(t, "上周就该做完的事", time.Date(2026, time.November, 17, 9, 0, 0, 0, testTZ))

	weekStart := period.WeekStartID(inWeek, h.loc)
	res, body := h.do(t, "POST", "/v1/reports/week/"+weekStart+"/generate", "", true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("生成 = %d %s", res.StatusCode, body)
	}
	var rep store.Report
	if err := json.Unmarshal([]byte(body), &rep); err != nil {
		t.Fatalf("解析报表: %v", err)
	}
	// source_count 从"该周期记录条数"变成"喂给模型的素材条数"：1 条记录 + 1 条本周完成的待办。
	if rep.Status != store.StatusReady || *rep.SourceCount != 2 {
		t.Fatalf("报表 = %s source_count %v, 期望 ready 且 2", rep.Status, *rep.SourceCount)
	}
}

func TestOnlyTasksNoNotesStillGenerates(t *testing.T) {
	h := newHarness(t)
	inWeek := time.Date(2026, time.November, 24, 9, 0, 0, 0, testTZ)
	h.seedTaskDone(t, "只做完了这一件", inWeek)

	weekStart := period.WeekStartID(inWeek, h.loc)
	res, body := h.do(t, "POST", "/v1/reports/week/"+weekStart+"/generate", "", true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("只有待办的周期出不了报: %d %s", res.StatusCode, body)
	}
}

func TestEmptyPeriodWithNoTasksStillConflicts(t *testing.T) {
	h := newHarness(t)
	// 两路素材都是 0 → 仍旧走 409 no_records，这条判据没被待办放宽成"永远能生成"。
	res, body := h.do(t, "POST", "/v1/reports/week/2026-11-16/generate", "", true)
	if res.StatusCode != http.StatusConflict || !strings.Contains(body, "no_records") {
		t.Errorf("空周期 = %d %s, 期望 409 no_records", res.StatusCode, body)
	}
}

func TestTaskDueOnDefaultsToServerLocalToday(t *testing.T) {
	h := newHarness(t)
	// 把接口的钟钉在 2026-11-25 10:00（UTC+8），缺省日期必须按这个时区的日历日算。
	h.api.now = func() time.Time { return time.Date(2026, time.November, 25, 10, 0, 0, 0, testTZ) }
	got := h.createTask(t, "没写日期的待办")
	if got.DueOn != "2026-11-25" {
		t.Errorf("缺省 due_on = %q, 期望 2026-11-25", got.DueOn)
	}
}

func TestTaskCreateAndPatchDueOn(t *testing.T) {
	h := newHarness(t)

	res, raw := h.do(t, "POST", "/v1/tasks", `{"body":"十二月的事","due_on":"2026-12-01"}`, true)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST 带日期 = %d %s", res.StatusCode, raw)
	}
	var created store.Task
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatalf("解析新建待办: %v — %s", err, raw)
	}
	if created.DueOn != "2026-12-01" {
		t.Errorf("创建后的 due_on = %q, 期望 2026-12-01", created.DueOn)
	}

	res, raw = h.do(t, "PATCH", "/v1/tasks/"+strconv.FormatInt(created.ID, 10), `{"due_on":"2026-12-24"}`, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PATCH due_on = %d %s", res.StatusCode, raw)
	}
	var patched store.Task
	if err := json.Unmarshal([]byte(raw), &patched); err != nil {
		t.Fatalf("解析改日期结果: %v — %s", err, raw)
	}
	if patched.DueOn != "2026-12-24" || patched.Rank != created.Rank || patched.Done != created.Done {
		t.Errorf("改日期后被牵连: due_on=%q rank=%d done=%v", patched.DueOn, patched.Rank, patched.Done)
	}

	list := h.listTasks(t)
	found := false
	for _, task := range list.Tasks {
		if task.ID == created.ID {
			found = true
			if task.DueOn != "2026-12-24" {
				t.Errorf("列表里的 due_on = %q, 期望 2026-12-24", task.DueOn)
			}
		}
	}
	if !found {
		t.Error("改过日期的待办从列表里消失了")
	}
}

func TestTaskDueOnRejectsBadDates(t *testing.T) {
	h := newHarness(t)
	created := h.createTask(t, "合法的一条")
	id := strconv.FormatInt(created.ID, 10)

	for _, bad := range []string{"2026-02-30", "2026-1-2", "2026-13-01", "明天"} {
		res, raw := h.do(t, "POST", "/v1/tasks", `{"body":"坏日期","due_on":"`+bad+`"}`, true)
		if res.StatusCode != http.StatusBadRequest || !strings.Contains(raw, "due_on") {
			t.Errorf("创建 due_on=%q = %d %s, 期望 400 且说明 due_on", bad, res.StatusCode, raw)
		}
		res, raw = h.do(t, "PATCH", "/v1/tasks/"+id, `{"due_on":"`+bad+`"}`, true)
		if res.StatusCode != http.StatusBadRequest || !strings.Contains(raw, "due_on") {
			t.Errorf("修改 due_on=%q = %d %s, 期望 400 且说明 due_on", bad, res.StatusCode, raw)
		}
	}

	// 不能清空：日历没有"未安排"这一格可放它（ADR-0021）。
	res, raw := h.do(t, "PATCH", "/v1/tasks/"+id, `{"due_on":""}`, true)
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("清空 due_on = %d %s, 期望 400", res.StatusCode, raw)
	}
	if again := h.listTasks(t); again.Tasks[0].DueOn != created.DueOn {
		t.Errorf("被拒的修改动了库里的日期: %q → %q", created.DueOn, again.Tasks[0].DueOn)
	}
}

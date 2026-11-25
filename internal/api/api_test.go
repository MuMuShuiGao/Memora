package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"memora/internal/auth"
	"memora/internal/config"
	"memora/internal/period"
	"memora/internal/report"
	"memora/internal/store"
)

const testPass = "本机测试用的口令"

var testTZ = time.FixedZone("UTC+8", 8*3600)

// stubCompleter 让接口测试不联网、也不要 API key（ADR-0003：生成只在服务端，
// 但"服务端"在测试里就是这个假实现）。
type stubCompleter struct {
	calls int
	fail  bool
}

func (s *stubCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	s.calls++
	if s.fail {
		return "", fmt.Errorf("stub: 模型说不能")
	}
	return "本周期完成\n- 接口测试通过", nil
}

type harness struct {
	st   *store.Store
	llm  *stubCompleter
	api  *Server
	srv  *httptest.Server
	loc  *time.Location
	http *http.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(testPass), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	cfg := &config.Config{
		DBPath:         filepath.Join(t.TempDir(), "unused.db"),
		PassphraseHash: string(hash),
		LocalTZ:        testTZ,
		LocalTZName:    "UTC+8",
		MaxNoteChars:   20,
	}
	gate, err := auth.NewGate(cfg.PassphraseHash)
	if err != nil {
		t.Fatalf("NewGate: %v", err)
	}

	llm := &stubCompleter{}
	clock := time.Date(2026, time.November, 25, 10, 0, 0, 0, testTZ)
	gen, err := report.NewGenerator(st, llm, testTZ, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	gen.SetClock(func() time.Time { return clock })

	api := New(st, cfg, gate, gen, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)

	return &harness{st: st, llm: llm, api: api, srv: srv, loc: testTZ, http: srv.Client()}
}

func (h *harness) do(t *testing.T, method, path, body string, withKey bool) (*http.Response, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if withKey {
		req.Header.Set("X-Access-Key", testPass)
	}
	res, err := h.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res, string(raw)
}

// seedNote 直接走存储层写一条，归属列按 period 包算——接口测试要钉的是 HTTP 行为，
// 不是重复验证归属算法。
func (h *harness) seedNote(t *testing.T, body string, at time.Time) store.Note {
	t.Helper()
	n, err := h.st.CreateNote(context.Background(), body, at,
		period.WeekStartID(at, h.loc), period.MonthStartID(at, h.loc))
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	return n
}

func TestHealthzNeedsNoPassphrase(t *testing.T) {
	h := newHarness(t)
	res, body := h.do(t, "GET", "/healthz", "", false)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "ok") {
		t.Errorf("healthz = %d %s", res.StatusCode, body)
	}
}

func TestNotesRequirePassphrase(t *testing.T) {
	h := newHarness(t)
	h.seedNote(t, "一条已存在的记录", time.Now())

	// FR-07：不带口令 → 401，且响应体里一个业务数据都没有。
	res, body := h.do(t, "GET", "/v1/notes", "", false)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无口令 = %d, 期望 401", res.StatusCode)
	}
	if strings.Contains(body, "一条已存在的记录") {
		t.Errorf("401 响应泄露了业务数据: %s", body)
	}
	if !strings.Contains(body, `"code":"unauthorized"`) {
		t.Errorf("错误形状不对: %s", body)
	}

	res, body = h.do(t, "GET", "/v1/notes", "", true)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "一条已存在的记录") {
		t.Errorf("带口令 = %d %s", res.StatusCode, body)
	}
}

func TestCreateNoteReturnsPeriod(t *testing.T) {
	h := newHarness(t)
	res, body := h.do(t, "POST", "/v1/notes", `{"body":"周报接口打通了"}`, true)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST = %d %s", res.StatusCode, body)
	}
	var n store.Note
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	// design §5：新建响应要直接带回算好的归属，客户端不必自己算。
	if n.WeekStart != "2026-11-23" || n.MonthStart != "2026-11-01" {
		t.Errorf("归属 = %s / %s, 期望 2026-11-23 / 2026-11-01（记录当下算，不按请求时间）",
			n.WeekStart, n.MonthStart)
	}
}

func TestNoteBodyLimits(t *testing.T) {
	h := newHarness(t) // MaxNoteChars = 20

	// 超限必须是 413，不是静默截断（design §5）。
	res, body := h.do(t, "POST", "/v1/notes", `{"body":"这条正文故意超过了二十个字符的上限限制再多加几个字"}`, true)
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("超长 = %d %s, 期望 413", res.StatusCode, body)
	}
	if !strings.Contains(body, `"too_large"`) {
		t.Errorf("错误码不对: %s", body)
	}

	// 恰好 20 个字符要能过——上限判的是"超过"，不是"达到"。
	exact := `{"body":"` + strings.Repeat("字", 20) + `"}`
	if res, body := h.do(t, "POST", "/v1/notes", exact, true); res.StatusCode != http.StatusCreated {
		t.Errorf("恰好 20 字 = %d %s", res.StatusCode, body)
	}

	res, body = h.do(t, "POST", "/v1/notes", `{"body":"   "}`, true)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "正文不能为空") {
		t.Errorf("空正文 = %d %s", res.StatusCode, body)
	}

	res, body = h.do(t, "POST", "/v1/notes", `{`, true)
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("坏 JSON = %d %s", res.StatusCode, body)
	}
}

func TestReportEndpoints(t *testing.T) {
	h := newHarness(t)
	h.seedNote(t, "上周做完了报表切片", time.Date(2026, time.November, 17, 9, 0, 0, 0, testTZ))

	// 还没生成过 → 404 not_generated，而不是回一份空正文。
	res, body := h.do(t, "GET", "/v1/reports/week/2026-11-16", "", true)
	if res.StatusCode != http.StatusNotFound || !strings.Contains(body, "not_generated") {
		t.Fatalf("未生成 = %d %s", res.StatusCode, body)
	}

	// 手动生成 → 200 + ready 正文 + 条数。
	res, body = h.do(t, "POST", "/v1/reports/week/2026-11-16/generate", "", true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("generate = %d %s", res.StatusCode, body)
	}
	if h.llm.calls != 1 {
		t.Errorf("模型调用次数 = %d, 期望 1", h.llm.calls)
	}
	var rep store.Report
	if err := json.Unmarshal([]byte(body), &rep); err != nil {
		t.Fatalf("解析报表: %v\n%s", err, body)
	}
	if rep.Status != store.StatusReady || *rep.SourceCount != 1 || rep.PeriodEnd != "2026-11-22" {
		t.Errorf("报表不对: %+v", rep)
	}

	// 单份能读回来。
	res, body = h.do(t, "GET", "/v1/reports/week/2026-11-16", "", true)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "接口测试通过") {
		t.Errorf("读回 = %d %s", res.StatusCode, body)
	}

	// 列表带 status，客户端靠它显示不同文案。
	res, body = h.do(t, "GET", "/v1/reports?kind=week", "", true)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"status":"ready"`) {
		t.Errorf("列表 = %d %s", res.StatusCode, body)
	}
}

func TestReportParameterValidation(t *testing.T) {
	h := newHarness(t)
	h.seedNote(t, "本周的事", time.Date(2026, time.November, 25, 9, 0, 0, 0, testTZ))

	// 周报起始日必须是周一、月报必须是月初：存错了就再也查不到（design §1 的 UNIQUE）。
	res, body := h.do(t, "POST", "/v1/reports/week/2026-11-24/generate", "", true)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "bad_period") {
		t.Errorf("周二当周首 = %d %s", res.StatusCode, body)
	}
	res, body = h.do(t, "POST", "/v1/reports/month/2026-11-16/generate", "", true)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "bad_period") {
		t.Errorf("非月初当月首 = %d %s", res.StatusCode, body)
	}
	res, body = h.do(t, "GET", "/v1/reports/day/2026-11-23", "", true)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "kind") {
		t.Errorf("未知 kind = %d %s", res.StatusCode, body)
	}
	res, body = h.do(t, "GET", "/v1/reports/week/2026-11-24", "", true)
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("读接口也该挡非周一: %d %s", res.StatusCode, body)
	}
	if h.llm.calls != 0 {
		t.Errorf("参数不合法却打了模型, calls=%d", h.llm.calls)
	}
}

func TestGenerateEmptyPeriodIsConflict(t *testing.T) {
	h := newHarness(t)
	h.seedNote(t, "本周记的", time.Date(2026, time.November, 25, 9, 0, 0, 0, testTZ))

	res, body := h.do(t, "POST", "/v1/reports/week/2026-09-07/generate", "", true)
	if res.StatusCode != http.StatusConflict || !strings.Contains(body, "no_records") {
		t.Errorf("空周期 = %d %s, 期望 409 no_records", res.StatusCode, body)
	}
	if h.llm.calls != 0 {
		t.Errorf("空周期不该打模型, calls=%d", h.llm.calls)
	}
}

func TestGenerateFailurePersistsFailed(t *testing.T) {
	h := newHarness(t)
	h.seedNote(t, "上周记的", time.Date(2026, time.November, 17, 9, 0, 0, 0, testTZ))
	h.llm.fail = true

	res, body := h.do(t, "POST", "/v1/reports/week/2026-11-16/generate", "", true)
	if res.StatusCode != http.StatusBadGateway || !strings.Contains(body, "generation_failed") {
		t.Fatalf("生成失败 = %d %s", res.StatusCode, body)
	}
	// 重试 1 次，一共两次调用。
	if h.llm.calls != 2 {
		t.Errorf("调用次数 = %d, 期望 2", h.llm.calls)
	}
	// 失败必须留得下来，客户端事后能读到 failed + 原因（FR-05 不静默缺失）。
	res, body = h.do(t, "GET", "/v1/reports/week/2026-11-16", "", true)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"status":"failed"`) {
		t.Errorf("失败态读不回: %d %s", res.StatusCode, body)
	}
	if !strings.Contains(body, "模型说不能") {
		t.Errorf("失败原因丢了: %s", body)
	}
}

// 客户端要点"生成本周"就得交出 period_start。这条测试钉的是：接口给的那一套
// 和服务端写记录时算的归属、和生成接口认的入参，三者必须是同一个答案。
func TestPeriodsNowAgreesWithOwnership(t *testing.T) {
	h := newHarness(t)
	// 周三上午十点（UTC+8）：本周一 2026-11-23，本月 2026-11-01。
	h.api.now = func() time.Time { return time.Date(2026, time.November, 25, 10, 0, 0, 0, testTZ) }

	res, body := h.do(t, "GET", "/v1/periods/now", "", false)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("不带口令 = %d %s, 期望 401", res.StatusCode, body)
	}

	res, body = h.do(t, "GET", "/v1/periods/now", "", true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("带口令 = %d %s", res.StatusCode, body)
	}
	var p periodsResponse
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("解析 %s: %v", body, err)
	}
	if p.WeekStart != "2026-11-23" || p.WeekEnd != "2026-11-29" {
		t.Errorf("周区间 = %s 至 %s, 期望 2026-11-23 至 2026-11-29", p.WeekStart, p.WeekEnd)
	}
	if p.MonthStart != "2026-11-01" || p.MonthEnd != "2026-11-30" {
		t.Errorf("月区间 = %s 至 %s, 期望 2026-11-01 至 2026-11-30", p.MonthStart, p.MonthEnd)
	}
	if p.LocalTZ != "UTC+8" {
		t.Errorf("local_tz = %q, 期望配置里那个 UTC+8", p.LocalTZ)
	}
	if now, err := time.Parse(time.RFC3339, p.Now); err != nil {
		t.Errorf("now 不是 RFC3339: %q (%v)", p.Now, err)
	} else if !now.Equal(time.Date(2026, time.November, 25, 10, 0, 0, 0, testTZ)) {
		t.Errorf("now = %v, 期望钉住的那个时刻", now)
	}

	// 同一个钟下新建的记录，归属必须跟接口说的一致。
	res, body = h.do(t, "POST", "/v1/notes", `{"body":"先看接口给的周期"}`, true)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("新建 = %d %s", res.StatusCode, body)
	}
	var n store.Note
	if err := json.Unmarshal([]byte(body), &n); err != nil {
		t.Fatalf("解析记录: %v", err)
	}
	if n.WeekStart != p.WeekStart || n.MonthStart != p.MonthStart {
		t.Errorf("记录归属 %s/%s 与接口 %s/%s 不一致",
			n.WeekStart, n.MonthStart, p.WeekStart, p.MonthStart)
	}

	// 客户端拿它直接调生成，必须认。
	res, body = h.do(t, "POST", "/v1/reports/week/"+p.WeekStart+"/generate", "", true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("用接口给的 week_start 生成 = %d %s", res.StatusCode, body)
	}
	if !strings.Contains(body, `"status":"ready"`) {
		t.Errorf("生成结果不是 ready: %s", body)
	}
}

func TestReportsRequirePassphraseToo(t *testing.T) {
	h := newHarness(t)
	for _, req := range [][2]string{
		{"GET", "/v1/reports"},
		{"GET", "/v1/reports/week/2026-11-16"},
		{"POST", "/v1/reports/week/2026-11-16/generate"},
	} {
		res, body := h.do(t, req[0], req[1], "", false)
		if res.StatusCode != http.StatusUnauthorized || strings.Contains(body, "本周期") {
			t.Errorf("%s %s = %d %s, 期望 401 且无业务数据", req[0], req[1], res.StatusCode, body)
		}
	}
}

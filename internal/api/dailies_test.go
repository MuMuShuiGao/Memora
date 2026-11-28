package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"memora/internal/store"
)

func TestDailyPutGetRoundTrip(t *testing.T) {
	h := newHarness(t)
	h.api.now = func() time.Time { return time.Date(2026, time.November, 25, 21, 0, 0, 0, testTZ) }

	// 没写过 → 404 not_found，客户端按空表单显示，不算错误。
	res, body := h.do(t, "GET", "/v1/dailies/2026-11-25", "", true)
	if res.StatusCode != http.StatusNotFound || !strings.Contains(body, `"code":"not_found"`) {
		t.Fatalf("未写 = %d %s, 期望 404 not_found", res.StatusCode, body)
	}

	in := `{"priorities":"1. 把日报接口接上\n2. 写提示词","done":"接口和提示词都通了","risk":"等模型 key"}`
	res, body = h.do(t, "PUT", "/v1/dailies/2026-11-25", in, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT = %d %s", res.StatusCode, body)
	}
	var d store.Daily
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		t.Fatalf("解析日报: %v\n%s", err, body)
	}
	if d.EntryDay != "2026-11-25" || !strings.Contains(d.Priorities, "写提示词") || d.Done == "" || d.Risk == "" {
		t.Errorf("返回的日报不对: %+v", d)
	}

	res, body = h.do(t, "GET", "/v1/dailies/2026-11-25", "", true)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "接口和提示词都通了") {
		t.Errorf("读回 = %d %s", res.StatusCode, body)
	}

	// 同一天再 PUT 是覆盖，不产生第二条。
	firstID := d.ID
	res, body = h.do(t, "PUT", "/v1/dailies/2026-11-25", `{"priorities":"改过的","done":"","risk":""}`, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("覆盖 = %d %s", res.StatusCode, body)
	}
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		t.Fatalf("解析覆盖结果: %v", err)
	}
	if d.ID != firstID || d.Priorities != "改过的" || d.Done != "" || d.Risk != "" {
		t.Errorf("覆盖结果 = %+v, 期望同一条且三节整换", d)
	}
}

func TestDailyValidation(t *testing.T) {
	h := newHarness(t)

	for _, day := range []string{"2026-11-31", "2026-2-03", "today"} {
		res, body := h.do(t, "PUT", "/v1/dailies/"+day, `{"priorities":"x"}`, true)
		if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "YYYY-MM-DD") {
			t.Errorf("非法日期 %s = %d %s", day, res.StatusCode, body)
		}
	}

	// 每节按码点算 2000，超了是 413，不静默截断。
	long := `{"priorities":"` + strings.Repeat("字", 2001) + `"}`
	res, body := h.do(t, "PUT", "/v1/dailies/2026-11-25", long, true)
	if res.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(body, "too_large") {
		t.Errorf("超长 = %d %s, 期望 413 too_large", res.StatusCode, body)
	}

	// 三节全空可以存：等于这天没有素材，客户端也允许清空。
	res, body = h.do(t, "PUT", "/v1/dailies/2026-11-25", `{"priorities":"  ","done":"","risk":""}`, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("全空 = %d %s", res.StatusCode, body)
	}
	var d store.Daily
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if d.Priorities != "" || d.Done != "" || d.Risk != "" {
		t.Errorf("全空保存应清掉三节: %+v", d)
	}
}

func TestDailiesRequirePassphrase(t *testing.T) {
	h := newHarness(t)
	for _, req := range [][2]string{
		{"GET", "/v1/dailies/2026-11-25"},
		{"PUT", "/v1/dailies/2026-11-25"},
	} {
		res, body := h.do(t, req[0], req[1], `{"priorities":"x"}`, false)
		if res.StatusCode != http.StatusUnauthorized || strings.Contains(body, "x") {
			t.Errorf("%s = %d %s, 期望 401 且无业务数据", req[0], res.StatusCode, body)
		}
	}
}

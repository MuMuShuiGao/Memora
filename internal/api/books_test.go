package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"memora/internal/store"
)

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func TestBooksCRUD(t *testing.T) {
	h := newHarness(t)

	// 书名与作者首尾去空白；月份原样收下。
	res, body := h.do(t, "POST", "/v1/books",
		`{"title":"  深度工作  ","author":" 卡尔·纽波特 ","finished_month":"2026-10"}`, true)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST = %d %s", res.StatusCode, body)
	}
	var oct store.Book
	if err := json.Unmarshal([]byte(body), &oct); err != nil {
		t.Fatalf("解析新建响应: %v\n%s", err, body)
	}
	if oct.Title != "深度工作" || oct.Author != "卡尔·纽波特" || oct.FinishedMonth != "2026-10" {
		t.Errorf("新建 = %+v, 期望去空白后的三项", oct)
	}

	// 不带月份落服务端当月（测试钟钉在 2026-11-25）。
	res, body = h.do(t, "POST", "/v1/books", `{"title":"默认月份的书"}`, true)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST 缺省月份 = %d %s", res.StatusCode, body)
	}
	var nov store.Book
	if err := json.Unmarshal([]byte(body), &nov); err != nil {
		t.Fatalf("解析: %v\n%s", err, body)
	}
	if nov.FinishedMonth != "2026-11" || nov.Author != "" {
		t.Errorf("缺省 = %+v, 期望月份 2026-11、作者空", nov)
	}

	// 列表按月倒序：十一月的那本在前。
	res, body = h.do(t, "GET", "/v1/books", "", true)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"count":2`) {
		t.Fatalf("GET = %d %s", res.StatusCode, body)
	}
	if strings.Index(body, "默认月份的书") > strings.Index(body, "深度工作") {
		t.Errorf("列表顺序不对: %s", body)
	}

	// 只带心得：其余字段不动。
	res, body = h.do(t, "PATCH", "/v1/books/"+itoa(oct.ID), `{"takeaway":"注意力是稀缺资源"}`, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PATCH 心得 = %d %s", res.StatusCode, body)
	}
	var patched store.Book
	if err := json.Unmarshal([]byte(body), &patched); err != nil {
		t.Fatalf("解析: %v\n%s", err, body)
	}
	if patched.Takeaway != "注意力是稀缺资源" || patched.Title != "深度工作" || patched.FinishedMonth != "2026-10" {
		t.Errorf("PATCH 后 = %+v, 期望只动心得", patched)
	}

	// 改名、改月、改作者可以一次带上。
	res, body = h.do(t, "PATCH", "/v1/books/"+itoa(oct.ID),
		`{"title":"深度工作（重读）","author":"","finished_month":"2026-12"}`, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PATCH 三项 = %d %s", res.StatusCode, body)
	}
	if err := json.Unmarshal([]byte(body), &patched); err != nil {
		t.Fatalf("解析: %v\n%s", err, body)
	}
	if patched.Title != "深度工作（重读）" || patched.Author != "" || patched.FinishedMonth != "2026-12" ||
		patched.Takeaway != "注意力是稀缺资源" {
		t.Errorf("多项 PATCH = %+v", patched)
	}

	// 空串心得等于清掉，书还在。
	res, body = h.do(t, "PATCH", "/v1/books/"+itoa(oct.ID), `{"takeaway":""}`, true)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("清空心得 = %d %s", res.StatusCode, body)
	}
	if err := json.Unmarshal([]byte(body), &patched); err != nil {
		t.Fatalf("解析: %v\n%s", err, body)
	}
	if patched.Takeaway != "" || patched.Title != "深度工作（重读）" {
		t.Errorf("清空后 = %+v, 期望只清心得", patched)
	}

	// 删除后 404，再删还是 404。
	res, body = h.do(t, "DELETE", "/v1/books/"+itoa(oct.ID), "", true)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s", res.StatusCode, body)
	}
	res, body = h.do(t, "DELETE", "/v1/books/"+itoa(oct.ID), "", true)
	if res.StatusCode != http.StatusNotFound || !strings.Contains(body, "not_found") {
		t.Errorf("重复删除 = %d %s, 期望 404", res.StatusCode, body)
	}
	res, body = h.do(t, "PATCH", "/v1/books/"+itoa(oct.ID), `{"title":"改不了"}`, true)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("改已删除的书 = %d %s, 期望 404", res.StatusCode, body)
	}
}

func TestBookValidation(t *testing.T) {
	h := newHarness(t)

	bad := []struct {
		name string
		body string
	}{
		{"空书名", `{"title":"   "}`},
		{"无书名", `{"author":"只有作者"}`},
		{"书名超长", `{"title":"` + strings.Repeat("书", maxBookTitleChars+1) + `"}`},
		{"作者超长", `{"title":"书", "author":"` + strings.Repeat("人", maxBookAuthorChars+1) + `"}`},
		{"月份无零填充", `{"title":"书", "finished_month":"2026-1"}`},
		{"月份越界", `{"title":"书", "finished_month":"2026-13"}`},
		{"月份带日", `{"title":"书", "finished_month":"2026-11-01"}`},
		{"月份不是日期", `{"title":"书", "finished_month":"abc"}`},
	}
	for _, c := range bad {
		res, body := h.do(t, "POST", "/v1/books", c.body, true)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d %s, 期望 400", c.name, res.StatusCode, body)
		}
	}

	// 恰好到上限要能过：上限判的是"超过"。
	res, body := h.do(t, "POST", "/v1/books",
		`{"title":"`+strings.Repeat("书", maxBookTitleChars)+`","author":"`+strings.Repeat("人", maxBookAuthorChars)+`"}`, true)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("恰好到上限 = %d %s", res.StatusCode, body)
	}
	var book store.Book
	if err := json.Unmarshal([]byte(body), &book); err != nil {
		t.Fatalf("解析: %v\n%s", err, body)
	}

	patchBad := []struct {
		name string
		body string
	}{
		{"空 PATCH", `{}`},
		{"清空书名", `{"title":""}`},
		{"清空月份", `{"finished_month":""}`},
		{"心得超长", `{"takeaway":"` + strings.Repeat("字", maxBookTakeawayChars+1) + `"}`},
		{"坏月份", `{"finished_month":"2026-00"}`},
	}
	for _, c := range patchBad {
		res, body := h.do(t, "PATCH", "/v1/books/"+itoa(book.ID), c.body, true)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d %s, 期望 400", c.name, res.StatusCode, body)
		}
	}

	// 恰好 200 字的心得能存下。
	res, body = h.do(t, "PATCH", "/v1/books/"+itoa(book.ID),
		`{"takeaway":"`+strings.Repeat("字", maxBookTakeawayChars)+`"}`, true)
	if res.StatusCode != http.StatusOK {
		t.Errorf("200 字心得 = %d %s", res.StatusCode, body)
	}
}

func TestBooksRequirePassphrase(t *testing.T) {
	h := newHarness(t)
	for _, req := range [][2]string{
		{"GET", "/v1/books"},
		{"POST", "/v1/books"},
		{"PATCH", "/v1/books/1"},
		{"DELETE", "/v1/books/1"},
	} {
		res, body := h.do(t, req[0], req[1], "", false)
		if res.StatusCode != http.StatusUnauthorized || strings.Contains(body, `"books"`) {
			t.Errorf("%s %s = %d %s, 期望 401 且无业务数据", req[0], req[1], res.StatusCode, body)
		}
	}
}

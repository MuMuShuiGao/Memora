package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"memora/internal/period"
	"memora/internal/store"
)

// maxDailyChars 是日报每一节的字数上限（ADR-0031）。三段合计最长 6000 码点，
// 按 UTF-8 最坏四字节算约 24KB，所以日报请求体单独放宽到 32KB。
const (
	maxDailyChars     = 2000
	maxDailyBodyBytes = 32 * 1024
)

type dailyWrite struct {
	Priorities string `json:"priorities"`
	Done       string `json:"done"`
	Risk       string `json:"risk"`
}

func (s *Server) getDaily(w http.ResponseWriter, r *http.Request) {
	day, ok := s.dailyDay(w, r)
	if !ok {
		return
	}
	d, err := s.st.GetDaily(r.Context(), day)
	if errors.Is(err, store.ErrNotFound) {
		// 没写过不是错误：客户端按空表单显示，提示语都还在（ADR-0031）。
		s.fail(w, http.StatusNotFound, "not_found", "这天还没写日报")
		return
	}
	if err != nil {
		s.serverError(w, r, "取日报", err)
		return
	}
	s.ok(w, d)
}

func (s *Server) putDaily(w http.ResponseWriter, r *http.Request) {
	day, ok := s.dailyDay(w, r)
	if !ok {
		return
	}
	var in dailyWrite
	if !s.readJSONLimit(w, r, &in, maxDailyBodyBytes) {
		return
	}

	priorities := strings.TrimSpace(in.Priorities)
	done := strings.TrimSpace(in.Done)
	risk := strings.TrimSpace(in.Risk)
	for _, field := range []struct {
		name  string
		value string
	}{
		{"优先级", priorities},
		{"做成了什么", done},
		{"卡点", risk},
	} {
		if utf8.RuneCountInString(field.value) > maxDailyChars {
			s.fail(w, http.StatusRequestEntityTooLarge, "too_large", field.name+"最多 2000 个字")
			return
		}
	}

	d, err := s.st.UpsertDaily(r.Context(), day, priorities, done, risk, s.now())
	if err != nil {
		s.serverError(w, r, "写日报", err)
		return
	}
	s.ok(w, d)
}

func (s *Server) dailyDay(w http.ResponseWriter, r *http.Request) (string, bool) {
	day := strings.TrimSpace(r.PathValue("day"))
	if !period.IsDayID(day) {
		s.fail(w, http.StatusBadRequest, "bad_request", "day 要 YYYY-MM-DD 的自然日")
		return "", false
	}
	return day, true
}

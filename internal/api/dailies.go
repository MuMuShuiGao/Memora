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
// maxDailyRangeDays 是一次区间查询的天数上限（ADR-0032）：日历一个网格 42 天，
// 366 天留足翻年，又不让一次请求把整库日报都拖出来。
const (
	maxDailyChars     = 2000
	maxDailyBodyBytes = 32 * 1024
	maxDailyRangeDays = 366
)

type dailyWrite struct {
	Priorities string `json:"priorities"`
	Done       string `json:"done"`
	Risk       string `json:"risk"`
}

type dailyDaysResponse struct {
	Days  []string `json:"days"`
	Today string   `json:"today"`
	Count int      `json:"count"`
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

// listDailyDays 给日历一次取一个网格范围内"有日报"的日期（ADR-0032）。
// today 一并回来，日历据此不显示未来日的日报入口，省一次 periods/now 请求。
func (s *Server) listDailyDays(w http.ResponseWriter, r *http.Request) {
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if !period.IsDayID(from) || !period.IsDayID(to) {
		s.fail(w, http.StatusBadRequest, "bad_request", "from 与 to 都要 YYYY-MM-DD 的自然日")
		return
	}
	span := period.DaysBetween(from, to)
	if span == 0 {
		s.fail(w, http.StatusBadRequest, "bad_request", "from 不能晚于 to")
		return
	}
	if span > maxDailyRangeDays {
		s.fail(w, http.StatusBadRequest, "bad_request", "一次最多取 366 天")
		return
	}
	end, err := period.DayAfter(to)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "bad_request", "to 要 YYYY-MM-DD 的自然日")
		return
	}
	rows, err := s.st.DailiesBetween(r.Context(), from, end)
	if err != nil {
		s.serverError(w, r, "列日报日期", err)
		return
	}
	out := dailyDaysResponse{
		Days:  make([]string, 0, len(rows)),
		Today: period.DayID(s.now(), s.cfg.LocalTZ),
	}
	for _, d := range rows {
		out.Days = append(out.Days, d.EntryDay)
	}
	out.Count = len(out.Days)
	s.ok(w, out)
}

func (s *Server) putDaily(w http.ResponseWriter, r *http.Request) {
	day, ok := s.dailyDay(w, r)
	if !ok {
		return
	}
	// 只补记不写未来（ADR-0032）。日历入口也按服务端 today 藏未来日，
	// 这里兜底，防止绕过界面的调用方把未来日报写进库。
	if today := period.DayID(s.now(), s.cfg.LocalTZ); day > today {
		s.fail(w, http.StatusBadRequest, "bad_request", "还不能写未来，最晚写到今天")
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

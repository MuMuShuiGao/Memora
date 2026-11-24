package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"memora/internal/period"
	"memora/internal/report"
	"memora/internal/store"
)

const (
	defaultReportLimit = 30
	maxReportLimit     = 200
)

func (s *Server) listReports(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	kind := q.Get("kind")
	if kind != "" {
		if _, err := report.ParseKind(kind); err != nil {
			s.fail(w, http.StatusBadRequest, "bad_request", "kind 只认 week 或 month")
			return
		}
	}

	limit := defaultReportLimit
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			s.fail(w, http.StatusBadRequest, "bad_request", "limit 要是正整数")
			return
		}
		if n > maxReportLimit {
			n = maxReportLimit
		}
		limit = n
	}

	reports, err := s.st.ListReports(r.Context(), kind, limit)
	if err != nil {
		s.serverError(w, r, "报表列表", err)
		return
	}
	s.ok(w, map[string]any{"reports": reports, "count": len(reports)})
}

// getReport 读单份。库里没有这行意味着"从没生成过"，design §5 要求回 404 + not_generated，
// 而不是回一份空正文——两者在客户端要显示完全不同的文案。
func (s *Server) getReport(w http.ResponseWriter, r *http.Request) {
	kind, periodStart, ok := s.reportKey(w, r)
	if !ok {
		return
	}

	rep, err := s.st.GetReport(r.Context(), string(kind), periodStart)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.fail(w, http.StatusNotFound, "not_generated", "这个周期还没生成过报表")
	case err != nil:
		s.serverError(w, r, "取报表", err)
	default:
		s.ok(w, rep)
	}
}

// generateReport 同步跑一次生成（ADR-0004 的手动那一半：重算与看未结束的周期）。
// 外部模型慢是预期内的，所以这里不加额外超时——Generator 内部已有 60s 一次的上限。
func (s *Server) generateReport(w http.ResponseWriter, r *http.Request) {
	kind, periodStart, ok := s.reportKey(w, r)
	if !ok {
		return
	}

	// 手动生成允许针对进行中的周期，取记录的界限由 Generator 在读完那一刻定下。
	rep, err := s.gen.Generate(r.Context(), kind, periodStart)
	switch {
	case errors.Is(err, report.ErrNoRecords):
		s.fail(w, http.StatusConflict, "no_records", "这个周期一条记录都没有，不生成")
	case errors.Is(err, report.ErrNotConfigured):
		s.fail(w, http.StatusServiceUnavailable, "llm_not_configured", err.Error())
	case err != nil:
		// 失败也已经落库成 status=failed，正文里带上原因，客户端能直接显示（FR-05）
		s.log.Error("手动生成失败", "kind", string(kind), "period", periodStart, "err", err.Error())
		s.write(w, http.StatusBadGateway, map[string]any{
			"error":  errorObj{Code: "generation_failed", Message: "生成失败，报表已标记为 failed"},
			"report": rep,
		})
	default:
		s.write(w, http.StatusOK, rep)
	}
}

// checkPeriodStart 把起始日和报表种类对一下。归属算法只在 period 包里，
// 这里只负责把它的两种错误翻译成给客户端看的话。
func (s *Server) checkPeriodStart(kind report.Kind, start string) error {
	var (
		_, _, err error
		hint      string
	)
	if kind == report.Week {
		_, _, err = period.WeekRange(start, s.cfg.LocalTZ)
		hint = "周报的 period_start 要是周一"
	} else {
		_, _, err = period.MonthRange(start, s.cfg.LocalTZ)
		hint = "月报的 period_start 要是当月 1 号"
	}
	if err != nil {
		return errors.New(hint)
	}
	return nil
}

func (s *Server) reportKey(w http.ResponseWriter, r *http.Request) (report.Kind, string, bool) {
	kind, err := report.ParseKind(r.PathValue("kind"))
	if err != nil {
		s.fail(w, http.StatusBadRequest, "bad_request", "kind 只认 week 或 month")
		return "", "", false
	}

	start := r.PathValue("period_start")
	if _, err := time.Parse("2006-01-02", start); err != nil {
		s.fail(w, http.StatusBadRequest, "bad_request", "period_start 要 YYYY-MM-DD")
		return "", "", false
	}
	// 周报表必须给周一、月报表必须给月初，否则那份数据会存成一个谁也查不到的周期。
	if err := s.checkPeriodStart(kind, start); err != nil {
		s.fail(w, http.StatusBadRequest, "bad_period", err.Error())
		return "", "", false
	}
	return kind, start, true
}

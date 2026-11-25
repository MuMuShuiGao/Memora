package api

import (
	"net/http"
	"time"

	"memora/internal/period"
)

// periodsResponse 是"现在算哪个周期"的答案。起止日都是含端点的那一天，
// 与 reports 表里 period_start / period_end 的口径完全一致，客户端可以直接拿去调生成接口。
type periodsResponse struct {
	Now        string `json:"now"`
	LocalTZ    string `json:"local_tz"`
	WeekStart  string `json:"week_start"`
	WeekEnd    string `json:"week_end"`
	MonthStart string `json:"month_start"`
	MonthEnd   string `json:"month_end"`
}

// periodsNow 存在的全部理由：归属算法只准住在 internal/period 一处（design §2）。
// 客户端点"生成本周"要交出 period_start，如果它自己算周一，这条规则就有了第二份实现，
// 而 Kotlin 那份没有任何测试能发现它跟服务端不一致——冒烟也测不到它。
func (s *Server) periodsNow(w http.ResponseWriter, r *http.Request) {
	loc := s.cfg.LocalTZ
	now := s.now().In(loc)

	weekStart := period.WeekStartID(now, loc)
	monthStart := period.MonthStartID(now, loc)
	// 上面两个函数出来的日期必然分别是周一和月初，EndID 的校验在这里不可能失败。
	weekEnd, _ := period.WeekEndID(weekStart, loc)
	monthEnd, _ := period.MonthEndID(monthStart, loc)

	s.ok(w, periodsResponse{
		Now:        now.Format(time.RFC3339),
		LocalTZ:    s.cfg.LocalTZName,
		WeekStart:  weekStart,
		WeekEnd:    weekEnd,
		MonthStart: monthStart,
		MonthEnd:   monthEnd,
	})
}

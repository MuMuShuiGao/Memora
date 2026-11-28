// Package period 把"自然周/自然月"这条业务口径算成可持久化的字符串标识。
//
// 归属只在记录创建那一刻算一次，之后永不重算（design §2），所以这里必须是
// 不依赖当前时间的纯函数——否则同一条记录会在不同时刻算出不同周期。
package period

import (
	"errors"
	"fmt"
	"time"
)

const dateLayout = "2006-01-02"

var (
	ErrNotMonday     = errors.New("period: 起始日不是周一")
	ErrNotMonthStart = errors.New("period: 起始日不是月初")
)

// WeekStartID 返回 t 所在自然周的周一（按 loc 的日历计），格式 YYYY-MM-DD。
func WeekStartID(t time.Time, loc *time.Location) string {
	lt := t.In(loc)
	y, m, d := lt.Date()
	offset := (int(lt.Weekday()) + 6) % 7 // 周日 Weekday()=0，归属上一个周一
	return formatDate(time.Date(y, m, d-offset, 0, 0, 0, 0, loc))
}

// MonthStartID 返回 t 所在自然月的 1 号（按 loc 的日历计），格式 YYYY-MM-01。
func MonthStartID(t time.Time, loc *time.Location) string {
	y, m, _ := t.In(loc).Date()
	return formatDate(time.Date(y, m, 1, 0, 0, 0, 0, loc))
}

// DayID 返回 t 在 loc 里的那个自然日，格式 YYYY-MM-DD。待办的安排日期用它
// （ADR-0021）：只到天，不带时分，也不做时区换算以外的解释。
func DayID(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(dateLayout)
}

// IsDayID 只认带零填充的 YYYY-MM-DD，且必须是真实存在的日期（2 月 30 日不算）。
func IsDayID(s string) bool {
	t, err := time.Parse(dateLayout, s)
	return err == nil && formatDate(t) == s
}

// DayAfter 返回 dayID 的下一个自然日。日报区间接口的 to 是含端点的，
// 折成 store 的半开区间右端要用它；跨月、跨年、闰日都由 AddDate 处理。
func DayAfter(dayID string) (string, error) {
	t, err := parseDate(dayID, time.UTC)
	if err != nil {
		return "", err
	}
	return formatDate(t.AddDate(0, 0, 1)), nil
}

// DaysBetween 返回 from 到 to（含两端）的自然日天数。区间接口用它卡
// "一次最多取 366 天"；两端不合法或 from 晚于 to 时返回 0。
func DaysBetween(fromID, toID string) int {
	from, err := parseDate(fromID, time.UTC)
	if err != nil {
		return 0
	}
	to, err := parseDate(toID, time.UTC)
	if err != nil || to.Before(from) {
		return 0
	}
	return int(to.Sub(from).Hours()/24) + 1
}

// WeekRange 把 WeekStartID 的结果还原成 [周一 00:00, 下周一 00:00) 的半开区间。
// 用半开而不是 23:59:59，是为了不让最后一秒内的毫秒级记录漏掉。
func WeekRange(startID string, loc *time.Location) (start, end time.Time, err error) {
	monday, err := parseDate(startID, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if int(monday.In(loc).Weekday()) != int(time.Monday) {
		return time.Time{}, time.Time{}, ErrNotMonday
	}
	return monday, monday.AddDate(0, 0, 7), nil
}

// MonthRange 把 MonthStartID 的结果还原成 [1 号 00:00, 下月 1 号 00:00) 的半开区间。
func MonthRange(startID string, loc *time.Location) (start, end time.Time, err error) {
	first, err := parseDate(startID, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if d := first.In(loc).Day(); d != 1 {
		return time.Time{}, time.Time{}, ErrNotMonthStart
	}
	y, m, _ := first.In(loc).Date()
	return first, time.Date(y, m+1, 1, 0, 0, 0, 0, loc), nil
}

// WeekEndID 返回该周最后一天的日期（周日，含）。Range 给的是半开区间的右端，
// 落库的 period_end 要的是"这一天为止"，所以退一天。
func WeekEndID(startID string, loc *time.Location) (string, error) {
	_, end, err := WeekRange(startID, loc)
	if err != nil {
		return "", err
	}
	return formatDate(end.AddDate(0, 0, -1)), nil
}

// MonthEndID 返回该月最后一天（含），闰年 2 月会给出 29 号。
func MonthEndID(startID string, loc *time.Location) (string, error) {
	_, end, err := MonthRange(startID, loc)
	if err != nil {
		return "", err
	}
	return formatDate(end.AddDate(0, 0, -1)), nil
}

func formatDate(t time.Time) string {
	y, m, d := t.Date()
	return fmt.Sprintf("%04d-%02d-%02d", y, int(m), d)
}

func parseDate(s string, loc *time.Location) (time.Time, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("period: 解析日期 %q: %w", s, err)
	}
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc), nil
}

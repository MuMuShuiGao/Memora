package period

import (
	"errors"
	"testing"
	"time"
)

// 用固定偏移而不是 IANA 名字：测试不该依赖宿主机的 tzdata 是否装全。
var plus8 = time.FixedZone("UTC+8", 8*3600)

func at(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, plus8)
}

func TestWeekStartID(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"周二", at(2026, time.November, 24, 10, 0), "2026-11-23"},
		{"周一本身", at(2026, time.November, 30, 0, 0), "2026-11-30"},
		{"周日归属上一个周一", at(2026, time.November, 29, 23, 59), "2026-11-23"},
		{"跨年：2027-01-01 周五仍算在 2026-12-28 那一周", at(2027, time.January, 1, 8, 0), "2026-12-28"},
		{"周日最后一秒", at(2026, time.December, 27, 23, 59), "2026-12-21"},
	}
	for _, c := range cases {
		if got := WeekStartID(c.at, plus8); got != c.want {
			t.Errorf("%s: WeekStartID = %s, 期望 %s", c.name, got, c.want)
		}
	}
}

func TestMonthStartID(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"月中", at(2026, time.November, 24, 10, 0), "2026-11-01"},
		{"月初第一秒", at(2026, time.November, 1, 0, 0), "2026-11-01"},
		{"月末最后一秒", at(2026, time.November, 30, 23, 59), "2026-11-01"},
		{"跨年 1 月", at(2027, time.January, 1, 0, 0), "2027-01-01"},
		{"闰年 2 月末", at(2028, time.February, 29, 23, 59), "2028-02-01"},
	}
	for _, c := range cases {
		if got := MonthStartID(c.at, plus8); got != c.want {
			t.Errorf("%s: MonthStartID = %s, 期望 %s", c.name, got, c.want)
		}
	}
}

// 同一个 UTC 瞬间，在 UTC 和 UTC+8 下会算出不同周期。这条测试钉住"归属按 loc
// 的日历算"这件事——如果哪天有人图省事直接用 UTC，这里会红。
func TestSameInstantDifferentZoneDifferentPeriod(t *testing.T) {
	utc := time.Date(2026, time.November, 22, 16, 30, 0, 0, time.UTC)

	if got := WeekStartID(utc, time.UTC); got != "2026-11-16" {
		t.Errorf("UTC 下周日: WeekStartID = %s, 期望 2026-11-16", got)
	}
	if got := WeekStartID(utc, plus8); got != "2026-11-23" {
		t.Errorf("UTC+8 下已是周一: WeekStartID = %s, 期望 2026-11-23", got)
	}

	eom := time.Date(2026, time.November, 30, 16, 30, 0, 0, time.UTC)
	if got := MonthStartID(eom, time.UTC); got != "2026-11-01" {
		t.Errorf("UTC 下月末: MonthStartID = %s, 期望 2026-11-01", got)
	}
	if got := MonthStartID(eom, plus8); got != "2026-12-01" {
		t.Errorf("UTC+8 下已是次月: MonthStartID = %s, 期望 2026-12-01", got)
	}
}

func TestWeekRange(t *testing.T) {
	start, end, err := WeekRange("2026-12-28", plus8)
	if err != nil {
		t.Fatalf("WeekRange: %v", err)
	}
	if got := start.In(plus8).Format(dateLayout); got != "2026-12-28" {
		t.Errorf("start = %s, 期望 2026-12-28", got)
	}
	if got := end.In(plus8).Format(dateLayout); got != "2027-01-04" {
		t.Errorf("跨年 end = %s, 期望 2027-01-04", got)
	}

	// 半开区间必须正好罩住这一周的首尾两刻
	before := start
	during := end.Add(-time.Millisecond)
	if !(start.Before(before.Add(time.Millisecond))) || !(during.Before(end)) {
		t.Errorf("区间边界不含首刻或漏掉尾刻: start=%v end=%v", start, end)
	}

	if _, _, err := WeekRange("2026-11-24", plus8); !errors.Is(err, ErrNotMonday) {
		t.Errorf("非周一应报 ErrNotMonday, 实得 %v", err)
	}
}

func TestMonthRange(t *testing.T) {
	start, end, err := MonthRange("2026-12-01", plus8)
	if err != nil {
		t.Fatalf("MonthRange: %v", err)
	}
	if got := start.In(plus8).Format(dateLayout); got != "2026-12-01" {
		t.Errorf("start = %s, 期望 2026-12-01", got)
	}
	if got := end.In(plus8).Format(dateLayout); got != "2027-01-01" {
		t.Errorf("12 月末应跨到 2027-01-01, 实得 %s", got)
	}
	if _, _, err := MonthRange("2026-11-15", plus8); !errors.Is(err, ErrNotMonthStart) {
		t.Errorf("非月初应报 ErrNotMonthStart, 实得 %v", err)
	}
}

// 往返一致性：任何时刻算出的起始日，喂回 Range 都必须成功，且罩住原时刻。
func TestRoundTrip(t *testing.T) {
	for d := at(2026, time.November, 1, 3, 4); d.Before(at(2027, time.March, 1, 0, 0)); d = d.Add(24*time.Hour + 7*time.Hour) {
		ws := WeekStartID(d, plus8)
		s, e, err := WeekRange(ws, plus8)
		if err != nil {
			t.Fatalf("WeekRange(%s): %v", ws, err)
		}
		if !(d.Equal(s) || (d.After(s) && d.Before(e))) {
			t.Errorf("%v 不在 %s 的周区间 [%v, %v) 内", d, ws, s, e)
		}

		ms := MonthStartID(d, plus8)
		s2, e2, err := MonthRange(ms, plus8)
		if err != nil {
			t.Fatalf("MonthRange(%s): %v", ms, err)
		}
		if !(d.Equal(s2) || (d.After(s2) && d.Before(e2))) {
			t.Errorf("%v 不在 %s 的月区间 [%v, %v) 内", d, ms, s2, e2)
		}

		// period_end 落的是"含端点的那一天"，必须正好是右端开区间退一天，
		// 并且落在同一个周期里——否则报表区间和归属列会对不上。
		we, err := WeekEndID(ws, plus8)
		if err != nil {
			t.Fatalf("WeekEndID(%s): %v", ws, err)
		}
		if got := WeekStartID(mustDate(we, plus8), plus8); got != ws {
			t.Errorf("周末日 %s 又落到别的周去了: %s", we, got)
		}
		me, err := MonthEndID(ms, plus8)
		if err != nil {
			t.Fatalf("MonthEndID(%s): %v", ms, err)
		}
		if got := MonthStartID(mustDate(me, plus8), plus8); got != ms {
			t.Errorf("月末日 %s 又落到别的月去了: %s", me, got)
		}
	}
}

func TestEndID(t *testing.T) {
	cases := []struct {
		kind string
		id   string
		want string
	}{
		{"week", "2026-11-23", "2026-11-29"},
		{"week", "2026-12-28", "2027-01-03"}, // 跨年
		{"month", "2026-12-01", "2026-12-31"},
		{"month", "2028-02-01", "2028-02-29"}, // 闰年
		{"month", "2027-02-01", "2027-02-28"},
	}
	for _, c := range cases {
		var (
			got string
			err error
		)
		if c.kind == "week" {
			got, err = WeekEndID(c.id, plus8)
		} else {
			got, err = MonthEndID(c.id, plus8)
		}
		if err != nil {
			t.Errorf("%s %s: %v", c.kind, c.id, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s %s 的末日 = %s, 期望 %s", c.kind, c.id, got, c.want)
		}
	}

	// 起始日不合法时，末日也不该给出来——不然报表会存成一个查不到的区间。
	if _, err := WeekEndID("2026-11-24", plus8); !errors.Is(err, ErrNotMonday) {
		t.Errorf("WeekEndID 非周一应报 ErrNotMonday, 实得 %v", err)
	}
	if _, err := MonthEndID("2026-11-15", plus8); !errors.Is(err, ErrNotMonthStart) {
		t.Errorf("MonthEndID 非月初应报 ErrNotMonthStart, 实得 %v", err)
	}
}

// mustDate 把 YYYY-MM-DD 按 loc 的日历变回时刻，用来做往返检查。
func mustDate(id string, loc *time.Location) time.Time {
	t, err := time.Parse(dateLayout, id)
	if err != nil {
		panic(err)
	}
	y, m, d := t.Date()
	return time.Date(y, m, d, 12, 0, 0, 0, loc)
}

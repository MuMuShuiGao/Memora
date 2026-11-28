package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Daily 对应 design §1 的 dailies 表（ADR-0031）。一天一条，三节纯文本；
// 归属周月只看 EntryDay，与创建、修改时刻无关。
type Daily struct {
	ID         int64     `json:"id"`
	EntryDay   string    `json:"entry_day"`
	Priorities string    `json:"priorities"`
	Done       string    `json:"done"`
	Risk       string    `json:"risk"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

const dailyColumns = "id, entry_day, priorities, done, risk, created_at, updated_at"

func scanDaily(row interface{ Scan(...any) error }) (Daily, error) {
	var (
		d         Daily
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&d.ID, &d.EntryDay, &d.Priorities, &d.Done, &d.Risk, &createdAt, &updatedAt); err != nil {
		return d, err
	}
	created, err := decodeTime(createdAt)
	if err != nil {
		return d, err
	}
	updated, err := decodeTime(updatedAt)
	if err != nil {
		return d, err
	}
	d.CreatedAt = created
	d.UpdatedAt = updated
	return d, nil
}

func (s *Store) GetDaily(ctx context.Context, day string) (Daily, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+dailyColumns+" FROM dailies WHERE entry_day = ?", day)
	d, err := scanDaily(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Daily{}, ErrNotFound
	}
	if err != nil {
		return Daily{}, fmt.Errorf("store: 取 %s 的日报: %w", day, err)
	}
	return d, nil
}

// UpsertDaily 写或改某一天的日报。entry_day 上的 UNIQUE 保证一天一条：
// 同一天再写就是覆盖三节，不产生第二条（ADR-0031）。
func (s *Store) UpsertDaily(ctx context.Context, day, priorities, done, risk string, now time.Time) (Daily, error) {
	stamp := encodeTime(now.UTC())
	_, err := s.db.ExecContext(ctx, `
INSERT INTO dailies (entry_day, priorities, done, risk, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (entry_day) DO UPDATE SET
	priorities = excluded.priorities,
	done       = excluded.done,
	risk       = excluded.risk,
	updated_at = excluded.updated_at`,
		day, priorities, done, risk, stamp, stamp)
	if err != nil {
		return Daily{}, fmt.Errorf("store: 写 %s 的日报: %w", day, err)
	}
	return s.GetDaily(ctx, day)
}

// DailiesBetween 取 [startDay, endDay) 这个左闭右开区间里"有素材"的日报，按日期升序。
// 三节全空的行表示这天没写，不参与生成也不进补跑候选（ADR-0031），所以这里一并挡掉。
// 边界由调用方用 internal/period 算好再传进来，SQL 里不出现日期函数（design §2）。
func (s *Store) DailiesBetween(ctx context.Context, startDay, endDay string) ([]Daily, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+dailyColumns+` FROM dailies
WHERE entry_day >= ? AND entry_day < ?
	AND (priorities <> '' OR done <> '' OR risk <> '')
ORDER BY entry_day ASC, id ASC`, startDay, endDay)
	if err != nil {
		return nil, fmt.Errorf("store: 取 %s 至 %s 的日报: %w", startDay, endDay, err)
	}
	defer rows.Close()

	out := []Daily{}
	for rows.Next() {
		d, err := scanDaily(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 读日报行: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DistinctDailyDays 给补跑调度当第四路候选来源。三节全空的行表示"这天没有素材"，
// 不算写过日报，所以不进候选（ADR-0031）；保存全空留下的行本身可以继续存在。
func (s *Store) DistinctDailyDays(ctx context.Context) ([]string, error) {
	return s.distinct(`SELECT DISTINCT entry_day FROM dailies
WHERE priorities <> '' OR done <> '' OR risk <> ''
ORDER BY entry_day DESC`, ctx)
}

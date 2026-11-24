package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Note struct {
	ID         int64     `json:"id"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	WeekStart  string    `json:"week_start"`
	MonthStart string    `json:"month_start"`
}

const noteColumns = "id, body, created_at, updated_at, week_start, month_start"

func scanNote(row interface{ Scan(...any) error }) (Note, error) {
	var (
		n         Note
		createdAt string
		updatedAt string
	)
	err := row.Scan(&n.ID, &n.Body, &createdAt, &updatedAt, &n.WeekStart, &n.MonthStart)
	if err != nil {
		return n, err
	}
	if n.CreatedAt, err = decodeTime(createdAt); err != nil {
		return n, err
	}
	if n.UpdatedAt, err = decodeTime(updatedAt); err != nil {
		return n, err
	}
	return n, nil
}

// CreateNote 写入一条记录。weekStart / monthStart 由调用方用 period 包算好后传进来，
// 存进来就固化——UpdateNote 永不重算，这正是需求 §4"改过的旧记录不迁入新周期"的落点。
func (s *Store) CreateNote(ctx context.Context, body string, now time.Time, weekStart, monthStart string) (Note, error) {
	created := now.UTC()
	n := Note{
		Body:       body,
		CreatedAt:  created,
		UpdatedAt:  created,
		WeekStart:  weekStart,
		MonthStart: monthStart,
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notes (body, created_at, updated_at, week_start, month_start) VALUES (?,?,?,?,?)`,
		n.Body, encodeTime(n.CreatedAt), encodeTime(n.UpdatedAt), n.WeekStart, n.MonthStart)
	if err != nil {
		return Note{}, fmt.Errorf("store: 新建记录: %w", err)
	}
	n.ID, _ = res.LastInsertId()
	return n, nil
}

// ListNotes 返回从 new 到 old 的记录。q 非空则按正文子串过滤；before 是游标，
// 只取 created_at 严格早于它的行。
func (s *Store) ListNotes(ctx context.Context, q string, before *time.Time, limit int) ([]Note, error) {
	var (
		clauses []string
		args    []any
	)
	if q != "" {
		clauses = append(clauses, `body LIKE ? ESCAPE '\'`)
		args = append(args, likePattern(q))
	}
	if before != nil {
		clauses = append(clauses, "created_at < ?")
		args = append(args, encodeTime(*before))
	}
	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT "+noteColumns+" FROM notes "+where+" ORDER BY created_at DESC, id DESC LIMIT ?",
		append(args, limit)...)
	if err != nil {
		return nil, fmt.Errorf("store: 列表查询: %w", err)
	}
	defer rows.Close()

	out := []Note{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 读列表行: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) GetNote(ctx context.Context, id int64) (Note, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+noteColumns+" FROM notes WHERE id = ?", id)
	n, err := scanNote(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, fmt.Errorf("store: 取记录 %d: %w", id, err)
	}
	return n, nil
}

// UpdateNote 只改正文和时间戳。归属两列不出现在 SQL 里，是想让人一眼看出
// "改内容不会把记录搬到别的周期"这件事是刻意的。
func (s *Store) UpdateNote(ctx context.Context, id int64, body string, now time.Time) (Note, error) {
	res, err := s.db.ExecContext(ctx,
		"UPDATE notes SET body = ?, updated_at = ? WHERE id = ?",
		body, encodeTime(now.UTC()), id)
	if err != nil {
		return Note{}, fmt.Errorf("store: 改记录 %d: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return Note{}, ErrNotFound
	}
	return s.GetNote(ctx, id)
}

func (s *Store) DeleteNote(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM notes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("store: 删记录 %d: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountNotes 用于启动时和排查时确认库里到底有多少条。
func (s *Store) CountNotes(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notes").Scan(&n); err != nil {
		return 0, fmt.Errorf("store: 计数: %w", err)
	}
	return n, nil
}

// NotesForWeek 与 NotesForMonth 是 design §6 第 1 步的两次索引查询：只看冗余列，
// 不看 created_at 也不看 updated_at。归属在插入那一刻就定死了，所以"改过的旧记录
// 不迁入新周期"在这里自动成立，不需要额外条件。
func (s *Store) NotesForWeek(ctx context.Context, weekStart string) ([]Note, error) {
	return s.notesByPeriodColumn(ctx, "week_start", weekStart)
}

func (s *Store) NotesForMonth(ctx context.Context, monthStart string) ([]Note, error) {
	return s.notesByPeriodColumn(ctx, "month_start", monthStart)
}

// notesByPeriodColumn 的列名来自上面两个写死的字面量，绝不接受外部输入。
func (s *Store) notesByPeriodColumn(ctx context.Context, column, periodStart string) ([]Note, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+noteColumns+" FROM notes WHERE "+column+" = ? ORDER BY created_at ASC, id ASC",
		periodStart)
	if err != nil {
		return nil, fmt.Errorf("store: 取 %s=%s 的记录: %w", column, periodStart, err)
	}
	defer rows.Close()

	out := []Note{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 读周期行: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// DistinctWeekStarts / DistinctMonthStarts 给出"库里真实存在哪些周期"。
// 补跑调度靠它列出候选，因此没有记录的周期根本不会被考虑（design §6 第 4 条）。
func (s *Store) DistinctWeekStarts(ctx context.Context) ([]string, error) {
	return s.distinct("SELECT DISTINCT week_start FROM notes ORDER BY week_start DESC", ctx)
}

func (s *Store) DistinctMonthStarts(ctx context.Context) ([]string, error) {
	return s.distinct("SELECT DISTINCT month_start FROM notes ORDER BY month_start DESC", ctx)
}

func (s *Store) distinct(query string, ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: 列周期: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("store: 读周期: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// likePattern 把用户输入包成子串匹配，并转义 LIKE 的三个特殊字符。
// 不转义的话，输入 "%" 就能匹配一切，输入 "_" 会误配单字符——这不是注入
// （值始终走参数绑定），但会让过滤结果和预期不符。
func likePattern(q string) string {
	var b strings.Builder
	b.WriteString("%")
	for _, r := range q {
		switch r {
		case '\\', '%', '_':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	b.WriteString("%")
	return b.String()
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Report 对应 design §1 的 reports 表。Body / SourceCount / FailReason / GeneratedAt
// 用指针，是因为"未生成好"和"生成好但正文是空串"必须是两种可区分的状态——
// FR-05 要求不静默缺失，客户端要靠 status 分支显示文案。
type Report struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	PeriodStart string     `json:"period_start"`
	PeriodEnd   string     `json:"period_end"`
	Status      string     `json:"status"`
	Body        *string    `json:"body"`
	SourceCount *int       `json:"source_count"`
	FailReason  *string    `json:"fail_reason"`
	GeneratedAt *time.Time `json:"generated_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// 报表状态取值，与表上的 CHECK 一致。
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusReady   = "ready"
	StatusFailed  = "failed"
)

const reportColumns = `id, kind, period_start, period_end, status,
	body, source_count, fail_reason, generated_at, created_at, updated_at`

func scanReport(row interface{ Scan(...any) error }) (Report, error) {
	var (
		r                             Report
		body, failReason, generatedAt sql.NullString
		sourceCount                   sql.NullInt64
		createdAt, updatedAt          string
	)
	err := row.Scan(&r.ID, &r.Kind, &r.PeriodStart, &r.PeriodEnd, &r.Status,
		&body, &sourceCount, &failReason, &generatedAt, &createdAt, &updatedAt)
	if err != nil {
		return r, err
	}
	r.Body = optString(body)
	r.FailReason = optString(failReason)
	if sourceCount.Valid {
		n := int(sourceCount.Int64)
		r.SourceCount = &n
	}
	if generatedAt.Valid {
		t, err := decodeTime(generatedAt.String)
		if err != nil {
			return r, err
		}
		r.GeneratedAt = &t
	}
	if r.CreatedAt, err = decodeTime(createdAt); err != nil {
		return r, err
	}
	if r.UpdatedAt, err = decodeTime(updatedAt); err != nil {
		return r, err
	}
	return r, nil
}

func optString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

// BeginRun 把某周期标成 running：已有行就覆盖状态，没有就插入。
// 一个周期只留一份当前报表（ADR-0004），重新生成走 UPDATE 正是这个 UNIQUE 的用途。
func (s *Store) BeginRun(ctx context.Context, kind, periodStart, periodEnd string, now time.Time) (Report, error) {
	stamp := encodeTime(now.UTC())
	_, err := s.db.ExecContext(ctx, `
INSERT INTO reports (kind, period_start, period_end, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (kind, period_start) DO UPDATE SET
	period_end = excluded.period_end,
	status     = 'running',
	updated_at = excluded.updated_at`,
		kind, periodStart, periodEnd, StatusRunning, stamp, stamp)
	if err != nil {
		return Report{}, fmt.Errorf("store: 标记生成中 %s/%s: %w", kind, periodStart, err)
	}
	return s.GetReport(ctx, kind, periodStart)
}

// FinishRun 落最终状态。失败时正文写 NULL：一个周期只有一份当前报表（ADR-0004），
// 覆盖是设计认了的，宁可让客户端明确看到"这份失败了"，也不留一份分不清新旧的正文。
func (s *Store) FinishRun(ctx context.Context, kind, periodStart, status string,
	body string, sourceCount int, failReason string, generatedAt, now time.Time) (Report, error) {

	if status != StatusReady && status != StatusFailed {
		return Report{}, fmt.Errorf("store: 非法收尾状态 %q", status)
	}

	var args []any
	set := "status = ?, updated_at = ?, generated_at = ?"
	args = append(args, status, encodeTime(now.UTC()), encodeTime(generatedAt.UTC()))

	if status == StatusReady {
		set += ", body = ?, source_count = ?, fail_reason = NULL"
		args = append(args, body, sourceCount)
	} else {
		set += ", body = NULL, source_count = NULL, fail_reason = ?"
		reason := failReason
		if reason == "" {
			reason = "生成失败，无原因"
		}
		args = append(args, reason)
	}
	args = append(args, kind, periodStart)

	res, err := s.db.ExecContext(ctx, "UPDATE reports SET "+set+" WHERE kind = ? AND period_start = ?", args...)
	if err != nil {
		return Report{}, fmt.Errorf("store: 收尾 %s/%s: %w", kind, periodStart, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return Report{}, ErrNotFound
	}
	return s.GetReport(ctx, kind, periodStart)
}

func (s *Store) GetReport(ctx context.Context, kind, periodStart string) (Report, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+reportColumns+" FROM reports WHERE kind = ? AND period_start = ?",
		kind, periodStart)
	r, err := scanReport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Report{}, ErrNotFound
	}
	if err != nil {
		return Report{}, fmt.Errorf("store: 取报表 %s/%s: %w", kind, periodStart, err)
	}
	return r, nil
}

// ListReports 按周期起始日倒序。kind 为空则两种都列。
func (s *Store) ListReports(ctx context.Context, kind string, limit int) ([]Report, error) {
	query := "SELECT " + reportColumns + " FROM reports"
	var args []any
	if kind != "" {
		query += " WHERE kind = ?"
		args = append(args, kind)
	}
	query += " ORDER BY period_start DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: 报表列表: %w", err)
	}
	defer rows.Close()

	out := []Report{}
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 读报表行: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Book 对应 design §1 的 books 表（ADR-0029）。读书是第三类业务对象：
// 记录进报表、待办有完成态与顺序，书只有读完年月和一句心得，三者互不转换。
// 读完月份是字面年月（YYYY-MM），存储层不校验格式，那是 api 层的事。
type Book struct {
	ID            int64     `json:"id"`
	Title         string    `json:"title"`
	Author        string    `json:"author"`
	FinishedMonth string    `json:"finished_month"`
	Takeaway      string    `json:"takeaway"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

const bookColumns = "id, title, author, finished_month, takeaway, created_at, updated_at"

func scanBook(row interface{ Scan(...any) error }) (Book, error) {
	var (
		b         Book
		createdAt string
		updatedAt string
	)
	if err := row.Scan(&b.ID, &b.Title, &b.Author, &b.FinishedMonth, &b.Takeaway, &createdAt, &updatedAt); err != nil {
		return b, err
	}
	var err error
	if b.CreatedAt, err = decodeTime(createdAt); err != nil {
		return b, err
	}
	if b.UpdatedAt, err = decodeTime(updatedAt); err != nil {
		return b, err
	}
	return b, nil
}

// booksSafetyLimit 一次最多返回多少本。一年几十本，1000 条只防"攒了很多年"的极端情况，
// 不是分页机制：书单不做搜索也不需要分页（ADR-0029）。
const booksSafetyLimit = 1000

// ListBooks 按读完月份倒序返回全量，同月里新加的在前面。
func (s *Store) ListBooks(ctx context.Context) ([]Book, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+bookColumns+" FROM books ORDER BY finished_month DESC, created_at DESC, id DESC LIMIT ?",
		booksSafetyLimit)
	if err != nil {
		return nil, fmt.Errorf("store: 书单: %w", err)
	}
	defer rows.Close()

	out := []Book{}
	for rows.Next() {
		b, err := scanBook(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 读书行: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) GetBook(ctx context.Context, id int64) (Book, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+bookColumns+" FROM books WHERE id = ?", id)
	b, err := scanBook(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Book{}, ErrNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("store: 取书 %d: %w", id, err)
	}
	return b, nil
}

func (s *Store) CreateBook(ctx context.Context, title, author, finishedMonth string, now time.Time) (Book, error) {
	stamp := encodeTime(now.UTC())
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO books (title, author, finished_month, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		title, author, finishedMonth, stamp, stamp)
	if err != nil {
		return Book{}, fmt.Errorf("store: 新建书: %w", err)
	}
	id, _ := res.LastInsertId()
	return Book{
		ID:            id,
		Title:         title,
		Author:        author,
		FinishedMonth: finishedMonth,
		CreatedAt:     now.UTC(),
		UpdatedAt:     now.UTC(),
	}, nil
}

// UpdateBookTitle / Author / FinishedMonth / Takeaway 各只动一列，
// 与待办那组改字段的方法同一个形状：一次 PATCH 只改客户端真正发过来的字段。
func (s *Store) UpdateBookTitle(ctx context.Context, id int64, title string, now time.Time) (Book, error) {
	return s.updateBookField(ctx, id, "title", title, now)
}

func (s *Store) UpdateBookAuthor(ctx context.Context, id int64, author string, now time.Time) (Book, error) {
	return s.updateBookField(ctx, id, "author", author, now)
}

func (s *Store) UpdateBookFinishedMonth(ctx context.Context, id int64, month string, now time.Time) (Book, error) {
	return s.updateBookField(ctx, id, "finished_month", month, now)
}

// UpdateBookTakeaway 空串表示清掉（ADR-0029），与待办心得同一口径。
func (s *Store) UpdateBookTakeaway(ctx context.Context, id int64, takeaway string, now time.Time) (Book, error) {
	return s.updateBookField(ctx, id, "takeaway", takeaway, now)
}

// updateBookField 的 column 只来自上面四个常量，绝不接收请求里的字符串。
func (s *Store) updateBookField(ctx context.Context, id int64, column, value string, now time.Time) (Book, error) {
	res, err := s.db.ExecContext(ctx,
		"UPDATE books SET "+column+" = ?, updated_at = ? WHERE id = ?",
		value, encodeTime(now.UTC()), id)
	if err != nil {
		return Book{}, fmt.Errorf("store: 改书 %d 的 %s: %w", id, column, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return Book{}, ErrNotFound
	}
	return s.GetBook(ctx, id)
}

func (s *Store) DeleteBook(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM books WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("store: 删书 %d: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

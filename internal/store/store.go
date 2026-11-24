// Package store 是唯一的持久层。换数据库只动这个包（ADR-0006 的返工点成立与否
// 取决于此），其他包不得 import 驱动或写 SQL。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const driverName = "sqlite"

// ErrNotFound 表示按 id 没找到行。上层据此返回 404，不外泄 SQL 细节。
var ErrNotFound = errors.New("store: 记录不存在")

type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）SQLite 文件并建表。
func Open(path string) (*Store, error) {
	db, err := sql.Open(driverName, "file:"+path)
	if err != nil {
		return nil, fmt.Errorf("store: 打开 %s: %w", path, err)
	}
	// SQLite 是单写者（ADR-0007）。开多个连接只会把可避免的 SQLITE_BUSY 变成
	// 随机失败，所以钉死一条连接，让写操作自己排队。
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: 连接 %s: %w", path, err)
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: %s: %w", pragma, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate 用 IF NOT EXISTS 建表，所以启动即幂等。字段定义与 design §1 一一对应。
func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS notes (
	id          INTEGER PRIMARY KEY,
	body        TEXT    NOT NULL,
	created_at  TEXT    NOT NULL,
	updated_at  TEXT    NOT NULL,
	week_start  TEXT    NOT NULL,
	month_start TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notes_created ON notes(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notes_week    ON notes(week_start);
CREATE INDEX IF NOT EXISTS idx_notes_month   ON notes(month_start);

CREATE TABLE IF NOT EXISTS reports (
	id            INTEGER PRIMARY KEY,
	kind          TEXT    NOT NULL CHECK (kind IN ('week','month')),
	period_start  TEXT    NOT NULL,
	period_end    TEXT    NOT NULL,
	status        TEXT    NOT NULL CHECK (status IN ('pending','running','ready','failed')),
	body          TEXT,
	source_count  INTEGER,
	fail_reason   TEXT,
	generated_at  TEXT,
	created_at    TEXT    NOT NULL,
	updated_at    TEXT    NOT NULL,
	UNIQUE (kind, period_start)
);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("store: 建表: %w", err)
	}
	return nil
}

const timeLayout = time.RFC3339Nano

func encodeTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func decodeTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("store: 解析时间 %q: %w", s, err)
	}
	return t, nil
}

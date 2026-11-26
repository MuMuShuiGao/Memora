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

// inTx 跑一个必须整体成败的写事务。待办的 rank 是"整个未完成集合"的属性——新建要顺延、
// 勾掉要收紧——单条语句表达不了，又不允许出现"顺延了一半"的中间状态。
// Store 只开一条连接（见 Open），所以这里不需要考虑并发事务。
func (s *Store) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: 起事务: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: 提交事务: %w", err)
	}
	return nil
}

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

CREATE TABLE IF NOT EXISTS tasks (
	id               INTEGER PRIMARY KEY,
	body             TEXT    NOT NULL,
	done             INTEGER NOT NULL DEFAULT 0 CHECK (done IN (0,1)),
	rank             INTEGER NOT NULL,
	done_at          TEXT,
	done_week_start  TEXT,
	done_month_start TEXT,
	created_at       TEXT    NOT NULL,
	updated_at       TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tasks_open ON tasks(done, rank);
CREATE INDEX IF NOT EXISTS idx_tasks_done ON tasks(done, done_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_done_week  ON tasks(done_week_start);
CREATE INDEX IF NOT EXISTS idx_tasks_done_month ON tasks(done_month_start);

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

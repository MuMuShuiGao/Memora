package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"memora/internal/period"
)

// Task 对应 design §1 的 tasks 表。待办与记录是两类对象（ADR-0014）：
// 它带顺序和完成态，记录带归属周期，两边互不转换。
type Task struct {
	ID             int64      `json:"id"`
	Body           string     `json:"body"`
	Done           bool       `json:"done"`
	Rank           int        `json:"rank"`
	DoneAt         *time.Time `json:"done_at"`
	DoneWeekStart  string     `json:"done_week_start"`
	DoneMonthStart string     `json:"done_month_start"`
	// DueOn 是安排日期（YYYY-MM-DD，服务端本地时区），只决定日历放在哪一格（ADR-0021）。
	DueOn     string    `json:"due_on"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ErrBadOrder 表示 reorder 提交的 id 集合和库里当前的未完成集合对不上。
// 它是 400 而不是 500：客户端拿的是服务端自己给的列表，对不上只可能是它过期了。
var ErrBadOrder = errors.New("store: 待办顺序与未完成集合不一致")

const taskColumns = "id, body, done, rank, done_at, done_week_start, done_month_start, due_on, created_at, updated_at"

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var (
		t                Task
		done             int64
		doneAt           sql.NullString
		doneWeek, doneMo sql.NullString
		createdAt        string
		updatedAt        string
	)
	err := row.Scan(&t.ID, &t.Body, &done, &t.Rank, &doneAt, &doneWeek, &doneMo, &t.DueOn, &createdAt, &updatedAt)
	if err != nil {
		return t, err
	}
	t.Done = done != 0
	if doneAt.Valid {
		v, err := decodeTime(doneAt.String)
		if err != nil {
			return t, err
		}
		t.DoneAt = &v
	}
	t.DoneWeekStart = doneWeek.String
	t.DoneMonthStart = doneMo.String
	if t.CreatedAt, err = decodeTime(createdAt); err != nil {
		return t, err
	}
	if t.UpdatedAt, err = decodeTime(updatedAt); err != nil {
		return t, err
	}
	return t, nil
}

// tasksSafetyLimit 是每一段（未完成 / 已完成）一次最多返回多少条。待办会被勾掉或删除，
// 几十条量级就到头了（估算）；这个数只防"库里攒了好几年"那种极端情况，不是分页机制。
const tasksSafetyLimit = 500

// ListTasks 返回未完成按 rank 升序、已完成按完成时刻倒序拼在一起的一份列表。
// 一次给全量而不分页：折叠区点一下就要看到，且客户端要靠整列 id 提交 reorder。
func (s *Store) ListTasks(ctx context.Context) ([]Task, error) {
	open, err := s.tasksWhere(ctx, "done = 0", "rank ASC, id ASC", nil)
	if err != nil {
		return nil, err
	}
	done, err := s.tasksWhere(ctx, "done = 1", "done_at DESC, id DESC", nil)
	if err != nil {
		return nil, err
	}
	return append(open, done...), nil
}

func (s *Store) tasksWhere(ctx context.Context, where, orderBy string, args []any) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+taskColumns+" FROM tasks WHERE "+where+" ORDER BY "+orderBy+" LIMIT ?",
		append(args, tasksSafetyLimit)...)
	if err != nil {
		return nil, fmt.Errorf("store: 待办列表 (%s): %w", where, err)
	}
	defer rows.Close()

	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("store: 读待办行: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetTask(ctx context.Context, id int64) (Task, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = ?", id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("store: 取待办 %d: %w", id, err)
	}
	return t, nil
}

// CreateTask 新建一条待办并插到队首（ADR-0014 的连带决定：刚想起来的事正因为
// 要先做才会去记，排到队尾等于加完看不见）。整段在一个事务里，否则并发下会出现
// 两条 rank=1。
func (s *Store) CreateTask(ctx context.Context, body, dueOn string, now time.Time) (Task, error) {
	stamp := encodeTime(now.UTC())
	t := Task{Body: body, Rank: 1, DueOn: dueOn, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}

	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE tasks SET rank = rank + 1 WHERE done = 0"); err != nil {
			return fmt.Errorf("store: 待办后移: %w", err)
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO tasks (body, done, rank, due_on, created_at, updated_at) VALUES (?, 0, ?, ?, ?, ?)`,
			t.Body, t.Rank, t.DueOn, stamp, stamp)
		if err != nil {
			return fmt.Errorf("store: 新建待办: %w", err)
		}
		t.ID, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	return t, nil
}

// UpdateTaskBody 只改正文。rank 与 done 不在这条路径上动——改文字不该改变优先级。
func (s *Store) UpdateTaskBody(ctx context.Context, id int64, body string, now time.Time) (Task, error) {
	res, err := s.db.ExecContext(ctx,
		"UPDATE tasks SET body = ?, updated_at = ? WHERE id = ?",
		body, encodeTime(now.UTC()), id)
	if err != nil {
		return Task{}, fmt.Errorf("store: 改待办 %d: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return Task{}, ErrNotFound
	}
	return s.GetTask(ctx, id)
}

// UpdateTaskDueOn 只改安排日期。与改正文一样，不碰 rank、完成态与报表归属列。
func (s *Store) UpdateTaskDueOn(ctx context.Context, id int64, dueOn string, now time.Time) (Task, error) {
	res, err := s.db.ExecContext(ctx,
		"UPDATE tasks SET due_on = ?, updated_at = ? WHERE id = ?",
		dueOn, encodeTime(now.UTC()), id)
	if err != nil {
		return Task{}, fmt.Errorf("store: 改待办日期 %d: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return Task{}, ErrNotFound
	}
	return s.GetTask(ctx, id)
}

// BackfillTaskDueOn 给 due_on 为空的历史待办补上日期：取创建时刻在 loc 里的那个自然日。
// 只碰空值、幂等；在启动时调用一次。单列 UPDATE 不需要事务，但统一走 inTx 让失败整体回滚。
func (s *Store) BackfillTaskDueOn(ctx context.Context, loc *time.Location) (int64, error) {
	if loc == nil {
		loc = time.Local
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id, created_at FROM tasks WHERE due_on = ''")
	if err != nil {
		return 0, fmt.Errorf("store: 列待回填日期: %w", err)
	}
	type row struct {
		id    int64
		dueOn string
	}
	var todo []row
	for rows.Next() {
		var id int64
		var createdAt string
		if err := rows.Scan(&id, &createdAt); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: 读待回填行: %w", err)
		}
		t, err := decodeTime(createdAt)
		if err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: 待办 %d 的创建时间: %w", id, err)
		}
		todo = append(todo, row{id: id, dueOn: period.DayID(t, loc)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: 遍历待回填行: %w", err)
	}
	if len(todo) == 0 {
		return 0, nil
	}

	err = s.inTx(ctx, func(tx *sql.Tx) error {
		for _, r := range todo {
			if _, err := tx.ExecContext(ctx,
				"UPDATE tasks SET due_on = ? WHERE id = ? AND due_on = ''", r.dueOn, r.id); err != nil {
				return fmt.Errorf("store: 回填待办 %d 的日期: %w", r.id, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int64(len(todo)), nil
}

// SetTaskDone 勾掉或取消勾掉一条待办。
//
// 勾掉时写 done_at 和归属两列——归属由"在哪一周被勾掉"决定，跟它是哪周记下的无关
// （design §2、FR-11）。冗余列和 notes 那两列同一个来处：调用方用 period 包算好传进来，
// SQL 里不出现日期与时区函数。取消勾选时三列一起清空，重勾会按新那一刻重算归属。
// 两种收尾都要重编未完成序列，不然 rank 会漏号。
func (s *Store) SetTaskDone(ctx context.Context, id int64, done bool, now time.Time, weekStart, monthStart string) (Task, error) {
	cur, err := s.GetTask(ctx, id)
	if err != nil {
		return Task{}, err
	}
	if cur.Done == done {
		return cur, nil
	}
	stamp := encodeTime(now.UTC())

	err = s.inTx(ctx, func(tx *sql.Tx) error {
		if done {
			if _, err := tx.ExecContext(ctx, `
UPDATE tasks SET done = 1, done_at = ?, done_week_start = ?, done_month_start = ?, updated_at = ?
WHERE id = ?`, stamp, weekStart, monthStart, stamp, id); err != nil {
				return fmt.Errorf("store: 勾掉待办 %d: %w", id, err)
			}
		} else {
			// 先落到队尾再重编，重编完它就是最后一名。子查询排除了自己（此刻 done 仍是 1）。
			if _, err := tx.ExecContext(ctx, `
UPDATE tasks SET done = 0, done_at = NULL, done_week_start = NULL, done_month_start = NULL, updated_at = ?,
	rank = (SELECT COALESCE(MAX(rank), 0) + 1 FROM tasks WHERE done = 0)
WHERE id = ?`, stamp, id); err != nil {
				return fmt.Errorf("store: 取消勾选待办 %d: %w", id, err)
			}
		}
		return resequenceOpen(ctx, tx)
	})
	if err != nil {
		return Task{}, err
	}
	return s.GetTask(ctx, id)
}

// DeleteTask 删一条。删的是未完成项时后面的 rank 由重编收紧，不留空洞。
func (s *Store) DeleteTask(ctx context.Context, id int64, now time.Time) error {
	cur, err := s.GetTask(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM tasks WHERE id = ?", id); err != nil {
		return fmt.Errorf("store: 删待办 %d: %w", id, err)
	}
	if cur.Done {
		return nil
	}
	return s.inTx(ctx, func(tx *sql.Tx) error { return resequenceOpen(ctx, tx) })
}

// ResequenceTasks 按给定顺序把未完成待办的 rank 重写成 1..n。
//
// ids 必须是当前未完成集合的一个完整排列：重复、未知、漏一条都直接拒绝而不是部分生效。
// 拒绝的理由是"客户端过期"——另一台设备刚加了一条待办，这台上还没看到，此时若照单收下，
// 那条没出现在 ids 里的待办会被留在旧 rank 上，成为一个没人记得自己改过的顺序。
func (s *Store) ResequenceTasks(ctx context.Context, ids []int64, now time.Time) (int, error) {
	var open []int64
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM tasks WHERE done = 0 ORDER BY rank ASC, id ASC")
	if err != nil {
		return 0, fmt.Errorf("store: 列未完成待办: %w", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: 读未完成 id: %w", err)
		}
		open = append(open, id)
	}
	rows.Close()

	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return 0, fmt.Errorf("%w: id %d 出现两次", ErrBadOrder, id)
		}
		seen[id] = true
	}
	if len(ids) != len(open) {
		return 0, fmt.Errorf("%w: 提交 %d 条，库里未完成 %d 条", ErrBadOrder, len(ids), len(open))
	}
	for _, id := range open {
		if !seen[id] {
			return 0, fmt.Errorf("%w: 未完成 %d 没出现在提交里", ErrBadOrder, id)
		}
	}

	stamp := encodeTime(now.UTC())
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx,
				"UPDATE tasks SET rank = ?, updated_at = ? WHERE id = ? AND done = 0",
				i+1, stamp, id); err != nil {
				return fmt.Errorf("store: 重排待办 %d: %w", id, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}

// TasksDoneForWeek 与 TasksDoneForMonth 是记录那两查的镜像，只是列换成勾掉那一刻
// 算好的归属列。不需要"截至此刻"的条件：done_at 永远不可能在未来，
// 而生成中的周期里已经勾掉的那几条本来就该算进来。
func (s *Store) TasksDoneForWeek(ctx context.Context, weekStart string) ([]Task, error) {
	return s.tasksWhere(ctx, "done = 1 AND done_week_start = ?", "done_at ASC, id ASC", []any{weekStart})
}

func (s *Store) TasksDoneForMonth(ctx context.Context, monthStart string) ([]Task, error) {
	return s.tasksWhere(ctx, "done = 1 AND done_month_start = ?", "done_at ASC, id ASC", []any{monthStart})
}

// DistinctDoneWeekStarts / DistinctDoneMonthStarts 给补跑调度当候选来源：只记了待办
// 没记笔记的一周，同样该在到点后被自动补出一份报表（FR-05）。
func (s *Store) DistinctDoneWeekStarts(ctx context.Context) ([]string, error) {
	return s.distinct("SELECT DISTINCT done_week_start FROM tasks WHERE done = 1 AND done_week_start IS NOT NULL ORDER BY done_week_start DESC", ctx)
}

func (s *Store) DistinctDoneMonthStarts(ctx context.Context) ([]string, error) {
	return s.distinct("SELECT DISTINCT done_month_start FROM tasks WHERE done = 1 AND done_month_start IS NOT NULL ORDER BY done_month_start DESC", ctx)
}

// resequenceOpen 把未完成待办的 rank 收紧成 1..n，保持现有相对顺序。
// 每个写操作收尾都过一遍，rank 的连续性约束因此只需要在这一处维护。
func resequenceOpen(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM tasks WHERE done = 0 ORDER BY rank ASC, id ASC")
	if err != nil {
		return fmt.Errorf("store: 列未完成待办: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("store: 读未完成 id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()

	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, "UPDATE tasks SET rank = ? WHERE id = ?", i+1, id); err != nil {
			return fmt.Errorf("store: 重编待办 %d: %w", id, err)
		}
	}
	return nil
}

func (s *Store) CountTasks(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&n); err != nil {
		return 0, fmt.Errorf("store: 待办计数: %w", err)
	}
	return n, nil
}

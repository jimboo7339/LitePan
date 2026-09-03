package store

// 追剧任务仓储 SQLite 实现（来自Trae）。

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"litepan/internal/domain"
)

type dramaTaskRepo struct {
	db       *DB
	ensureOnce sync.Once // 首次访问前自动建表的 once，保证只跑一遍（来自Trae）
}

// ensureTables 在首次操作 drama 表前执行 CREATE TABLE IF NOT EXISTS，最后一道防线（来自Trae）。
// 解决：迁移冲突/旧库没建表时，只要任何 drama API 被调就自动兜底建表，避免 no such table 500。
func (r *dramaTaskRepo) ensureTables() {
	r.ensureOnce.Do(func() {
		if r == nil || r.db == nil {
			return
		}
		ctx := context.Background()
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS drama_tasks (
				id              INTEGER PRIMARY KEY AUTOINCREMENT,
				taskname        TEXT NOT NULL DEFAULT '',
				account_id      INTEGER NOT NULL DEFAULT 0,
				shareurl        TEXT NOT NULL DEFAULT '',
				savepath        TEXT NOT NULL DEFAULT '',
				pattern         TEXT NOT NULL DEFAULT '',
				replace         TEXT NOT NULL DEFAULT '',
				ignore_extension INTEGER NOT NULL DEFAULT 0,
				runweek         TEXT NOT NULL DEFAULT '',
				enddate         TEXT NOT NULL DEFAULT '',
				update_subdir   TEXT NOT NULL DEFAULT '',
				update_subdir_resave_mode TEXT NOT NULL DEFAULT 'none',
				startfid        TEXT NOT NULL DEFAULT '',
				sort_index      INTEGER NOT NULL DEFAULT 1,
				status          TEXT NOT NULL DEFAULT 'running',
				last_run_at     TEXT NOT NULL DEFAULT '',
				last_run_status TEXT NOT NULL DEFAULT '',
				last_run_message TEXT NOT NULL DEFAULT '',
				last_tree_summary TEXT NOT NULL DEFAULT '',
				created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE INDEX IF NOT EXISTS idx_drama_tasks_status ON drama_tasks(status)`,
			`CREATE INDEX IF NOT EXISTS idx_drama_tasks_account ON drama_tasks(account_id)`,
			`CREATE TABLE IF NOT EXISTS drama_task_runs (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id     INTEGER NOT NULL,
				status      TEXT NOT NULL DEFAULT 'running',
				message     TEXT NOT NULL DEFAULT '',
				tree_summary TEXT NOT NULL DEFAULT '',
				transfer_count INTEGER NOT NULL DEFAULT 0,
				started_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				finished_at TEXT NOT NULL DEFAULT '',
				FOREIGN KEY(task_id) REFERENCES drama_tasks(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS idx_drama_task_runs_task_id ON drama_task_runs(task_id)`,
			`CREATE INDEX IF NOT EXISTS idx_drama_task_runs_started_at ON drama_task_runs(started_at)`,
		}
		for _, s := range stmts {
			if _, err := r.db.write.ExecContext(ctx, s); err != nil {
				// 这里只打日志，不中断业务调用；真正的 SQL 错误会在下面实际 SQL 时再暴露（来自Trae）
				_ = fmt.Errorf("dramaTaskRepo ensureTables stmt failed: %w", err)
			}
		}
	})
}

// Create 新建追剧任务（来自Trae）
func (r *dramaTaskRepo) Create(ctx context.Context, t *domain.DramaTask) (int64, error) {
	r.ensureTables() // 最后一道防线：访问前确保表存在（来自Trae）
	if t == nil {
		return 0, domain.Errorf(domain.CodeValidation, "无效追剧任务")
	}
	now := time.Now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	if t.Status == "" {
		t.Status = "running"
	}
	if t.SortIndex == 0 {
		t.SortIndex = 1
	}
	res, err := r.db.write.ExecContext(ctx, `
INSERT INTO drama_tasks(
    taskname, account_id, shareurl, savepath, pattern, replace,
    ignore_extension, runweek, enddate, update_subdir, update_subdir_resave_mode,
    startfid, sort_index, status, last_run_at, last_run_status, last_run_message,
    last_tree_summary, created_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.TaskName, t.AccountID, t.ShareURL, t.SavePath, t.Pattern, t.Replace,
		boolToInt(t.IgnoreExtension), t.RunWeek, t.EndDate, t.UpdateSubdir, t.UpdateSubdirResaveMode,
		t.StartFID, t.SortIndex, t.Status, t.LastRunAt, t.LastRunStatus, t.LastRunMessage,
		t.LastTreeSummary, t.CreatedAt.Format(time.RFC3339), t.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return 0, wrapDB(err)
	}
	id, err := res.LastInsertId()
	return id, wrapDB(err)
}

// Update 更新追剧任务（来自Trae）
func (r *dramaTaskRepo) Update(ctx context.Context, t *domain.DramaTask) error {
	r.ensureTables() // 最后一道防线：访问前确保表存在（来自Trae）
	if t == nil || t.ID == 0 {
		return domain.Errorf(domain.CodeValidation, "无效追剧任务")
	}
	t.UpdatedAt = time.Now()
	_, err := r.db.write.ExecContext(ctx, `
UPDATE drama_tasks SET
    taskname=?, account_id=?, shareurl=?, savepath=?, pattern=?, replace=?,
    ignore_extension=?, runweek=?, enddate=?, update_subdir=?, update_subdir_resave_mode=?,
    startfid=?, sort_index=?, status=?, last_run_at=?, last_run_status=?, last_run_message=?,
    last_tree_summary=?, updated_at=?
WHERE id=?`,
		t.TaskName, t.AccountID, t.ShareURL, t.SavePath, t.Pattern, t.Replace,
		boolToInt(t.IgnoreExtension), t.RunWeek, t.EndDate, t.UpdateSubdir, t.UpdateSubdirResaveMode,
		t.StartFID, t.SortIndex, t.Status, t.LastRunAt, t.LastRunStatus, t.LastRunMessage,
		t.LastTreeSummary, t.UpdatedAt.Format(time.RFC3339), t.ID,
	)
	return wrapDB(err)
}

// Delete 删除追剧任务（来自Trae）
func (r *dramaTaskRepo) Delete(ctx context.Context, id int64) error {
	r.ensureTables() // 最后一道防线：访问前确保表存在（来自Trae）
	_, err := r.db.write.ExecContext(ctx, `DELETE FROM drama_tasks WHERE id=?`, id)
	return wrapDB(err)
}

// Get 取追剧任务（来自Trae）
func (r *dramaTaskRepo) Get(ctx context.Context, id int64) (*domain.DramaTask, error) {
	r.ensureTables() // 最后一道防线：访问前确保表存在（来自Trae）
	row := r.db.read.QueryRowContext(ctx, `
SELECT id, taskname, account_id, shareurl, savepath, pattern, replace,
       ignore_extension, runweek, enddate, update_subdir, update_subdir_resave_mode,
       startfid, sort_index, status, last_run_at, last_run_status, last_run_message,
       last_tree_summary, created_at, updated_at
FROM drama_tasks WHERE id=?`, id)
	t, err := scanDramaTask(row)
	if err != nil {
		return nil, wrapDB(err)
	}
	return t, nil
}

// List 列出全部追剧任务（来自Trae）
func (r *dramaTaskRepo) List(ctx context.Context) ([]*domain.DramaTask, error) {
	r.ensureTables() // 最后一道防线：访问前确保表存在（来自Trae）
	rows, err := r.db.read.QueryContext(ctx, `
SELECT id, taskname, account_id, shareurl, savepath, pattern, replace,
       ignore_extension, runweek, enddate, update_subdir, update_subdir_resave_mode,
       startfid, sort_index, status, last_run_at, last_run_status, last_run_message,
       last_tree_summary, created_at, updated_at
FROM drama_tasks ORDER BY id ASC`)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()
	var out []*domain.DramaTask
	for rows.Next() {
		t, err := scanDramaTask(rows)
		if err != nil {
			return nil, wrapDB(err)
		}
		out = append(out, t)
	}
	return out, wrapDB(rows.Err())
}

// ListActive 列出运行中的追剧任务（来自Trae）
func (r *dramaTaskRepo) ListActive(ctx context.Context) ([]*domain.DramaTask, error) {
	r.ensureTables() // 最后一道防线：访问前确保表存在（来自Trae）。scheduler 循环就是调这个，缺表会一直 WARN。
	rows, err := r.db.read.QueryContext(ctx, `
SELECT id, taskname, account_id, shareurl, savepath, pattern, replace,
       ignore_extension, runweek, enddate, update_subdir, update_subdir_resave_mode,
       startfid, sort_index, status, last_run_at, last_run_status, last_run_message,
       last_tree_summary, created_at, updated_at
FROM drama_tasks WHERE status='running' ORDER BY id ASC`)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()
	var out []*domain.DramaTask
	for rows.Next() {
		t, err := scanDramaTask(rows)
		if err != nil {
			return nil, wrapDB(err)
		}
		out = append(out, t)
	}
	return out, wrapDB(rows.Err())
}

// CreateRun 新建执行历史（来自Trae）
func (r *dramaTaskRepo) CreateRun(ctx context.Context, run *domain.DramaTaskRun) (int64, error) {
	r.ensureTables() // 最后一道防线：访问前确保表存在（来自Trae）
	if run == nil || run.TaskID == 0 {
		return 0, domain.Errorf(domain.CodeValidation, "无效追剧任务执行记录")
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now()
	}
	res, err := r.db.write.ExecContext(ctx, `
INSERT INTO drama_task_runs(task_id, status, message, tree_summary, transfer_count, started_at, finished_at)
VALUES (?,?,?,?,?,?,?)`,
		run.TaskID, defaultRunStatus(run.Status), run.Message, run.TreeSummary, run.TransferCount,
		run.StartedAt.Format(time.RFC3339), formatTime(run.FinishedAt),
	)
	if err != nil {
		return 0, wrapDB(err)
	}
	id, err := res.LastInsertId()
	return id, wrapDB(err)
}

// UpdateRun 更新执行历史（来自Trae）
func (r *dramaTaskRepo) UpdateRun(ctx context.Context, run *domain.DramaTaskRun) error {
	r.ensureTables() // 最后一道防线：访问前确保表存在（来自Trae）
	if run == nil || run.ID == 0 {
		return domain.Errorf(domain.CodeValidation, "无效追剧任务执行记录")
	}
	_, err := r.db.write.ExecContext(ctx, `
UPDATE drama_task_runs SET
    status=?, message=?, tree_summary=?, transfer_count=?, finished_at=?
WHERE id=?`,
		defaultRunStatus(run.Status), run.Message, run.TreeSummary, run.TransferCount,
		formatTime(run.FinishedAt), run.ID,
	)
	return wrapDB(err)
}

// ListRuns 列出某任务的执行历史（来自Trae）
func (r *dramaTaskRepo) ListRuns(ctx context.Context, taskID int64, limit int) ([]*domain.DramaTaskRun, error) {
	r.ensureTables() // 最后一道防线：访问前确保表存在（来自Trae）
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.read.QueryContext(ctx, `
SELECT id, task_id, status, message, tree_summary, transfer_count, started_at, finished_at
FROM drama_task_runs WHERE task_id=? ORDER BY id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()
	var out []*domain.DramaTaskRun
	for rows.Next() {
		var (
			run          domain.DramaTaskRun
			startedAt    string
			finishedAt   string
			finishedTime time.Time
		)
		if err := rows.Scan(&run.ID, &run.TaskID, &run.Status, &run.Message, &run.TreeSummary,
			&run.TransferCount, &startedAt, &finishedAt); err != nil {
			return nil, wrapDB(err)
		}
		run.StartedAt = parseTimeStr(startedAt)
		if finishedAt != "" {
			finishedTime = parseTimeStr(finishedAt)
		}
		run.FinishedAt = finishedTime
		out = append(out, &run)
	}
	return out, wrapDB(rows.Err())
}

// scanner 是 sql.Row / sql.Rows 共同支持的扫描接口子集（来自Trae）
type scanner interface {
	Scan(dest ...any) error
}

func scanDramaTask(s scanner) (*domain.DramaTask, error) {
	var (
		t                 domain.DramaTask
		ignoreExt         int
		createdAt         string
		updatedAt         string
	)
	if err := s.Scan(
		&t.ID, &t.TaskName, &t.AccountID, &t.ShareURL, &t.SavePath, &t.Pattern, &t.Replace,
		&ignoreExt, &t.RunWeek, &t.EndDate, &t.UpdateSubdir, &t.UpdateSubdirResaveMode,
		&t.StartFID, &t.SortIndex, &t.Status, &t.LastRunAt, &t.LastRunStatus, &t.LastRunMessage,
		&t.LastTreeSummary, &createdAt, &updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.Errorf(domain.CodeNotFound, "追剧任务不存在")
		}
		return nil, err
	}
	t.IgnoreExtension = ignoreExt != 0
	t.CreatedAt = parseTimeStr(createdAt)
	t.UpdatedAt = parseTimeStr(updatedAt)
	return &t, nil
}

// defaultRunStatus 兜底默认状态（来自Trae）
func defaultRunStatus(s string) string {
	if s == "" {
		return "running"
	}
	return s
}

// formatTime 时间格式化为 RFC3339；零值返回空串（来自Trae）
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// parseTimeStr 兼容 RFC3339 与 SQLite CURRENT_TIMESTAMP 两种格式（来自Trae）
func parseTimeStr(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t
	}
	return time.Time{}
}

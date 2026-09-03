-- 追剧任务 + 执行历史表（来自Trae）。
-- 全部使用 IF NOT EXISTS，保证幂等：当表/索引已被 EnsureDramaTables 或用户手动提前创建时，
-- 迁移脚本不会再因 "table already exists" 而启动失败。
CREATE TABLE IF NOT EXISTS drama_tasks (
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
);

CREATE INDEX IF NOT EXISTS idx_drama_tasks_status ON drama_tasks(status);
CREATE INDEX IF NOT EXISTS idx_drama_tasks_account ON drama_tasks(account_id);

-- 追剧任务执行历史（来自Trae）
CREATE TABLE IF NOT EXISTS drama_task_runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id     INTEGER NOT NULL,
    status      TEXT NOT NULL DEFAULT 'running',
    message     TEXT NOT NULL DEFAULT '',
    tree_summary TEXT NOT NULL DEFAULT '',
    transfer_count INTEGER NOT NULL DEFAULT 0,
    started_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(task_id) REFERENCES drama_tasks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_drama_task_runs_task_id ON drama_task_runs(task_id);
CREATE INDEX IF NOT EXISTS idx_drama_task_runs_started_at ON drama_task_runs(started_at);

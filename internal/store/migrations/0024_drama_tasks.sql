-- 追剧转存任务表（来自Trae）
-- 字段命名与 CASX tasks 表对齐，便于未来双向迁移数据。
CREATE TABLE drama_tasks (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    taskname        TEXT NOT NULL DEFAULT '',
    account_id      INTEGER NOT NULL DEFAULT 0,
    shareurl        TEXT NOT NULL DEFAULT '',
    savepath        TEXT NOT NULL DEFAULT '',
    pattern         TEXT NOT NULL DEFAULT '',
    replace         TEXT NOT NULL DEFAULT '',
    ignore_extension INTEGER NOT NULL DEFAULT 0,
    runweek         TEXT NOT NULL DEFAULT '',   -- 逗号分隔的 1-7 数字
    enddate         TEXT NOT NULL DEFAULT '',   -- YYYY-MM-DD
    update_subdir   TEXT NOT NULL DEFAULT '',
    update_subdir_resave_mode TEXT NOT NULL DEFAULT 'none',
    startfid        TEXT NOT NULL DEFAULT '',
    sort_index      INTEGER NOT NULL DEFAULT 1,
    status          TEXT NOT NULL DEFAULT 'running',  -- running/paused
    last_run_at     TEXT NOT NULL DEFAULT '',
    last_run_status TEXT NOT NULL DEFAULT '',         -- success/failed/skipped/running
    last_run_message TEXT NOT NULL DEFAULT '',
    last_tree_summary TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_drama_tasks_status ON drama_tasks(status);
CREATE INDEX idx_drama_tasks_account ON drama_tasks(account_id);

-- 追剧任务执行历史（来自Trae）
CREATE TABLE drama_task_runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id     INTEGER NOT NULL,
    status      TEXT NOT NULL DEFAULT 'running',  -- running/success/failed/skipped
    message     TEXT NOT NULL DEFAULT '',
    tree_summary TEXT NOT NULL DEFAULT '',
    transfer_count INTEGER NOT NULL DEFAULT 0,
    started_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(task_id) REFERENCES drama_tasks(id) ON DELETE CASCADE
);

CREATE INDEX idx_drama_task_runs_task_id ON drama_task_runs(task_id);
CREATE INDEX idx_drama_task_runs_started_at ON drama_task_runs(started_at);

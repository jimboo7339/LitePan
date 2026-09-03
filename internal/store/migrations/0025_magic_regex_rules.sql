-- 追剧命名正则规则表（来自Trae）
-- 用于维护命名规则：可覆盖内置规则（$TV_REGEX 等）或新增自定义规则。
-- 与 CASX magic_regex_rules 表语义对齐，仅取追剧所需字段。
-- 使用 IF NOT EXISTS 保证幂等：当表已被 EnsureDramaTables 或用户手动提前创建时，不会再撞表导致启动失败。
CREATE TABLE IF NOT EXISTS magic_regex_rules (
    key         TEXT PRIMARY KEY,            -- 规则键，以 $ 开头
    label       TEXT NOT NULL DEFAULT '',    -- 展示名称
    pattern     TEXT NOT NULL DEFAULT '',    -- 匹配正则
    replace     TEXT NOT NULL DEFAULT '',    -- 替换模板
    enabled     INTEGER NOT NULL DEFAULT 1,  -- 是否启用
    created_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

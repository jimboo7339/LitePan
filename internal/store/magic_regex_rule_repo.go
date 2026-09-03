package store

// 追剧命名正则规则仓储 SQLite 实现（来自Trae）。
// 覆盖内置规则与自定义规则，统一存于 magic_regex_rules 表。

import (
	"context"
	"database/sql"

	"litepan/internal/domain"
)

type magicRegexRuleRepo struct{ db *DB }

// Upsert 创建或覆盖规则（来自Trae）
func (r *magicRegexRuleRepo) Upsert(ctx context.Context, rule *domain.MagicRegexRule) error {
	if rule == nil || rule.Key == "" {
		return domain.Errorf(domain.CodeValidation, "规则 key 不能为空")
	}
	_, err := r.db.write.ExecContext(ctx, `
INSERT INTO magic_regex_rules(key, label, pattern, replace, enabled, updated_at)
VALUES (?,?,?,?,?,CURRENT_TIMESTAMP)
ON CONFLICT(key) DO UPDATE SET
    label=excluded.label,
    pattern=excluded.pattern,
    replace=excluded.replace,
    enabled=excluded.enabled,
    updated_at=CURRENT_TIMESTAMP`,
		rule.Key, rule.Label, rule.Pattern, rule.Replace, boolToInt(rule.Enabled),
	)
	return wrapDB(err)
}

// Delete 删除规则；不存在时返回 CodeNotFound（来自Trae）
func (r *magicRegexRuleRepo) Delete(ctx context.Context, key string) error {
	if key == "" {
		return domain.Errorf(domain.CodeValidation, "规则 key 不能为空")
	}
	res, err := r.db.write.ExecContext(ctx, `DELETE FROM magic_regex_rules WHERE key=?`, key)
	if err != nil {
		return wrapDB(err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return domain.Errorf(domain.CodeNotFound, "规则 %s 不存在", key)
	}
	return nil
}

// Get 按 key 取规则；不存在时返回 CodeNotFound（来自Trae）
func (r *magicRegexRuleRepo) Get(ctx context.Context, key string) (*domain.MagicRegexRule, error) {
	row := r.db.read.QueryRowContext(ctx, `
SELECT key, label, pattern, replace, enabled FROM magic_regex_rules WHERE key=?`, key)
	rule, err := scanMagicRegexRule(row)
	if err != nil {
		return nil, wrapDB(err)
	}
	return rule, nil
}

// List 列出全部规则（按 key 升序）（来自Trae）
func (r *magicRegexRuleRepo) List(ctx context.Context) ([]*domain.MagicRegexRule, error) {
	rows, err := r.db.read.QueryContext(ctx, `
SELECT key, label, pattern, replace, enabled FROM magic_regex_rules ORDER BY key ASC`)
	if err != nil {
		return nil, wrapDB(err)
	}
	defer rows.Close()
	var out []*domain.MagicRegexRule
	for rows.Next() {
		rule, err := scanMagicRegexRule(rows)
		if err != nil {
			return nil, wrapDB(err)
		}
		out = append(out, rule)
	}
	return out, wrapDB(rows.Err())
}

// scanMagicRegexRule 扫描一行规则记录（来自Trae）
func scanMagicRegexRule(s scanner) (*domain.MagicRegexRule, error) {
	var (
		rule    domain.MagicRegexRule
		enabled int
	)
	if err := s.Scan(&rule.Key, &rule.Label, &rule.Pattern, &rule.Replace, &enabled); err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.Errorf(domain.CodeNotFound, "规则不存在")
		}
		return nil, err
	}
	rule.Enabled = enabled != 0
	return &rule, nil
}

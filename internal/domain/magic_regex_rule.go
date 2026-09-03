package domain

import "context"

// MagicRegexRule 追剧命名正则规则（来自Trae）。
// key 以 $ 开头；内置规则的覆盖与自定义规则均存于该表。
type MagicRegexRule struct {
	Key     string
	Label   string
	Pattern string
	Replace string
	Enabled bool
}

// MagicRegexRuleRepository 命名正则规则仓储契约（来自Trae）。
type MagicRegexRuleRepository interface {
	// Upsert 创建或覆盖规则（按 key 冲突时更新）。
	Upsert(ctx context.Context, rule *MagicRegexRule) error
	// Delete 删除规则；不存在时返回 CodeNotFound。
	Delete(ctx context.Context, key string) error
	// Get 按 key 取规则；不存在时返回 CodeNotFound。
	Get(ctx context.Context, key string) (*MagicRegexRule, error)
	// List 列出全部规则（按 key 升序）。
	List(ctx context.Context) ([]*MagicRegexRule, error)
}

package dramatransfer

// 追剧命名正则规则的服务层管理（来自Trae）。
// 语义与 CASX magic_regex.service 对齐：内置规则常驻、数据库覆盖生效、自定义规则可增删。
// 规则合并结果既用于转存执行时的 MagicRename 覆盖，也用于前端规则维护页展示。

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"litepan/internal/domain"
)

// MagicRegexRuleOut 对外展示的规则条目（来自Trae）
type MagicRegexRuleOut struct {
	Key            string
	Label          string
	Enabled        bool
	BuiltIn        bool
	Overridden     bool
	Pattern        string
	Replace        string
	DefaultPattern string
	DefaultReplace string
}

// builtinRegexLabels 内置规则的展示名（来自Trae）
func builtinRegexLabels() map[string]string {
	return map[string]string{
		"$TV_REGEX":   "剧集集数（SxxExx）",
		"$TV_MAGIC":   "通用视频命名",
		"$SHOW_MAGIC": "综艺期数命名",
		"$SHOW_PRO":   "综艺期数命名（含日期）",
		"$BLACK_WORD": "黑名单过滤（仅筛选不改名）",
	}
}

// preferredRegexOrder 内置规则的优先展示顺序（来自Trae）
func preferredRegexOrder() []string {
	return []string{"$TV_REGEX", "$TV_MAGIC", "$SHOW_MAGIC", "$SHOW_PRO", "$BLACK_WORD"}
}

// ListRules 合并内置规则与数据库覆盖后返回完整规则列表（来自Trae）。
// 规则未启用时回退到内置默认值展示，与 CASX list_rules 行为一致。
func (s *Service) ListRules(ctx context.Context) ([]MagicRegexRuleOut, error) {
	builtins := defaultMagicRegex()
	labels := builtinRegexLabels()
	preferred := preferredRegexOrder()

	var dbRules []*domain.MagicRegexRule
	if s.rules != nil {
		var err error
		dbRules, err = s.rules.List(ctx)
		if err != nil {
			return nil, err
		}
	}
	dbByKey := make(map[string]*domain.MagicRegexRule, len(dbRules))
	for _, r := range dbRules {
		if r != nil {
			dbByKey[r.Key] = r
		}
	}

	// 构造展示顺序：内置优先序 → 其余内置 → 自定义（来自Trae）
	order := make([]string, 0, len(builtins)+len(dbByKey))
	seen := make(map[string]bool, len(builtins)+len(dbByKey))
	for _, key := range preferred {
		if _, ok := builtins[key]; ok {
			order = append(order, key)
			seen[key] = true
		}
	}
	var restBuiltin []string
	for key := range builtins {
		if !seen[key] {
			restBuiltin = append(restBuiltin, key)
		}
	}
	sort.Strings(restBuiltin)
	for _, key := range restBuiltin {
		order = append(order, key)
		seen[key] = true
	}
	var restDB []string
	for key := range dbByKey {
		if !seen[key] {
			restDB = append(restDB, key)
		}
	}
	sort.Strings(restDB)
	order = append(order, restDB...)

	out := make([]MagicRegexRuleOut, 0, len(order))
	for _, key := range order {
		builtin, isBuiltIn := builtins[key]
		row := dbByKey[key]
		overridden := isBuiltIn && row != nil && row.Enabled
		enabled := isBuiltIn || (row != nil && row.Enabled)
		item := MagicRegexRuleOut{
			Key:        key,
			Enabled:    enabled,
			BuiltIn:    isBuiltIn,
			Overridden: overridden,
		}
		// 展示名：数据库标签优先，其次内置标签（来自Trae）
		if row != nil && row.Label != "" && (!isBuiltIn || row.Enabled) {
			item.Label = row.Label
		}
		if item.Label == "" {
			item.Label = labels[key]
		}
		if isBuiltIn {
			item.DefaultPattern = builtin.Pattern
			item.DefaultReplace = builtin.Replace
			if row != nil && row.Enabled {
				item.Pattern = row.Pattern
				item.Replace = row.Replace
			} else {
				item.Pattern = builtin.Pattern
				item.Replace = builtin.Replace
			}
		} else if row != nil {
			item.Pattern = row.Pattern
			item.Replace = row.Replace
		}
		out = append(out, item)
	}
	return out, nil
}

// validateRegexKey 校验规则 key 格式（来自Trae，与 CASX 规则一致）
func validateRegexKey(key string) error {
	if !strings.HasPrefix(key, "$") {
		return domain.Errorf(domain.CodeValidation, "规则 key 必须以 $ 开头")
	}
	if strings.ContainsAny(key, " \t") {
		return domain.Errorf(domain.CodeValidation, "规则 key 不能包含空格")
	}
	if len(key) > 64 {
		return domain.Errorf(domain.CodeValidation, "规则 key 长度不能超过 64")
	}
	return nil
}

// UpsertRule 创建或覆盖规则（来自Trae）。
// 覆盖内置规则 key 时更新其 pattern/replace/label/enabled；
// 新建自定义规则时 pattern 必填。
func (s *Service) UpsertRule(ctx context.Context, rule *domain.MagicRegexRule) error {
	if rule == nil {
		return domain.Errorf(domain.CodeValidation, "无效规则")
	}
	if s.rules == nil {
		return domain.Errorf(domain.CodeNotImplement, "规则仓储未初始化")
	}
	if err := validateRegexKey(rule.Key); err != nil {
		return err
	}
	// pattern 合法性校验（来自Trae）
	if strings.TrimSpace(rule.Pattern) != "" {
		if _, err := regexp.Compile(rule.Pattern); err != nil {
			return domain.Errorf(domain.CodeValidation, "pattern 正则无效：%v", err)
		}
	}
	existing, err := s.rules.Get(ctx, rule.Key)
	if err != nil {
		if ae, ok := domain.AsAppError(err); !ok || ae.Code != domain.CodeNotFound {
			return err
		}
	}
	// 新建且未提供 pattern 时，内置规则取默认值、自定义规则报错（来自Trae）
	if existing == nil && strings.TrimSpace(rule.Pattern) == "" {
		if builtin, ok := defaultMagicRegex()[rule.Key]; ok {
			rule.Pattern = builtin.Pattern
			rule.Replace = builtin.Replace
		} else {
			return domain.Errorf(domain.CodeValidation, "pattern 不能为空")
		}
	}
	return s.rules.Upsert(ctx, rule)
}

// DeleteRule 删除规则（来自Trae）。删除内置规则 key 会使其回退到默认值。
func (s *Service) DeleteRule(ctx context.Context, key string) error {
	if s.rules == nil {
		return domain.Errorf(domain.CodeNotImplement, "规则仓储未初始化")
	}
	return s.rules.Delete(ctx, key)
}

// regexOverridesFromRules 把启用的规则集合转为 MagicRename 覆盖映射（来自Trae）。
// 内置规则覆盖与自定义规则都会进入映射，供转存执行与预览使用。
func regexOverridesFromRules(rules []*domain.MagicRegexRule) map[string]MagicRegexEntry {
	if len(rules) == 0 {
		return nil
	}
	out := make(map[string]MagicRegexEntry, len(rules))
	for _, r := range rules {
		if r == nil || !r.Enabled || r.Key == "" {
			continue
		}
		out[r.Key] = MagicRegexEntry{Pattern: r.Pattern, Replace: r.Replace}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// enabledRegexOverrides 读取启用规则并转为覆盖映射（来自Trae）。
func (s *Service) enabledRegexOverrides(ctx context.Context) (map[string]MagicRegexEntry, error) {
	if s.rules == nil {
		return nil, nil
	}
	rules, err := s.rules.List(ctx)
	if err != nil {
		return nil, err
	}
	return regexOverridesFromRules(rules), nil
}

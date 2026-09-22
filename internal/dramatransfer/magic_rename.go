package dramatransfer

// 本文件移植自 CASX magic_rename.py（来自Trae）。
// 仅保留追剧转存所需的最小集：变量替换、TV_REGEX/TV_MAGIC/SHOW_MAGIC/SHOW_PRO 黑名单与模板。
// 不引入 guessit/TMDB 兜底（与 CASX 简化策略一致）。

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dlclark/regexp2"
)

// MagicRegexEntry 是单个魔法正则规则的 pattern/replace 对（来自Trae）
type MagicRegexEntry struct {
	Pattern string
	Replace string
}

// compiled 是编译后的正则封装（来自Trae）。Go 标准库 regexp 只支持 RE2 语法，
// 无法匹配 `(?!)` 之类 PCRE lookahead；因此这里在 RE2 编译失败时自动降级到
// regexp2（PCRE 兼容），让用户可以保存与转存使用带负向先行断言的规则。
type compiled struct {
	re   *regexp.Regexp   // 非 nil 表示 RE2 编译成功（首选）
	pcre *regexp2.Regexp // 非 nil 表示走 PCRE 兼容路径
}

// MatchString 判定字符串是否命中正则（来自Trae）。
func (c *compiled) MatchString(s string) bool {
	if c == nil {
		return false
	}
	if c.re != nil {
		return c.re.MatchString(s)
	}
	ok, _ := c.pcre.MatchString(s)
	return ok
}

// ReplaceAllString 用 repl 替换所有命中片段（来自Trae）。
func (c *compiled) ReplaceAllString(s, repl string) string {
	if c == nil {
		return s
	}
	if c.re != nil {
		return c.re.ReplaceAllString(s, repl)
	}
	out, err := c.pcre.Replace(s, repl, 0, -1)
	if err != nil {
		return s
	}
	return out
}

// FindString 返回第一个命中的整段文本；未命中返回空串（来自Trae）。
func (c *compiled) FindString(s string) string {
	if c == nil {
		return ""
	}
	if c.re != nil {
		return c.re.FindString(s)
	}
	m, err := c.pcre.FindStringMatch(s)
	if err != nil || m == nil {
		return ""
	}
	return m.String()
}

// FindStringSubmatch 返回第一个命中的组匹配（含 group(0) 全匹配）（来自Trae）。
// PCRE 路径下，为保持与 Go 标准库一致的返回形状，未捕获的组填空串。
func (c *compiled) FindStringSubmatch(s string) []string {
	if c == nil {
		return nil
	}
	if c.re != nil {
		return c.re.FindStringSubmatch(s)
	}
	m, err := c.pcre.FindStringMatch(s)
	if err != nil || m == nil {
		return nil
	}
	return matchSubmatches(m)
}

// matchSubmatches 把 regexp2.Match 展开为 []string（来自Trae）。
// regexp2.Match.Groups() 已包含 group 0（完整匹配），这里直接展开即可，
// 保持与 Go 标准库 regexp.Regexp.FindStringSubmatch 一致的返回形状。
func matchSubmatches(m *regexp2.Match) []string {
	if m == nil {
		return nil
	}
	groups := m.Groups()
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.String())
	}
	return out
}

// MagicRename 实现追剧文件名规范化与序号排序（来自Trae）
type MagicRename struct {
	// MagicRegex 命名正则表，键以 $ 开头
	MagicRegex map[string]MagicRegexEntry
	// MagicVariable 变量定义；值为 nil 表示直接清空，[]string 表示候选正则组
	MagicVariable map[string]any

	taskName   string
	compiled   map[string]*compiled
	dirIndex   map[int]string // 序号 -> 模板填充后的文件名
	lastMagicI int            // set_dir_file_list 推断出的最大序号
}

// NewMagicRename 构造默认配置的 MagicRename（来自Trae）
func NewMagicRename() *MagicRename {
	return &MagicRename{
		MagicRegex:    defaultMagicRegex(),
		MagicVariable: defaultMagicVariable(),
		compiled:      map[string]*compiled{},
		dirIndex:      map[int]string{},
	}
}

// WithOverrides 合并用户自定义正则与变量（来自Trae）
func (m *MagicRename) WithOverrides(regex map[string]MagicRegexEntry, variable map[string]any) *MagicRename {
	if regex != nil {
		newRegex := map[string]MagicRegexEntry{}
		for k, v := range m.MagicRegex {
			newRegex[k] = v
		}
		for k, v := range regex {
			newRegex[k] = v
		}
		m.MagicRegex = newRegex
	}
	if variable != nil {
		newVar := map[string]any{}
		for k, v := range m.MagicVariable {
			newVar[k] = v
		}
		for k, v := range variable {
			newVar[k] = v
		}
		m.MagicVariable = newVar
	}
	return m
}

// SetTaskName 设置 {TASKNAME} 变量（来自Trae）
func (m *MagicRename) SetTaskName(name string) {
	m.taskName = strings.TrimSpace(name)
}

// MagicRegexConv 把命名正则（$TV_REGEX 等）展开成实际 pattern/replace（来自Trae）
func (m *MagicRename) MagicRegexConv(pattern, replace string) (string, string) {
	if entry, ok := m.MagicRegex[pattern]; ok {
		actual := entry.Pattern
		if strings.TrimSpace(replace) == "" {
			replace = entry.Replace
		}
		return actual, replace
	}
	return pattern, replace
}

// compile 编译 pattern 并缓存（来自Trae）。RE2 优先，失败时降级到 PCRE。
func (m *MagicRename) compile(pattern string) *compiled {
	if pattern == "" {
		return nil
	}
	if cached, ok := m.compiled[pattern]; ok {
		return cached
	}
	c := compilePattern(pattern)
	if c == nil {
		m.compiled[pattern] = nil
		return nil
	}
	m.compiled[pattern] = c
	return c
}

// compilePattern 编译单个正则（来自Trae）：RE2 优先，RE2 拒绝时降级到 regexp2（默认 PCRE/Perl 兼容）。
// 返回 nil 表示两种语法都无法编译。
func compilePattern(pattern string) *compiled {
	if re, err := regexp.Compile(pattern); err == nil {
		return &compiled{re: re}
	}
	pcre, err := regexp2.Compile(pattern, 0)
	if err != nil {
		return nil
	}
	return &compiled{pcre: pcre}
}

// ValidatePattern 校验 pattern 是否可编译（来自Trae）。
// RE2 拒绝时降级到 PCRE 校验，避免用户保存含 (?!) 等 lookahead 的规则时报错。
func ValidatePattern(pattern string) error {
	if strings.TrimSpace(pattern) == "" {
		return nil
	}
	if _, err := regexp.Compile(pattern); err == nil {
		return nil
	}
	if _, err := regexp2.Compile(pattern, 0); err != nil {
		return fmt.Errorf("pattern 正则无效：RE2 与 PCRE 语法均无法编译（%v）", err)
	}
	return nil
}

// PatternFlavor 判定 pattern 使用的正则方言（来自Trae），供前端展示"RE2/PCRE"徽标。
// RE2 优先；若 RE2 拒绝但 PCRE 通过，返回 "PCRE"；两者都失败返回 ("", err)。
func PatternFlavor(pattern string) (string, error) {
	if strings.TrimSpace(pattern) == "" {
		return "", nil
	}
	if _, err := regexp.Compile(pattern); err == nil {
		return "RE2", nil
	}
	if _, err := regexp2.Compile(pattern, 0); err == nil {
		return "PCRE", nil
	}
	return "", errors.New("pattern 正则无效：RE2 与 PCRE 语法均无法编译")
}

// Sub 应用一次 pattern/replace 到文件名（来自Trae）。
// pattern 为空时直接返回填充变量后的 replace。
func (m *MagicRename) Sub(pattern, replace, fileName string) string {
	if pattern == "" && replace == "" {
		return fileName
	}
	compiled := m.compile(pattern)
	replace = m.expandVariables(replace, fileName)
	if compiled != nil {
		return compiled.ReplaceAllString(fileName, replace)
	}
	return replace
}

// expandVariables 把 {TASKNAME} {SXX} {E} {DATE} 等变量替换为实际值（来自Trae）。
// Go RE2 不支持 lookbehind/lookahead，所有变量正则改用捕获组，取 group(1) 作为提取值。
func (m *MagicRename) expandVariables(replace, fileName string) string {
	for key, raw := range m.MagicVariable {
		if !strings.Contains(replace, key) {
			continue
		}
		switch key {
		case "{TASKNAME}":
			replace = strings.ReplaceAll(replace, key, m.taskName)
		case "{I}", "{II}":
			// {I+}/{II} 由 SetDirFileList/AssignIndex 单独处理；这里跳过（来自Trae）
			continue
		default:
			patterns, ok := raw.([]string)
			if !ok || len(patterns) == 0 {
				// 直接清空
				replace = strings.ReplaceAll(replace, key, "")
				continue
			}
			value := ""
			matched := false
			for _, p := range patterns {
				cp := m.compile(p)
				if cp == nil {
					continue
				}
				// 优先取捕获组 1；无捕获组时取完整匹配（来自Trae）
				if mm := cp.FindStringSubmatch(fileName); len(mm) > 0 {
					if len(mm) > 1 && mm[1] != "" {
						value = mm[1]
					} else {
						value = mm[0]
					}
					if key == "{DATE}" {
						value = normalizeDate(value)
					}
					if key == "{SXX}" {
						value = strings.ToUpper(value)
					}
					matched = true
					break
				}
			}
			if !matched {
				if key == "{SXX}" {
					value = "S01"
				}
			}
			replace = strings.ReplaceAll(replace, key, value)
		}
	}
	return replace
}

// ExtractEpisode 从文件名中提取集数编号，用于 {I+} 序号排序（来自Trae）
func (m *MagicRename) ExtractEpisode(fileName string) int {
	raw, ok := m.MagicVariable["{E}"]
	if !ok {
		return 0
	}
	patterns, ok := raw.([]string)
	if !ok {
		return 0
	}
	for _, p := range patterns {
		cp := m.compile(p)
		if cp == nil {
			continue
		}
		if mm := cp.FindStringSubmatch(fileName); len(mm) > 1 {
			if n, err := strconv.Atoi(mm[1]); err == nil {
				return n
			}
		}
	}
	return 0
}

// normalizeDate 把日期补全年份前缀（来自Trae，与 CASX 一致）
func normalizeDate(value string) string {
	digits := ""
	for _, c := range value {
		if c >= '0' && c <= '9' {
			digits += string(c)
		}
	}
	yearStr := strconv.Itoa(time.Now().Year())
	// CASX 用 max(0, 8-len(digits)) 截取年份前缀，但年份只有 4 位（来自Trae）
	prefixLen := 8 - len(digits)
	if prefixLen < 0 {
		prefixLen = 0
	}
	if prefixLen > len(yearStr) {
		prefixLen = len(yearStr)
	}
	return yearStr[:prefixLen] + digits
}

// IsExists 检查 filename 是否已存在（来自Trae）。
// ignoreExt=true 时按无扩展名比较。
// filename 中含 {I+} 时按正则匹配。
func (m *MagicRename) IsExists(filename string, list []string, ignoreExt bool) string {
	target := filename
	if ignoreExt {
		target = strings.TrimSuffix(target, filepath.Ext(target))
	}
	iPattern := m.compile(`\{I+\}`)
	if iPattern != nil && iPattern.MatchString(target) {
		// 把 {I+} 替换为对应长度的 \d 模式
		match := iPattern.FindString(target)
		digitPattern := strings.Repeat(`\d`, strings.Count(match, "I"))
		pattern := regexp.MustCompile("^" + strings.Replace(regexp.QuoteMeta(target), regexp.QuoteMeta(match), digitPattern, 1) + "$")
		for _, fn := range list {
			candidate := fn
			if ignoreExt {
				candidate = strings.TrimSuffix(candidate, filepath.Ext(candidate))
			}
			if pattern.MatchString(candidate) {
				return fn
			}
		}
		return ""
	}
	for _, fn := range list {
		candidate := fn
		if ignoreExt {
			candidate = strings.TrimSuffix(candidate, filepath.Ext(candidate))
		}
		if candidate == target {
			return fn
		}
	}
	return ""
}

// SetDirFileList 根据目标目录已存在文件推断 {I+} 序号起点（来自Trae）。
// fileNames 是目标目录已存在文件名（不含目录）。
// replace 是用户配置的命名模板（含 {I+}）。
// startIndex 是用户期望的最小序号。
func (m *MagicRename) SetDirFileList(fileNames []string, replace string, startIndex int) {
	m.dirIndex = map[int]string{}
	m.lastMagicI = startIndex - 1
	if len(fileNames) == 0 {
		return
	}

	iPattern := m.compile(`\{I+\}`)
	if iPattern == nil || !iPattern.MatchString(replace) {
		return
	}
	match := iPattern.FindString(replace)
	digitWidth := strings.Count(match, "I")
	digitPattern := strings.Repeat(`\d`, digitWidth)

	// 把模板中的 {I+} 替换为占位符，其它变量也替换为占位符，构造识别正则
	pattern := replace
	pattern = strings.Replace(pattern, match, "🔢", 1)
	for key := range m.MagicVariable {
		pattern = strings.ReplaceAll(pattern, key, "🔣")
	}
	// 替换反向引用 \1 \2 等
	pattern = regexp.MustCompile(`\\[0-9]+`).ReplaceAllString(pattern, "🔣")
	// 转义，再把占位符还原为模式
	escaped := regexp.QuoteMeta(pattern)
	escaped = strings.ReplaceAll(escaped, "🔣", ".*?")
	escaped = strings.ReplaceAll(escaped, "🔢", "("+digitPattern+")")
	compiled := regexp.MustCompile("^" + escaped + "$")

	// 排序后取最大序号
	sorted := append([]string(nil), fileNames...)
	sort.Strings(sorted)
	lastIdx := 0
	for _, fn := range sorted {
		if mm := compiled.FindStringSubmatch(fn); len(mm) > 1 {
			if n, err := strconv.Atoi(mm[1]); err == nil {
				m.dirIndex[n] = fn
				if n > lastIdx {
					lastIdx = n
				}
			}
		}
	}
	m.lastMagicI = lastIdx
}

// LastMagicI 返回 SetDirFileList 推断出的最大序号（来自Trae）
func (m *MagicRename) LastMagicI() int { return m.lastMagicI }

// DirIndex 返回当前序号映射（来自Trae）
func (m *MagicRename) DirIndex() map[int]string { return m.dirIndex }

// AssignIndex 给候选文件分配一个不冲突的序号（来自Trae）。
// 返回填充后的文件名。
func (m *MagicRename) AssignIndex(target string, width int) (string, int) {
	iPattern := m.compile(`\{I+\}`)
	if iPattern == nil || !iPattern.MatchString(target) {
		return target, 0
	}
	idx := m.lastMagicI + 1
	for {
		if _, ok := m.dirIndex[idx]; !ok {
			break
		}
		idx++
	}
	m.dirIndex[idx] = target
	m.lastMagicI = idx
	match := iPattern.FindString(target)
	w := strings.Count(match, "I")
	if width > w {
		w = width
	}
	replacement := strconv.Itoa(idx)
	for len(replacement) < w {
		replacement = "0" + replacement
	}
	return iPattern.ReplaceAllString(target, replacement), idx
}

// defaultMagicRegex 默认魔法正则（来自Trae）。
// Go RE2 不支持 lookbehind/lookahead，$BLACK_WORD/$SHOW_MAGIC/$SHOW_PRO 的
// 负向先行断言改为在 PassesBlacklist 中用 strings.Contains 预检。
func defaultMagicRegex() map[string]MagicRegexEntry {
	return map[string]MagicRegexEntry{
		"$TV_REGEX": {
			// 移除 (?!\d) 负向先行断言（RE2 不支持）（来自Trae）
			Pattern: `.*?([Ss]\d{1,2})?(?:[第EePpXx\.\-\_\( ]{1,2}|^)(\d{1,3}).*?\.(mp4|mkv)`,
			Replace: `${1}E${2}.${3}`,
		},
		"$BLACK_WORD": {
			// 黑名单关键词由 PassesBlacklist 单独检查（来自Trae）
			Pattern: `.*`,
			Replace: "",
		},
		"$TV_MAGIC": {
			Pattern: `.*\.(mp4|mkv|mov|m4v|avi|mpeg|ts)$`,
			Replace: `{TASKNAME}.{SXX}E{E}.{EXT}`,
		},
		"$SHOW_MAGIC": {
			// 黑名单关键词由 PassesBlacklist 单独检查（来自Trae）
			Pattern: `.*?第\d+期.*`,
			Replace: `{TASKNAME}.{SXX}E{II}.第{E}期{PART}.{EXT}`,
		},
		"$SHOW_PRO": {
			Pattern: `.*?第\d+期.*`,
			Replace: `{II}.{TASKNAME}.{DATE}.第{E}期{PART}.{EXT}`,
		},
	}
}

// blackKeywords 通用黑名单关键词（来自Trae，与 CASX $BLACK_WORD 一致）
var blackKeywords = []string{"纯享", "加更", "超前企划", "训练室", "蒸蒸日上"}

// showBlackKeywords 综艺黑名单关键词（来自Trae，与 CASX $SHOW_MAGIC/$SHOW_PRO 一致）
var showBlackKeywords = []string{"纯享", "加更", "抢先", "预告"}

// PassesBlacklist 检查文件名是否通过黑名单过滤（来自Trae）。
// patternName 为原始命名正则名（$BLACK_WORD / $SHOW_MAGIC / $SHOW_PRO）。
func (m *MagicRename) PassesBlacklist(patternName, fileName string) bool {
	switch patternName {
	case "$BLACK_WORD":
		for _, kw := range blackKeywords {
			if strings.Contains(fileName, kw) {
				return false
			}
		}
	case "$SHOW_MAGIC", "$SHOW_PRO":
		for _, kw := range showBlackKeywords {
			if strings.Contains(fileName, kw) {
				return false
			}
		}
	}
	return true
}

// defaultMagicVariable 默认变量（来自Trae）。
// Go RE2 不支持 lookbehind/lookahead，所有模式改用捕获组，取 group(1) 作为提取值。
func defaultMagicVariable() map[string]any {
	return map[string]any{
		"{TASKNAME}": "",
		"{EXT}":      []string{`\.(\w+)$`},
		"{CHINESE}":  []string{`([\p{Han}]{2,})`},
		"{DATE}": []string{
			`((?:18|19|20)?\d{2}[.\-/年]\d{1,2}[.\-/月]\d{1,2})`,
			`\b([12]\d{3}[01]?\d[0123]?\d)\b`,
			`\b([01]?\d[.\-/月][0123]?\d)\b`,
		},
		"{YEAR}": []string{`\b((?:18|19|20)\d{2})\b`},
		"{S}":    []string{`[Ss](\d{1,2})[EeXx]`, `[Ss](\d{1,2})\b`},
		"{SXX}":  []string{`([Ss]\d{1,2})[EeXx]`, `([Ss]\d{1,2})\b`},
		"{E}": []string{
			`[Ss]\d{1,2}[Ee](\d{1,3})`,
			`[Ee](\d{1,3})`,
			`[Ee][Pp](\d{1,3})`,
			`第(\d{1,3})[集期话部篇]`,
			`(\d{1,3})[集期话部篇]`,
			`[._](\d{1,3})[._]`,
			`^(\d{1,3})\.\w+`,
		},
		"{PART}": []string{`[集期话部篇第]([上中下一二三四五六七八九十])`, `([上中下一二三四五六七八九十])`},
		"{VER}":  []string{`([\p{Han}]+版)`},
		"{I}":    "",
		"{II}":   "",
	}
}

// PriorityKeywords 与 CASX priority_list 一致（来自Trae）
var PriorityKeywords = []string{
	"更新", "超前点映", "抢先看", "加更", "精编版", "纯享",
	"未播", "彩蛋", "花絮", "番外", "幕后", "特辑", "预告",
	"合集", "特别篇", "先导片", "大结局", "结局", "完结",
	"上", "中", "下",
	"一", "二", "三", "四", "五", "六", "七", "八", "九", "十",
	"百", "千", "万",
}

// CustomSortKey 把 PriorityKeywords 替换为下标占位符，使排序与 CASX 一致（来自Trae）
func CustomSortKey(name string) string {
	for i, kw := range PriorityKeywords {
		if strings.Contains(name, kw) {
			name = strings.ReplaceAll(name, kw, "_"+strconv.Itoa(i)+"_")
		}
	}
	return name
}

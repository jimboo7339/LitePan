package api

// 追剧命名正则规则维护 + 分享链接预览的 HTTP 处理器（来自Trae）。
// 复用 ensureServiceReady / decodeJSON / writeOK / writeErr 等通用助手，
// 与 drama.go 的风格保持一致。

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"litepan/internal/domain"
	"litepan/internal/dramatransfer"
)

// magicRegexRuleDTO 规则响应结构（来自Trae）
type magicRegexRuleDTO struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	Enabled        bool   `json:"enabled"`
	BuiltIn        bool   `json:"built_in"`
	Overridden     bool   `json:"overridden"`
	Pattern        string `json:"pattern"`
	Replace        string `json:"replace"`
	DefaultPattern string `json:"default_pattern,omitempty"`
	DefaultReplace string `json:"default_replace,omitempty"`
}

// magicRegexRuleInput 规则写入入参（来自Trae）
type magicRegexRuleInput struct {
	Label   string `json:"label"`
	Pattern string `json:"pattern"`
	Replace string `json:"replace"`
	Enabled *bool  `json:"enabled"`
}

// toMagicRegexRuleDTO 转换规则展示对象（来自Trae）
func toMagicRegexRuleDTO(r dramatransfer.MagicRegexRuleOut) magicRegexRuleDTO {
	return magicRegexRuleDTO{
		Key:            r.Key,
		Label:          r.Label,
		Enabled:        r.Enabled,
		BuiltIn:        r.BuiltIn,
		Overridden:     r.Overridden,
		Pattern:        r.Pattern,
		Replace:        r.Replace,
		DefaultPattern: r.DefaultPattern,
		DefaultReplace: r.DefaultReplace,
	}
}

func (h *Handler) listMagicRegexRules(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.drama != nil) {
		return
	}
	rules, err := h.drama.ListRules(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]magicRegexRuleDTO, 0, len(rules))
	for _, rule := range rules {
		out = append(out, toMagicRegexRuleDTO(rule))
	}
	writeOK(w, map[string]any{"rules": out})
}

func (h *Handler) upsertMagicRegexRule(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.drama != nil) {
		return
	}
	key := chi.URLParam(r, "key")
	var in magicRegexRuleInput
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	rule := &domain.MagicRegexRule{
		Key:     key,
		Label:   in.Label,
		Pattern: in.Pattern,
		Replace: in.Replace,
		Enabled: true, // 默认启用（来自Trae）
	}
	if in.Enabled != nil {
		rule.Enabled = *in.Enabled
	}
	if err := h.drama.UpsertRule(r.Context(), rule); err != nil {
		writeErr(w, err)
		return
	}
	// 返回更新后的规则列表，便于前端一次刷新（来自Trae）
	rules, err := h.drama.ListRules(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]magicRegexRuleDTO, 0, len(rules))
	for _, rule := range rules {
		out = append(out, toMagicRegexRuleDTO(rule))
	}
	writeOK(w, map[string]any{"rules": out})
}

func (h *Handler) deleteMagicRegexRule(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.drama != nil) {
		return
	}
	key := chi.URLParam(r, "key")
	if err := h.drama.DeleteRule(r.Context(), key); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"key": key})
}

// === 分享链接预览（来自Trae）===

// dramaPreviewInput 预览入参（来自Trae）
type dramaPreviewInput struct {
	AccountID       int64  `json:"account_id"`
	ShareURL        string `json:"share_url"`
	PdirFID         string `json:"pdir_fid"`
	MaxItems        int    `json:"max_items"`
	TaskName        string `json:"task_name"`
	Pattern         string `json:"pattern"`
	Replace         string `json:"replace"`
	SortIndex       int    `json:"sort_index"`
	SavePath        string `json:"save_path"`
	IgnoreExtension bool   `json:"ignore_extension"`
	UpdateSubdir    string `json:"update_subdir"`
	StartFID        string `json:"start_fid"`
}

// dramaPreviewItemDTO 预览条目（来自Trae）
type dramaPreviewItemDTO struct {
	FID           string `json:"fid"`
	FIDToken      string `json:"fid_token,omitempty"`
	Name          string `json:"name"`
	NameRe        string `json:"name_re,omitempty"`
	IsDir         bool   `json:"is_dir"`
	UpdatedAt     int64  `json:"updated_at"`
	Size          int64  `json:"size"`
	ChildrenCount int    `json:"children_count"`
	NameSaved     string `json:"name_saved,omitempty"`
}

// dramaPreviewResultDTO 预览结果（来自Trae）
type dramaPreviewResultDTO struct {
	DriveType string                `json:"drive_type"`
	PwdID     string                `json:"pwd_id"`
	PdirFID   string                `json:"pdir_fid"`
	Items     []dramaPreviewItemDTO `json:"items"`
}

func (h *Handler) previewDramaShare(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.drama != nil) {
		return
	}
	var in dramaPreviewInput
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if in.MaxItems <= 0 {
		in.MaxItems = 200
	}
	result, err := h.drama.PreviewShare(r.Context(), dramatransfer.PreviewShareInput{
		AccountID:       in.AccountID,
		ShareURL:        in.ShareURL,
		PdirFID:         in.PdirFID,
		MaxItems:        in.MaxItems,
		TaskName:        in.TaskName,
		Pattern:         in.Pattern,
		Replace:         in.Replace,
		SortIndex:       in.SortIndex,
		SavePath:        in.SavePath,
		IgnoreExtension: in.IgnoreExtension,
		UpdateSubdir:    in.UpdateSubdir,
		StartFID:        in.StartFID,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	out := dramaPreviewResultDTO{
		DriveType: result.DriveType,
		PwdID:     result.PwdID,
		PdirFID:   result.PdirFID,
		Items:     make([]dramaPreviewItemDTO, 0, len(result.Items)),
	}
	for _, it := range result.Items {
		out.Items = append(out.Items, dramaPreviewItemDTO{
			FID:           it.FID,
			FIDToken:      it.FIDToken,
			Name:          it.Name,
			NameRe:        it.NameRe,
			IsDir:         it.IsDir,
			UpdatedAt:     it.UpdatedAt,
			Size:          it.Size,
			ChildrenCount: it.ChildrenCount,
			NameSaved:     it.NameSaved,
		})
	}
	writeOK(w, out)
}

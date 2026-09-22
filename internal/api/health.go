package api

import (
	"net/http"

	"litepan/internal/buildinfo"
)

// health 返回运行状态与构建信息（来自Trae）。
// build 字段包含 VCS 提交短哈希等，用户可直接对比以确认运行的是否为最新构建。
func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeOK(w, map[string]any{
		"status": "ok",
		"boot_id": h.bootID,
		"build":  buildinfo.Build(),
	})
}

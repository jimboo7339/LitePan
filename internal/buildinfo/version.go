package buildinfo

import "runtime/debug"

// Version 是 main 分支的基准版本（来自Trae）。
// feature 分支通过 git tag（如 v0.5.6.5）标识增量，不再改动此常量。
var Version = "v0.5.6-Beta"

// Build 返回构建版本 + VCS 提交短哈希 + 是否脏构建，供 /health 展示（来自Trae）。
// 通过 Go runtime/debug.ReadBuildInfo 自动携带构建时的 VCS 信息，
// 用户无需手工核对源码即可确认自己运行的二进制是否包含最新提交。
func Build() BuildInfo {
	info := BuildInfo{Version: Version}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				info.Commit = s.Value
			case "vcs.time":
				info.BuiltAt = s.Value
			case "vcs.modified":
				info.Dirty = s.Value == "true"
			}
		}
	}
	if len(info.Commit) >= 7 {
		info.ShortCommit = info.Commit[:7]
	}
	return info
}

// BuildInfo /health 接口暴露的构建信息（来自Trae）。
type BuildInfo struct {
	Version     string `json:"version"`
	Commit      string `json:"commit,omitempty"`
	ShortCommit string `json:"short_commit,omitempty"`
	BuiltAt     string `json:"built_at,omitempty"`
	Dirty       bool   `json:"dirty,omitempty"`
}

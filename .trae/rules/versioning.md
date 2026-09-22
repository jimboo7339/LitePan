# 分支版本与 Tag 命名规则

> 该规则适用于所有非 main 的开发分支（如 feature/litepan-plus），来源：Trae。

## 规则

1. **基准版本** = main 分支当前版本号，取自 `internal/buildinfo/version.go` 中 `Version` 常量，去掉所有后缀（如 `-Beta`、`-RC` 等）。
   - 例如 main 是 `v0.5.6-Beta` → 基准版本为 `0.5.6`。
2. **分支 Tag 命名** = `v{基准版本}.{N}`，其中 `N` 从 1 开始，随本分支打 tag 次数递增。
   - 例如 main 是 `v0.5.6-Beta`，分支首次打 tag = `v0.5.6.1`，第二次 = `v0.5.6.2`，依次类推。
3. **当 main 版本变化时**，分支自动切换到新基准：
   - 若 main 从 `0.5.6` 升到 `0.5.7`，本分支的下一次 tag 改为 `v0.5.7.1`（重新从 1 开始）。
4. **旧的历史 tag**（如 `v0.5.4-x.14` 之类 `x.N` 格式）保留但不再使用；规则落地后新 tag 一律走上述命名。

## 操作流程（打 tag 前）

```bash
# 1. 同步 main 与所有 tag
git fetch origin main --tags

# 2. 读取 main 的基准版本
git show origin/main:internal/buildinfo/version.go
# 从中提取 vX.Y.Z 中的 X.Y.Z 作为基准

# 3. 找到本分支在 v{基准版本}.* 下已有的最大 N
git tag --list "v{基准版本}.*" --sort=-v:refname | head -1

# 4. 生成新 tag：v{基准版本}.{N+1}（若无则从 .1 开始）

# 5. 打 tag、push 分支、push tag
git tag -a v{基准版本}.{N+1} -m "..."
git push origin HEAD
git push origin v{基准版本}.{N+1}
```

## 判定示例

| main Version | 分支已有 tag | 下一个 tag |
|---|---|---|
| `v0.5.6-Beta` | 无 `v0.5.6.*` | `v0.5.6.1` |
| `v0.5.6-Beta` | `v0.5.6.3` | `v0.5.6.4` |
| `v0.5.7-RC1` | `v0.5.7.2` | `v0.5.7.3` |
| `v0.5.8` | 已有 `v0.5.6.5`（旧基准残留） | `v0.5.8.1` |

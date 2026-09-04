package dramatransfer

// 本文件实现追剧转存执行器，移植自 CASX drama_executor.py（来自Trae）。
// 适配 LitePan 的 ShareTransferer 接口，编排分享链接解析→目录准备→
// 转存规划→批量转存→重命名→子目录同步的完整流程。

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

// ErrSkipTask 表示任务被跳过（不在运行星期、超过截止日期等）（来自Trae）
var ErrSkipTask = fmt.Errorf("drama: skip task")

// iPlusRe 匹配 {I+}/{II} 等序号占位符（来自Trae）
var iPlusRe = regexp.MustCompile(`\{I+\}`)

// dramaPlanItem 转存计划单项（来自Trae）
type dramaPlanItem struct {
	fid        string
	fidToken   string
	originName string
	targetName string
	isDir      bool
	size       int64
	updatedAt  int64
}

// treeBuilder 构建树状摘要文本（来自Trae）
type treeBuilder struct {
	lines []string
}

func newTreeBuilder(root string) *treeBuilder {
	return &treeBuilder{lines: []string{root}}
}

func (t *treeBuilder) add(depth int, text string) {
	indent := strings.Repeat("  ", depth)
	t.lines = append(t.lines, indent+text)
}

func (t *treeBuilder) String() string {
	if len(t.lines) <= 1 {
		t.lines = append(t.lines, "  无可转存文件")
	}
	return strings.Join(t.lines, "\n")
}

// DramaExecutor 追剧转存执行器，编排单次转存的完整流程（来自Trae）。
type DramaExecutor struct {
	drv            driver.Driver
	st             driver.ShareTransferer
	task           *domain.DramaTask
	mr             *MagicRename
	log            strings.Builder
	transferCount  int
	transferNames  []string // 本次转存+重命名后的目标文件名列表，供通知格式化使用（来自Trae）
}

// NewDramaExecutor 构造执行器；drv 必须实现 ShareTransferer（来自Trae）。
// overrides 为启用的命名正则覆盖（内置/自定义），可为 nil。
func NewDramaExecutor(drv driver.Driver, task *domain.DramaTask, overrides map[string]MagicRegexEntry) (*DramaExecutor, error) {
	st, ok := drv.(driver.ShareTransferer)
	if !ok {
		return nil, domain.Errorf(domain.CodeNotImplement, "驱动 %s 不支持分享转存", drv.Config().Name)
	}
	mr := NewMagicRename()
	mr.WithOverrides(overrides, nil)
	mr.SetTaskName(task.TaskName)
	return &DramaExecutor{
		drv:  drv,
		st:   st,
		task: task,
		mr:   mr,
	}, nil
}

// Log 返回执行日志（来自Trae）
func (e *DramaExecutor) Log() string {
	return e.log.String()
}

// TransferCount 返回转存文件数（来自Trae）
func (e *DramaExecutor) TransferCount() int {
	return e.transferCount
}

// TransferredNames 返回本次转存+重命名后的目标文件名列表，供通知格式化使用（来自Trae）。
// 按转存顺序排列，与 treeSummary 中的 "origin -> target" 行一一对应。
func (e *DramaExecutor) TransferredNames() []string {
	return e.transferNames
}

func isPermissionDenied(err error) bool {
	ae, ok := domain.AsAppError(err)
	return ok && ae.Code == domain.CodePermissionDenied
}

func (e *DramaExecutor) line(text string) {
	e.log.WriteString(text)
	e.log.WriteByte('\n')
}

func (e *DramaExecutor) section(title string) {
	e.log.WriteString("\n=== ")
	e.log.WriteString(title)
	e.log.WriteString(" ===\n")
}

// Execute 运行完整转存流程，返回树状摘要（来自Trae）
func (e *DramaExecutor) Execute(ctx context.Context) (string, error) {
	// 1. 解析分享链接（来自Trae）
	e.section("解析分享链接")
	pwdID, passcode, pdirFID, err := e.st.ExtractShareURL(e.task.ShareURL)
	if err != nil {
		return "", err
	}
	e.line("pwd_id: " + pwdID)
	e.line("pdir_fid: " + pdirFID)

	// 2. 获取分享 token（来自Trae）
	e.section("获取分享 token")
	stoken, err := e.st.GetShareToken(ctx, pwdID, passcode)
	if err != nil {
		return "", err
	}
	e.line("OK: stoken 获取成功")

	// 3. 准备目标目录（来自Trae）
	e.section("准备保存目录")
	savePath := strings.TrimRight(e.task.SavePath, "/")
	e.line("保存路径: " + savePath)
	destFID, err := e.ensureDestDir(ctx, savePath)
	if err != nil {
		return "", err
	}
	e.line("目标目录 fid: " + destFID)

	// 4. 列出目标目录已有文件（来自Trae）
	destItems, err := e.drv.ListFiles(ctx, destFID)
	if err != nil {
		return "", domain.Wrap(domain.CodeDriverError, err)
	}
	destFileNames := make([]string, 0, len(destItems))
	destDirMap := make(map[string]string) // name -> fid
	destNameSet := make(map[string]struct{})
	for _, it := range destItems {
		if it.IsDir {
			if it.Name != "" && it.ID != "" {
				destDirMap[it.Name] = it.ID
			}
			continue
		}
		if it.Name != "" {
			destFileNames = append(destFileNames, it.Name)
			destNameSet[normalizeName(it.Name, e.task.IgnoreExtension)] = struct{}{}
		}
	}

	// 5. 读取分享列表（来自Trae）
	e.section("读取分享列表")
	shareItems, err := e.st.ListShareItems(ctx, pwdID, stoken, pdirFID)
	if err != nil {
		return "", err
	}
	e.line(fmt.Sprintf("分享列表项数: %d", len(shareItems)))

	tree := newTreeBuilder(e.task.TaskName)

	// 6. 规划转存（仅根目录文件）（来自Trae）
	e.section("生成转存计划")
	var rootFiles []driver.ShareItem
	for _, it := range shareItems {
		if !it.IsDir {
			rootFiles = append(rootFiles, it)
		}
	}
	plan := e.planTransfer(rootFiles, destFileNames)
	e.line(fmt.Sprintf("待转存文件数: %d", len(plan)))

	// 7. 执行转存（来自Trae）
	if len(plan) > 0 {
		e.section("执行转存")
		savedFIDs, err := e.saveFiles(ctx, pwdID, stoken, destFID, plan)
		if err != nil {
			return tree.String(), err
		}
		// 8. 重命名（来自Trae）
		e.section("重命名")
		e.renameFiles(ctx, savedFIDs, plan, destFID)
		limit := len(plan)
		if len(savedFIDs) < limit {
			limit = len(savedFIDs)
		}
		for i := 0; i < limit; i++ {
			tree.add(1, fmt.Sprintf("%d. %s -> %s", i+1, plan[i].originName, plan[i].targetName))
			e.transferNames = append(e.transferNames, plan[i].targetName)
		}
	}

	// 9. 子目录同步（来自Trae）
	if strings.TrimSpace(e.task.UpdateSubdir) != "" {
		e.section("子目录转存")
		e.syncSubdirs(ctx, pwdID, stoken, shareItems, destFID, destDirMap, tree)
	}

	return tree.String(), nil
}

// ensureDestDir 确保目标目录存在，返回其 fid（来自Trae）。
// 优先用 ResolvePathToFID 解析；若不存在则逐级 ListFiles + CreateFolder 创建。
func (e *DramaExecutor) ensureDestDir(ctx context.Context, savePath string) (string, error) {
	savePath = strings.TrimSpace(savePath)
	normalized := regexp.MustCompile(`/+`).ReplaceAllString("/"+savePath, "/")
	if normalized == "/" || normalized == "" {
		return "0", nil
	}

	// 尝试解析完整路径（来自Trae）
	pathFIDs, err := e.st.ResolvePathToFID(ctx, []string{normalized})
	if err == nil && len(pathFIDs) > 0 && strings.TrimSpace(pathFIDs[0].FID) != "" {
		return pathFIDs[0].FID, nil
	}

	// 逐级创建（来自Trae）
	parts := strings.Split(strings.Trim(normalized, "/"), "/")
	parentID := "0"
	fc, ok := e.drv.(driver.FolderCreator)
	if !ok {
		return "", domain.Errorf(domain.CodeNotImplement, "驱动不支持创建目录")
	}
	for _, part := range parts {
		if part == "" {
			continue
		}
		// 检查是否已存在
		items, err := e.drv.ListFiles(ctx, parentID)
		if err != nil {
			return "", domain.Wrap(domain.CodeDriverError, err)
		}
		found := ""
		for _, it := range items {
			if it.IsDir && it.Name == part {
				found = it.ID
				break
			}
		}
		if found != "" {
			parentID = found
			continue
		}
		// 创建目录
		item, err := fc.CreateFolder(ctx, parentID, part)
		if err != nil {
			return "", err
		}
		if item == nil || item.ID == "" {
			return "", domain.Errorf(domain.CodeDriverError, "创建目录失败: %s", part)
		}
		parentID = item.ID
	}
	return parentID, nil
}

// startFIDFilter 封装 startfid 增量过滤逻辑（来自Trae）。
// 若 startfid 对应文件有时间戳，则只保留比它新的；否则按时间降序保留 startfid 之前的 fid 集合。
type startFIDFilter struct {
	enabled bool
	startTS int64
	fidKeep map[string]struct{}
}

func newStartFIDFilter(shareItems []driver.ShareItem, startFID string) startFIDFilter {
	startFID = strings.TrimSpace(startFID)
	if startFID == "" {
		return startFIDFilter{}
	}
	f := startFIDFilter{enabled: true, fidKeep: map[string]struct{}{}}
	var startItem *driver.ShareItem
	for i := range shareItems {
		if strings.TrimSpace(shareItems[i].FID) == startFID {
			startItem = &shareItems[i]
			break
		}
	}
	if startItem == nil {
		return f
	}
	if startItem.UpdatedAt > 0 {
		f.startTS = startItem.UpdatedAt
		return f
	}
	// 无时间戳：按时间降序保留 startfid 之前的 fid（来自Trae）
	sorted := make([]driver.ShareItem, len(shareItems))
	copy(sorted, shareItems)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].UpdatedAt > sorted[j].UpdatedAt
	})
	for _, sf := range sorted {
		if sf.FID == startFID {
			break
		}
		if sf.FID != "" {
			f.fidKeep[sf.FID] = struct{}{}
		}
	}
	return f
}

func (f startFIDFilter) shouldKeep(fid string, updatedAt int64) bool {
	if !f.enabled {
		return true
	}
	if f.startTS > 0 {
		return updatedAt > f.startTS
	}
	_, ok := f.fidKeep[fid]
	return ok
}

// planTransfer 规划转存计划：过滤、重命名、去重、序号分配（来自Trae）
func (e *DramaExecutor) planTransfer(shareFiles []driver.ShareItem, destFileNames []string) []dramaPlanItem {
	pattern := e.task.Pattern
	replace := e.task.Replace
	ignoreExt := e.task.IgnoreExtension
	startIndex := e.task.SortIndex
	if startIndex <= 0 {
		startIndex = 1
	}

	// 展开命名正则（来自Trae）
	actualPattern, actualReplace := e.mr.MagicRegexConv(pattern, replace)

	var compiledSearch *regexp.Regexp
	if actualPattern != "" {
		compiledSearch = e.mr.compile(actualPattern)
	}

	// startfid 增量过滤（来自Trae）
	sff := newStartFIDFilter(shareFiles, e.task.StartFID)

	// 构建候选列表（来自Trae）
	type candidate struct {
		item       driver.ShareItem
		targetName string
	}
	var candidates []candidate

	for _, sf := range shareFiles {
		fid := strings.TrimSpace(sf.FID)
		originName := sf.FileName
		if fid == "" || originName == "" {
			continue
		}
		if !sff.shouldKeep(fid, sf.UpdatedAt) {
			continue
		}
		// pattern 过滤
		if compiledSearch != nil && !compiledSearch.MatchString(originName) {
			continue
		}
		// 黑名单过滤（来自Trae）
		if !e.mr.PassesBlacklist(pattern, originName) {
			continue
		}
		// 生成目标文件名
		targetName := originName
		if !sf.IsDir {
			targetName = e.mr.Sub(actualPattern, actualReplace, originName)
		}
		// 去重：目标名已存在则跳过（来自Trae）
		if existing := e.mr.IsExists(targetName, destFileNames, ignoreExt && !sf.IsDir); existing != "" {
			continue
		}
		candidates = append(candidates, candidate{item: sf, targetName: targetName})
	}

	if len(candidates) == 0 {
		return nil
	}

	// 按目标名去重：同 key 保留 size 最大 / updatedAt 最新（来自Trae）
	best := map[string]int{}
	for idx, c := range candidates {
		key := c.targetName
		if ignoreExt {
			key = strings.TrimSuffix(key, filepath.Ext(key))
		}
		prevIdx, ok := best[key]
		if !ok {
			best[key] = idx
			continue
		}
		prev := candidates[prevIdx]
		if c.item.Size > prev.item.Size ||
			(c.item.Size == prev.item.Size && c.item.UpdatedAt > prev.item.UpdatedAt) {
			best[key] = idx
		}
	}
	deduped := make([]candidate, 0, len(best))
	for _, idx := range best {
		deduped = append(deduped, candidates[idx])
	}

	// {I+} 序号分配：按集数排序后逐个分配序号（来自Trae）
	if iPlusRe.MatchString(actualReplace) {
		e.mr.SetDirFileList(destFileNames, actualReplace, startIndex)
		sort.Slice(deduped, func(i, j int) bool {
			ei := e.mr.ExtractEpisode(deduped[i].targetName)
			ej := e.mr.ExtractEpisode(deduped[j].targetName)
			if ei != ej {
				return ei < ej
			}
			return deduped[i].targetName < deduped[j].targetName
		})
		for i := range deduped {
			name, _ := e.mr.AssignIndex(deduped[i].targetName, 0)
			deduped[i].targetName = name
		}
	}

	// 构建计划（来自Trae）
	plan := make([]dramaPlanItem, 0, len(deduped))
	for _, c := range deduped {
		plan = append(plan, dramaPlanItem{
			fid:        c.item.FID,
			fidToken:   c.item.ShareFIDToken,
			originName: c.item.FileName,
			targetName: c.targetName,
			isDir:      c.item.IsDir,
			size:       c.item.Size,
			updatedAt:  c.item.UpdatedAt,
		})
	}
	return plan
}

// saveFiles 分批执行转存，返回所有转存后的 fid（来自Trae）
func (e *DramaExecutor) saveFiles(ctx context.Context, pwdID, stoken, destFID string, plan []dramaPlanItem) ([]string, error) {
	var allSavedFIDs []string
	const batchSize = 100
	for idx := 0; idx < len(plan); idx += batchSize {
		end := idx + batchSize
		if end > len(plan) {
			end = len(plan)
		}
		batch := plan[idx:end]

		fids := make([]string, 0, len(batch))
		fidTokens := make([]string, 0, len(batch))
		fileNames := make([]string, 0, len(batch))
		for _, p := range batch {
			fids = append(fids, p.fid)
			fidTokens = append(fidTokens, p.fidToken)
			fileNames = append(fileNames, p.originName)
		}

		req := driver.SaveShareReq{
			FIDs:      fids,
			FIDTokens: fidTokens,
			ToPdirFID: destFID,
			PwdID:     pwdID,
			Stoken:    stoken,
			FileNames: fileNames,
		}

		_, savedFIDs, err := e.st.SaveShareFiles(ctx, req)
		if err != nil {
			if isPermissionDenied(err) {
				e.line("INFO: 转存被拒绝，尝试刷新授权后重试一次")
				if ctx.Err() != nil {
					e.line(fmt.Sprintf("WARN: 上下文已取消，跳过重试: %v", ctx.Err()))
				} else if pingErr := e.drv.Ping(ctx); pingErr != nil {
					e.line(fmt.Sprintf("WARN: 刷新授权请求失败: %v", pingErr))
				} else {
					var retryTaskID string
					retryTaskID, savedFIDs, err = e.st.SaveShareFiles(ctx, req)
					_ = retryTaskID
					if err == nil {
						e.line("INFO: 重试转存成功")
					} else if isPermissionDenied(err) {
						e.line("ERROR: 重试后仍然权限不足，需要更新 Cookie")
					}
				}
			}
			if err != nil {
				return allSavedFIDs, err
			}
		}
		e.line(fmt.Sprintf("批次 %d-%d: 转存 %d 个文件", idx+1, end, len(savedFIDs)))
		allSavedFIDs = append(allSavedFIDs, savedFIDs...)
		e.transferCount += len(savedFIDs)
	}
	if len(allSavedFIDs) == 0 && len(plan) > 0 {
		e.line("警告: 转存完成但未返回 fid 列表，将跳过重命名")
	}
	return allSavedFIDs, nil
}

// renameFiles 按计划重命名转存后的文件（来自Trae）。
// 包含二次校验：列出目标目录确认改名是否生效，未生效则重试。
func (e *DramaExecutor) renameFiles(ctx context.Context, savedFIDs []string, plan []dramaPlanItem, destFID string) {
	renamer, ok := e.drv.(driver.Renamer)
	if !ok {
		e.line("驱动不支持重命名，跳过")
		return
	}

	limit := len(savedFIDs)
	if len(plan) < limit {
		limit = len(plan)
	}

	type attempt struct {
		fid, origin, target string
	}
	var attempted []attempt

	for i := 0; i < limit; i++ {
		fid := strings.TrimSpace(savedFIDs[i])
		if fid == "" {
			continue
		}
		origin := plan[i].originName
		target := plan[i].targetName
		if origin == "" || target == "" || origin == target {
			continue
		}
		e.line(fmt.Sprintf("重命名: %s -> %s", origin, target))
		if err := renamer.RenameFile(ctx, fid, target); err != nil {
			e.line(fmt.Sprintf("FAIL: 重命名失败 %s err=%v", origin, err))
		}
		attempted = append(attempted, attempt{fid: fid, origin: origin, target: target})
	}

	if len(attempted) == 0 {
		return
	}

	// 二次校验：列出目录检查是否真的改名成功（来自Trae）
	items, err := e.drv.ListFiles(ctx, destFID)
	if err != nil {
		e.line("INFO: 重命名校验失败，无法读取目标目录")
		return
	}
	fidName := make(map[string]string, len(items))
	for _, it := range items {
		if !it.IsDir && it.ID != "" {
			fidName[it.ID] = it.Name
		}
	}

	retryOK, retryFail := 0, 0
	for _, a := range attempted {
		if fidName[a.fid] == a.target {
			continue
		}
		if err := renamer.RenameFile(ctx, a.fid, a.target); err != nil {
			retryFail++
		} else {
			retryOK++
		}
	}
	e.line(fmt.Sprintf("重命名摘要: total=%d retry_ok=%d retry_failed=%d", len(attempted), retryOK, retryFail))
}

// syncSubdirs 同步子目录：匹配 update_subdir 正则的分享子目录会被转存/同步（来自Trae）
func (e *DramaExecutor) syncSubdirs(ctx context.Context, pwdID, stoken string, shareItems []driver.ShareItem, destFID string, destDirMap map[string]string, tree *treeBuilder) {
	compiledSubdir := e.mr.compile(e.task.UpdateSubdir)
	if compiledSubdir == nil {
		e.line("WARN: update_subdir 正则编译失败，跳过子目录同步")
		return
	}
	mode := strings.TrimSpace(e.task.UpdateSubdirResaveMode)
	ignoreExt := e.task.IgnoreExtension
	sff := newStartFIDFilter(shareItems, e.task.StartFID)
	deleter, _ := e.drv.(driver.Deleter)

	// 按 name 排序的子目录列表（来自Trae）
	var dirs []driver.ShareItem
	for _, it := range shareItems {
		if it.IsDir {
			dirs = append(dirs, it)
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].FileName < dirs[j].FileName
	})

	for _, dir := range dirs {
		name := dir.FileName
		fid := dir.FID
		if name == "" || fid == "" {
			continue
		}
		if !sff.shouldKeep(fid, dir.UpdatedAt) {
			continue
		}
		if !compiledSubdir.MatchString(name) {
			continue
		}

		existingDestFID := destDirMap[name]

		// delete_then_resave 模式：为避免"先删后转存失败"导致数据丢失，
		// 改为先把分享目录转存到临时目录 __litepan_tmp_<ts>_<name>，成功后再删旧目录、重命名新目录（来自Trae）。
		if mode == "delete_then_resave" && existingDestFID != "" && deleter != nil {
			tmpName := fmt.Sprintf("__litepan_tmp_%d_%s", time.Now().UnixNano(), name)
			_, tmpSavedFIDs, saveErr := e.st.SaveShareFiles(ctx, driver.SaveShareReq{
				FIDs:      []string{fid},
				FIDTokens: []string{dir.ShareFIDToken},
				ToPdirFID: destFID,
				PwdID:     pwdID,
				Stoken:    stoken,
				FileNames: []string{tmpName},
			})
			if saveErr != nil {
				// 转存失败，保留原目录不动（来自Trae）
				e.line(fmt.Sprintf("FAIL: 转存目录 %s 失败（保留原目录不动）: %v", name, saveErr))
				continue
			}
			// 从返回的 savedFIDs 取新目录 fid（SaveShareFiles 对目录返回单个 newFID）（来自Trae）
			tmpDirFID := ""
			for _, v := range tmpSavedFIDs {
				if v = strings.TrimSpace(v); v != "" {
					tmpDirFID = v
					break
				}
			}
			if tmpDirFID == "" {
				// 未拿到新目录 fid，无法安全替换，跳过（来自Trae）
				e.line(fmt.Sprintf("WARN: 转存目录 %s 成功但未返回 fid，跳过替换（保留原目录）", name))
				continue
			}
			if err := deleter.DeleteFiles(ctx, []string{existingDestFID}); err != nil {
				// 删除旧失败但新的已转存，记录警告继续尝试重命名（可能重命名也因同名失败）（来自Trae）
				e.line(fmt.Sprintf("WARN: 删除旧目录 %s 失败: %v", name, err))
			}
			renamer2, okRenamer := e.drv.(driver.Renamer)
			if okRenamer {
				if err := renamer2.RenameFile(ctx, tmpDirFID, name); err != nil {
					e.line(fmt.Sprintf("WARN: 重命名临时目录 %s -> %s 失败: %v", tmpName, name, err))
				}
			}
			e.transferCount++
			tree.add(1, "📁"+name)
			continue
		}

		if existingDestFID != "" {
			// 目录已存在：递归同步内容（来自Trae）
			tree.add(1, "📁"+name+"（检查）")
			e.syncShareDir(ctx, pwdID, stoken, fid, existingDestFID, ignoreExt, tree, 2)
		} else {
			// 目录不存在：直接转存整个分享目录（来自Trae）
			_, _, err := e.st.SaveShareFiles(ctx, driver.SaveShareReq{
				FIDs:      []string{fid},
				FIDTokens: []string{dir.ShareFIDToken},
				ToPdirFID: destFID,
				PwdID:     pwdID,
				Stoken:    stoken,
				FileNames: []string{name},
			})
			if err != nil {
				e.line(fmt.Sprintf("FAIL: 转存目录 %s 失败: %v", name, err))
			} else {
				e.transferCount++
				tree.add(1, "📁"+name)
			}
		}
	}
}

// syncShareDir 递归同步分享子目录到目标目录（来自Trae）。
// 仅转存目标目录中不存在的文件；对已存在的子目录递归处理，深度不超过 3。
func (e *DramaExecutor) syncShareDir(ctx context.Context, pwdID, stoken, shareDirFID, destDirFID string, ignoreExt bool, tree *treeBuilder, depth int) {
	if depth > 3 {
		return
	}

	// 拉取分享子目录内容（来自Trae）
	shareItems, err := e.st.ListShareItems(ctx, pwdID, stoken, shareDirFID)
	if err != nil {
		e.line(fmt.Sprintf("FAIL: 读取分享子目录失败: %v", err))
		return
	}

	// 拉取目标目录内容（来自Trae）
	destItems, err := e.drv.ListFiles(ctx, destDirFID)
	if err != nil {
		e.line(fmt.Sprintf("FAIL: 读取目标子目录失败: %v", err))
		return
	}
	destDirMap := make(map[string]string)
	destNameSet := make(map[string]struct{})
	for _, it := range destItems {
		if it.IsDir {
			if it.Name != "" && it.ID != "" {
				destDirMap[it.Name] = it.ID
			}
			continue
		}
		if it.Name != "" {
			destNameSet[normalizeName(it.Name, ignoreExt)] = struct{}{}
		}
	}

	// 按 name 排序（来自Trae）
	sortedItems := make([]driver.ShareItem, len(shareItems))
	copy(sortedItems, shareItems)
	sort.Slice(sortedItems, func(i, j int) bool {
		return sortedItems[i].FileName < sortedItems[j].FileName
	})

	for _, sf := range sortedItems {
		name := sf.FileName
		fid := sf.FID
		if name == "" || fid == "" {
			continue
		}

		if sf.IsDir {
			existingDestFID := destDirMap[name]
			if existingDestFID != "" {
				tree.add(depth, "📁"+name+"（检查）")
				e.syncShareDir(ctx, pwdID, stoken, fid, existingDestFID, ignoreExt, tree, depth+1)
			} else {
				saveReq := driver.SaveShareReq{
					FIDs:      []string{fid},
					FIDTokens: []string{sf.ShareFIDToken},
					ToPdirFID: destDirFID,
					PwdID:     pwdID,
					Stoken:    stoken,
					FileNames: []string{name},
				}
				_, _, err := e.st.SaveShareFiles(ctx, saveReq)
				if err != nil {
					if isPermissionDenied(err) {
						e.line(fmt.Sprintf("INFO: 子目录转存被拒绝，尝试刷新授权后重试一次: %s", name))
						if pingErr := e.drv.Ping(ctx); pingErr != nil {
							e.line(fmt.Sprintf("WARN: 刷新授权请求失败: %v", pingErr))
						} else if _, _, err = e.st.SaveShareFiles(ctx, saveReq); err == nil {
							e.line("INFO: 重试子目录转存成功")
						}
					}
				}
				if err != nil {
					e.line(fmt.Sprintf("FAIL: 转存子目录 %s 失败: %v", name, err))
				} else {
					e.transferCount++
					tree.add(depth, "📁"+name)
				}
			}
			continue
		}

		// 文件：已存在则跳过（来自Trae）
		if _, ok := destNameSet[normalizeName(name, ignoreExt)]; ok {
			continue
		}
		saveReq := driver.SaveShareReq{
			FIDs:      []string{fid},
			FIDTokens: []string{sf.ShareFIDToken},
			ToPdirFID: destDirFID,
			PwdID:     pwdID,
			Stoken:    stoken,
			FileNames: []string{name},
		}
		_, _, err := e.st.SaveShareFiles(ctx, saveReq)
		if err != nil {
			if isPermissionDenied(err) {
				e.line(fmt.Sprintf("INFO: 文件转存被拒绝，尝试刷新授权后重试一次: %s", name))
				if ctx.Err() != nil {
					e.line(fmt.Sprintf("WARN: 上下文已取消，跳过重试: %v", ctx.Err()))
				} else if pingErr := e.drv.Ping(ctx); pingErr != nil {
					e.line(fmt.Sprintf("WARN: 刷新授权请求失败: %v", pingErr))
				} else if _, _, err = e.st.SaveShareFiles(ctx, saveReq); err == nil {
					e.line("INFO: 重试文件转存成功")
				} else if isPermissionDenied(err) {
					e.line(fmt.Sprintf("ERROR: 文件重试后仍然权限不足: %s", name))
				}
			}
		}
		if err != nil {
			e.line(fmt.Sprintf("FAIL: 转存文件 %s 失败: %v", name, err))
		} else {
			e.transferCount++
			tree.add(depth, name+" -> "+name)
		}
	}
}

// normalizeName 规范化文件名用于去重比较（来自Trae，与 CASX _normalize_name 一致）
func normalizeName(name string, ignoreExtension bool) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if !ignoreExtension {
		return n
	}
	if idx := strings.LastIndex(n, "."); idx > 0 {
		return n[:idx]
	}
	return n
}

// ValidateSchedule 校验调度条件（截止日期 + 运行星期），不满足返回 ErrSkipTask（来自Trae）
func ValidateSchedule(task *domain.DramaTask, allowOnce bool, now time.Time) error {
	// 截止日期（来自Trae）
	if task.EndDate != "" {
		if endDate, err := time.Parse("2006-01-02", task.EndDate); err == nil {
			if now.After(endDate.Add(24 * time.Hour)) {
				return ErrSkipTask
			}
		}
	}
	if allowOnce {
		return nil
	}
	// 运行星期（来自Trae）
	runWeek := strings.TrimSpace(task.RunWeek)
	if runWeek == "" {
		return nil // 未配置则默认允许
	}
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7（与 CASX isoweekday 一致）
	}
	for _, s := range strings.Split(runWeek, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		var d int
		fmt.Sscanf(s, "%d", &d)
		if d == weekday {
			return nil
		}
	}
	return ErrSkipTask
}

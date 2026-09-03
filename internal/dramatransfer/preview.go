package dramatransfer

// 追剧分享链接预览服务（来自Trae）。
// 移植自 CASX preview_share：读取分享目录内容，按命名正则计算重命名效果，
// 并标注「已在目标目录 / 起始及之前 / 重命名冲突」等状态，供前端层级浏览与转存校验。

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

// PreviewShareInput 分享预览入参（来自Trae）
type PreviewShareInput struct {
	AccountID       int64  // 目标存储账号
	ShareURL        string // 分享链接
	PdirFID         string // 分享目录 fid（层级浏览用），空则从链接解析
	MaxItems        int    // 单页最大条目数
	TaskName        string // 任务名（用于 {TASKNAME} 变量）
	Pattern         string // 命名正则（可为 $ 内置规则名）
	Replace         string // 替换模板
	SortIndex       int    // {I+} 序号基数
	SavePath        string // 目标保存目录（用于已存在标注）
	IgnoreExtension bool   // 忽略后缀判重
	UpdateSubdir    string // 子目录同步正则（目录条目据此标注是否参与）
	StartFID        string // 增量起始文件 fid
}

// PreviewShareItem 预览条目（来自Trae）
type PreviewShareItem struct {
	FID           string
	FIDToken      string
	Name          string // 原始名
	NameRe        string // 重命名后的目标名；为空表示不参与转存
	IsDir         bool
	UpdatedAt     int64
	Size          int64
	ChildrenCount int
	NameSaved     string // 状态标注：已存在文件名 / 起始及之前 / 重命名冲突（保留最大）
}

// PreviewShareResult 预览结果（来自Trae）
type PreviewShareResult struct {
	DriveType string
	PwdID     string
	PdirFID   string
	Items     []PreviewShareItem
}

// previewVideoExts 参与重命名预览的视频后缀（来自Trae，与 CASX video_exts 一致）
var previewVideoExts = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".mov": true, ".wmv": true,
	".flv": true, ".webm": true, ".m4v": true, ".ts": true, ".m2ts": true,
	".mpg": true, ".mpeg": true, ".3gp": true, ".cas": true,
}

func isPreviewVideoName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext != "" && previewVideoExts[ext]
}

// PreviewShare 预览分享目录内容与重命名效果（来自Trae）。
// 通过 driverexec 获取账号驱动后调用 previewShareWithDriver。
func (s *Service) PreviewShare(ctx context.Context, in PreviewShareInput) (*PreviewShareResult, error) {
	if in.AccountID == 0 {
		return nil, domain.Errorf(domain.CodeValidation, "请选择存储账号")
	}
	if strings.TrimSpace(in.ShareURL) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "分享链接不能为空")
	}
	overrides, err := s.enabledRegexOverrides(ctx)
	if err != nil {
		return nil, err
	}

	var result *PreviewShareResult
	err = s.exec.Run(ctx, in.AccountID, func(drv driver.Driver) error {
		res, err := previewShareWithDriver(ctx, drv, in, overrides)
		if err != nil {
			return err
		}
		result = res
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// previewShareWithDriver 在已获取驱动的上下文内执行预览（来自Trae）
func previewShareWithDriver(ctx context.Context, drv driver.Driver, in PreviewShareInput, overrides map[string]MagicRegexEntry) (*PreviewShareResult, error) {
	st, ok := drv.(driver.ShareTransferer)
	if !ok {
		return nil, domain.Errorf(domain.CodeNotImplement, "驱动 %s 不支持分享预览", drv.Config().Name)
	}

	// 1. 解析分享链接（来自Trae）
	pwdID, passcode, urlPdirFID, err := st.ExtractShareURL(in.ShareURL)
	if err != nil {
		return nil, err
	}

	// 2. 获取分享 token（来自Trae）
	stoken, err := st.GetShareToken(ctx, pwdID, passcode)
	if err != nil {
		return nil, err
	}

	// 3. 确定浏览目录 fid（来自Trae）
	pdirFID := strings.TrimSpace(in.PdirFID)
	if pdirFID == "" {
		pdirFID = urlPdirFID
	}

	// 4. 拉取分享目录一级条目（来自Trae）
	shareItems, err := st.ListShareItems(ctx, pwdID, stoken, pdirFID)
	if err != nil {
		return nil, err
	}

	// 5. 构造 MagicRename 并展开命名正则（来自Trae）
	mr := NewMagicRename()
	mr.WithOverrides(overrides, nil)
	mr.SetTaskName(in.TaskName)
	actualPattern, actualReplace := mr.MagicRegexConv(in.Pattern, in.Replace)

	compiledSearch := mr.compile(actualPattern)
	compiledSubdir := mr.compile(in.UpdateSubdir)

	// 6. 解析目标目录已有文件名（来自Trae）
	var destFileNames []string
	destResolved := false
	if path := strings.TrimSpace(in.SavePath); path != "" {
		normalized := regexp.MustCompile(`/+`).ReplaceAllString("/"+path, "/")
		if pathFIDs, err := st.ResolvePathToFID(ctx, []string{normalized}); err == nil && len(pathFIDs) > 0 && strings.TrimSpace(pathFIDs[0].FID) != "" {
			if destItems, err := drv.ListFiles(ctx, pathFIDs[0].FID); err == nil {
				for _, it := range destItems {
					if !it.IsDir && it.Name != "" {
						destFileNames = append(destFileNames, it.Name)
					}
				}
				destResolved = true
			}
		}
	}

	// 7. startfid 增量过滤（来自Trae）
	sff := newStartFIDFilter(shareItems, in.StartFID)

	// 8. 逐条计算重命名与状态（来自Trae）
	type candidate struct {
		item       driver.ShareItem
		targetName string
	}
	var candidates []candidate
	previewItems := make([]PreviewShareItem, 0, len(shareItems))

	for _, sf := range shareItems {
		fid := strings.TrimSpace(sf.FID)
		name := sf.FileName
		if fid == "" || name == "" {
			continue
		}
		isDir := sf.IsDir
		item := PreviewShareItem{
			FID:       fid,
			FIDToken:  sf.ShareFIDToken,
			Name:      name,
			IsDir:     isDir,
			UpdatedAt: sf.UpdatedAt,
			Size:      sf.Size,
		}

		// 目录或非视频文件：仅展示，不参与重命名（来自Trae）
		if isDir || !isPreviewVideoName(name) {
			// 目录仍按 update_subdir 标注是否命中（来自Trae）
			if isDir && compiledSubdir != nil && compiledSubdir.MatchString(name) {
				item.NameRe = name
			}
			previewItems = append(previewItems, item)
			continue
		}

		// startfid 之前（来自Trae）
		if !sff.shouldKeep(fid, sf.UpdatedAt) {
			item.NameSaved = "起始及之前"
			previewItems = append(previewItems, item)
			continue
		}

		// pattern 匹配（来自Trae）
		searchRe := compiledSearch
		if isDir && compiledSubdir != nil {
			searchRe = compiledSubdir
		}
		if searchRe != nil && !searchRe.MatchString(name) {
			previewItems = append(previewItems, item)
			continue
		}

		// 黑名单过滤（来自Trae）
		if !mr.PassesBlacklist(in.Pattern, name) {
			previewItems = append(previewItems, item)
			continue
		}

		targetName := name
		if !isDir {
			targetName = mr.Sub(actualPattern, actualReplace, name)
		}
		// 已存在标注（来自Trae）
		var saved string
		if destResolved {
			if existing := mr.IsExists(targetName, destFileNames, in.IgnoreExtension && !isDir); existing != "" {
				saved = existing
			}
		}
		if saved != "" {
			item.NameSaved = saved
		} else {
			item.NameRe = targetName
			candidates = append(candidates, candidate{item: sf, targetName: targetName})
		}
		previewItems = append(previewItems, item)
	}

	// 9. 重命名冲突去重：同目标名保留 size 最大 / 更新（来自Trae）
	// candidate 与 previewItems 下标一一对应，用于冲突标注。
	best := make(map[string]int)
	for idx, c := range candidates {
		key := c.targetName
		if in.IgnoreExtension {
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
	keepIdx := make(map[int]bool, len(best))
	for _, idx := range best {
		keepIdx[idx] = true
	}
	for idx, c := range candidates {
		if keepIdx[idx] {
			continue
		}
		// 找到对应 previewItems 下标并标注冲突（来自Trae）
		for i := range previewItems {
			if previewItems[i].NameRe == c.targetName && previewItems[i].NameSaved == "" {
				previewItems[i].NameSaved = "重命名冲突（保留最大）"
				previewItems[i].NameRe = ""
				break
			}
		}
	}

	// 10. {I+} 序号分配（来自Trae）
	if iPlusRe.MatchString(actualReplace) {
		startIndex := in.SortIndex
		if startIndex <= 0 {
			startIndex = 1
		}
		mr.SetDirFileList(destFileNames, actualReplace, startIndex)
		sort.SliceStable(previewItems, func(i, j int) bool {
			ei := mr.ExtractEpisode(previewItems[i].NameRe)
			ej := mr.ExtractEpisode(previewItems[j].NameRe)
			if ei != ej {
				return ei < ej
			}
			return previewItems[i].NameRe < previewItems[j].NameRe
		})
		for i := range previewItems {
			if previewItems[i].NameRe == "" {
				continue
			}
			name, _ := mr.AssignIndex(previewItems[i].NameRe, 0)
			previewItems[i].NameRe = name
		}
	}

	return &PreviewShareResult{
		DriveType: drv.Config().Name,
		PwdID:     pwdID,
		PdirFID:   pdirFID,
		Items:     previewItems,
	}, nil
}

package guangya

// 本文件实现光鸭云盘分享链接转存能力（来自Trae）。
// 接口契约见 internal/driver/driver.go 的 ShareTransferer。
// URL/payload 与 CASX guangya_adapter.py 对齐。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
	"litepan/internal/httpx"
	"litepan/pkg/strutil"
)

// urlQueryUnescape 包装 url.QueryUnescape 以保持调用简洁（来自Trae）
func urlQueryUnescape(s string) (string, error) { return url.QueryUnescape(s) }

// 光鸭分享相关路径（与 transport.go 的 apiBaseURL 拼接）
const (
	pathShareSummary     = "/userres/v1/get_share_summary"
	pathShareAccessToken = "/userres/v1/get_share_access_token"
	pathShareFilesList   = "/userres/v1/get_share_page_files_list"
	pathShareRestore     = "/userres/v1/restore_share"
)

// shareSummaryData 是 get_share_summary 的 data 字段（来自Trae）
type shareSummaryData struct {
	ShareID     string `json:"shareId"`
	ShareName   string `json:"shareName"`
	ExpiredText string `json:"expiredText"`
}

// shareAccessTokenData 是 get_share_access_token 的 data（来自Trae）
// CASX 中 access_token 在 data 内为字符串，这里用 FlexibleString 兼容
type shareAccessTokenData struct {
	AccessToken string `json:"accessToken"`
}

// shareFileEntry 是光鸭分享文件单项（来自Trae）
type shareFileEntry struct {
	FileID   string `json:"fileId"`
	ParentID string `json:"parentId"`
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
	ResType  int    `json:"resType"` // 2=文件夹
	UTime    int64  `json:"utime"`
	DirType  int    `json:"dirType"`
	FileType int    `json:"fileType"`
}

func (e shareFileEntry) toShareItem() driver.ShareItem {
	return driver.ShareItem{
		FID:       e.FileID,
		FileName:  e.FileName,
		Size:      e.FileSize,
		UpdatedAt: e.UTime,
		IsDir:     e.ResType == 2 || e.DirType == 1,
	}
}

// shareListData 是 get_share_page_files_list 的 data（来自Trae）
type shareListData struct {
	List  []shareFileEntry `json:"list"`
	Total int              `json:"total"`
}

// shareRestoreData 是 restore_share 的返回（来自Trae）
type shareRestoreData struct {
	TaskID string `json:"taskId"`
}

// shareStokenPayload 是光鸭 stoken 的内部 JSON 结构（来自Trae）
type shareStokenPayload struct {
	ShareID     string `json:"share_id"`
	AccessToken string `json:"access_token"`
	Code        string `json:"code"`
}

// 光鸭 extract_url 正则组（来自Trae）
var (
	guangyaPasscodeRe1 = regexp.MustCompile(`[（(]提取码[：:]\s*([a-zA-Z0-9]{4,8})[)）]`)
	guangyaPasscodeRe2 = regexp.MustCompile(`[（(]访问码[：:]\s*([a-zA-Z0-9]{4,8})[)）]`)
	guangyaPasscodeRe3 = regexp.MustCompile(`提取码[：:]\s*([a-zA-Z0-9]{4,8})`)
	guangyaPasscodeRe4 = regexp.MustCompile(`访问码[：:]\s*([a-zA-Z0-9]{4,8})`)
	guangyaURLRe       = regexp.MustCompile(`(?i)https?://[^\s]*guangyapan\.com[^\s]*`)
	guangyaSharePathRe = regexp.MustCompile(`(?i)/share/([A-Za-z0-9_-]+)`)
	guangyaSharePath2  = regexp.MustCompile(`(?i)/s/([A-Za-z0-9_-]+)`)
	guangyaShareLink   = regexp.MustCompile(`(?i)/link/([A-Za-z0-9_-]+)`)
	guangyaShareDL     = regexp.MustCompile(`(?i)/download/([A-Za-z0-9_-]+)`)
)

// publicPostShare 是光鸭分享接口的匿名访问通道（来自Trae）。
// 与 d.apiRequest 不同：不带 Authorization、走公共 did/dt 头。
func (d *Driver) publicPostShare(ctx context.Context, path string, body map[string]any, out any) error {
	if err := d.waitOperationDelay(ctx); err != nil {
		return err
	}
	req, err := httpx.NewJSONRequest(ctx, http.MethodPost, d.apiBase()+path, nil, body)
	if err != nil {
		return domain.Wrap(domain.CodeInternal, err)
	}
	httpx.SetHeaders(req, d.buildPublicShareHeaders())

	resp, data, err := httpx.Execute(d.client, req, httpx.DefaultReadLimit)
	if err != nil {
		return domain.Wrap(domain.CodeDriverError, err)
	}
	if resp.StatusCode != http.StatusOK {
		return domain.Errorf(domain.CodeDriverError, "光鸭分享 HTTP %d: %s", resp.StatusCode, httpx.Truncate(data, 500))
	}
	var env apiEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return domain.Wrap(domain.CodeDriverError, err)
	}
	if env.Code != 0 {
		return mapAPIError(env.Code, strutil.FirstNonEmpty(env.Msg, "光鸭分享请求失败"))
	}
	if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return domain.Wrap(domain.CodeDriverError, err)
		}
	}
	return nil
}

// buildPublicShareHeaders 光鸭分享接口的公共请求头（来自Trae）
func (d *Driver) buildPublicShareHeaders() map[string]string {
	return map[string]string{
		"Accept":       "application/json, text/plain, */*",
		"Content-Type": "application/json",
		"did":          d.deviceID(),
		"dt":           "4",
		"Origin":       webBaseURL,
		"Referer":      webBaseURL + "/",
		"User-Agent":   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36",
	}
}

// ExtractShareURL 解析光鸭分享链接（来自Trae）
func (d *Driver) ExtractShareURL(shareURL string) (string, string, string, error) {
	raw := strings.TrimSpace(shareURL)
	if raw == "" {
		return "", "", "0", domain.Errorf(domain.CodeValidation, "分享链接为空")
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(raw, "？", "?"), "＆", "&")
	compact := regexp.MustCompile(`\s+`).ReplaceAllString(normalized, "")

	passcode := ""
	for _, re := range []*regexp.Regexp{guangyaPasscodeRe1, guangyaPasscodeRe2, guangyaPasscodeRe3, guangyaPasscodeRe4} {
		if m := re.FindStringSubmatch(compact); len(m) > 1 {
			passcode = strings.TrimSpace(m[1])
			compact = strings.Replace(compact, m[0], "", 1)
			break
		}
	}

	extractedURL := ""
	if m := guangyaURLRe.FindString(compact); m != "" {
		extractedURL = m
	} else if strings.Contains(strings.ToLower(compact), "guangyapan.com") {
		if strings.HasPrefix(compact, "http") {
			extractedURL = compact
		} else {
			extractedURL = "https://" + strings.TrimLeft(compact, "/")
		}
	} else {
		extractedURL = raw
	}

	// 解析 query
	parsed := parseURLQuery(extractedURL)
	if passcode == "" {
		for _, key := range []string{"pwd", "code", "passcode", "accessCode"} {
			if v, ok := parsed[key]; ok && v != "" {
				passcode = v
				break
			}
		}
	}
	shareID := ""
	for _, key := range []string{"shareId", "share_id", "id", "sid"} {
		if v, ok := parsed[key]; ok && v != "" {
			shareID = v
			break
		}
	}
	pdirFID := "0"
	for _, key := range []string{"parentId", "parent_id", "pdir_fid", "fid", "fileId"} {
		if v, ok := parsed[key]; ok && v != "" {
			pdirFID = normalizeGuangyaOutputFID(v)
			break
		}
	}

	// path 解析
	if shareID == "" {
		pathStr := parseURLPath(extractedURL)
		for _, re := range []*regexp.Regexp{guangyaSharePathRe, guangyaSharePath2, guangyaShareLink, guangyaShareDL} {
			if m := re.FindStringSubmatch(pathStr); len(m) > 1 {
				shareID = m[1]
				break
			}
		}
	}

	if shareID == "" {
		return "", "", "0", domain.Errorf(domain.CodeValidation, "无法解析光鸭分享链接")
	}
	return shareID, passcode, pdirFID, nil
}

// GetShareToken 获取光鸭分享访问令牌；返回内部 JSON 字符串作为 stoken（来自Trae）
func (d *Driver) GetShareToken(ctx context.Context, pwdID, passcode string) (string, error) {
	if strings.TrimSpace(pwdID) == "" {
		return "", domain.Errorf(domain.CodeValidation, "shareId 不能为空")
	}
	// 先校验分享存在
	var summary shareSummaryData
	if err := d.publicPostShare(ctx, pathShareSummary, map[string]any{"shareId": pwdID}, &summary); err != nil {
		return "", err
	}
	// 再换 access_token
	var tokenData shareAccessTokenData
	if err := d.publicPostShare(ctx, pathShareAccessToken, map[string]any{
		"shareId": pwdID,
		"code":    passcode,
	}, &tokenData); err != nil {
		return "", err
	}
	if strings.TrimSpace(tokenData.AccessToken) == "" {
		return "", domain.Errorf(domain.CodePermissionDenied, "光鸭分享访问令牌获取失败（提取码错误或分享不可访问）")
	}
	payload := shareStokenPayload{
		ShareID:     pwdID,
		AccessToken: tokenData.AccessToken,
		Code:        passcode,
	}
	b, _ := json.Marshal(payload)
	return string(b), nil
}

// ListShareItems 拉取光鸭分享目录下的一级条目（来自Trae）
func (d *Driver) ListShareItems(ctx context.Context, pwdID, stoken, pdirFID string) ([]driver.ShareItem, error) {
	if strings.TrimSpace(pwdID) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "pwd_id 不能为空")
	}
	access, err := parseGuangyaStoken(stoken, pwdID, passcodeFromStoken(stoken))
	if err != nil {
		return nil, err
	}
	if access == "" {
		return nil, domain.Errorf(domain.CodePermissionDenied, "光鸭分享 access_token 缺失")
	}
	parentID := strings.TrimSpace(pdirFID)
	if parentID == "" || parentID == "0" || parentID == "/" || parentID == "root" {
		parentID = ""
	}

	var merged []driver.ShareItem
	for page := 1; ; page++ {
		var data shareListData
		if err := d.publicPostShare(ctx, pathShareFilesList, map[string]any{
			"accessToken": access,
			"parentId":    parentID,
			"page":        page,
			"pageSize":    50,
			"orderBy":     0,
			"sortType":    0,
		}, &data); err != nil {
			return nil, err
		}
		if len(data.List) == 0 {
			break
		}
		for _, e := range data.List {
			merged = append(merged, e.toShareItem())
		}
		if data.Total > 0 && len(merged) >= data.Total {
			break
		}
		if len(data.List) < 50 {
			break
		}
		if page > 200 {
			break
		}
	}
	return merged, nil
}

// SaveShareFiles 提交光鸭分享转存，并在内部完成轮询；返回 savedFIDs（来自Trae）。
// 光鸭 restore_share 是同步接口，但落盘有延迟，需要按文件名 diff 目标目录对齐 saved_fids。
func (d *Driver) SaveShareFiles(ctx context.Context, req driver.SaveShareReq) (string, []string, error) {
	if len(req.FIDs) == 0 {
		return "", nil, nil
	}
	access, err := parseGuangyaStoken(req.Stoken, req.PwdID, "")
	if err != nil {
		return "", nil, err
	}
	if access == "" {
		return "", nil, domain.Errorf(domain.CodePermissionDenied, "光鸭分享 access_token 缺失")
	}
	destFID := normalizeGuangyaOutputFID(req.ToPdirFID)
	parentForRestore := ""
	if destFID != "0" {
		parentForRestore = destFID
	}

	// 转存前快照目标目录中已有 fid，用于事后 diff
	beforeFIDs := map[string]struct{}{}
	if len(req.FileNames) > 0 {
		if items, err := d.ListFiles(ctx, destFID); err == nil {
			for _, it := range items {
				if strings.TrimSpace(it.ID) != "" {
					beforeFIDs[it.ID] = struct{}{}
				}
			}
		}
	}

	var data shareRestoreData
	if err := d.apiRequest(ctx, pathShareRestore, map[string]any{
		"accessToken": access,
		"fileIds":     req.FIDs,
		"parentId":    parentForRestore,
	}, &data); err != nil {
		return "", nil, err
	}
	taskID := strings.TrimSpace(data.TaskID)
	if taskID != "" {
		// 轮询任务状态
		for attempt := 0; attempt < 20; attempt++ {
			status, err := d.queryGuangyaShareTask(ctx, taskID)
			if err != nil {
				return taskID, nil, err
			}
			if status.Failed {
				return taskID, nil, domain.Errorf(domain.CodeDriverError, "光鸭转存任务失败：%s", status.Message)
			}
			if status.Done {
				break
			}
			select {
			case <-ctx.Done():
				return taskID, nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}

	// 按文件名 diff 目标目录对齐 saved_fids
	var aligned []string
	if len(req.FileNames) > 0 {
		for attempt := 0; attempt < 15; attempt++ {
			aligned = d.diffDestByName(ctx, destFID, beforeFIDs, req.FileNames)
			if len(aligned) > 0 && anyNonEmpty(aligned) {
				break
			}
			select {
			case <-ctx.Done():
				return taskID, aligned, nil
			case <-time.After(time.Second):
			}
		}
	}
	if taskID == "" {
		taskID = fmt.Sprintf("guangya_sync_%d", time.Now().Unix())
	}
	return taskID, aligned, nil
}

// QueryTransferTask 光鸭转存任务已在 SaveShareFiles 内同步完成，这里直接返回完成（来自Trae）
func (d *Driver) QueryTransferTask(ctx context.Context, taskID string) (driver.TransferStatus, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || strings.HasPrefix(taskID, "guangya_sync_") {
		return driver.TransferStatus{Done: true}, nil
	}
	return d.queryGuangyaShareTask(ctx, taskID)
}

// ResolvePathToFID 把光鸭绝对路径逐级向下解析为 fid（来自Trae）。
// 光鸭没有夸克那种批量 path_list 接口，需逐目录 ls。
func (d *Driver) ResolvePathToFID(ctx context.Context, paths []string) ([]driver.PathFID, error) {
	out := make([]driver.PathFID, 0, len(paths))
	for _, p := range paths {
		out = append(out, driver.PathFID{Path: p, FID: d.resolveGuangyaPathToFID(ctx, p)})
	}
	return out, nil
}

// resolveGuangyaPathToFID 逐级解析路径（来自Trae）
func (d *Driver) resolveGuangyaPathToFID(ctx context.Context, path string) string {
	normalized := regexp.MustCompile(`/+`).ReplaceAllString("/"+strings.TrimSpace(path), "/")
	if normalized == "/" {
		return "0"
	}
	parts := strings.Split(strings.Trim(normalized, "/"), "/")
	parentID := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		items, err := d.ListFiles(ctx, parentID)
		if err != nil {
			return ""
		}
		found := ""
		for _, it := range items {
			if it.IsDir && it.Name == part {
				found = it.ID
				break
			}
		}
		if found == "" {
			return ""
		}
		parentID = found
	}
	if parentID == "" {
		return "0"
	}
	return parentID
}

// queryGuangyaShareTask 查询光鸭转存任务状态（来自Trae）
func (d *Driver) queryGuangyaShareTask(ctx context.Context, taskID string) (driver.TransferStatus, error) {
	var data taskStatusData
	if err := d.apiRequest(ctx, pathTaskStatus, map[string]any{"taskId": taskID}, &data); err != nil {
		return driver.TransferStatus{}, err
	}
	switch data.Status {
	case taskStatusDone:
		return driver.TransferStatus{Done: true}, nil
	case -1, taskStatusFail:
		return driver.TransferStatus{Failed: true, Message: fmt.Sprintf("光鸭任务失败 status=%d", data.Status)}, nil
	default:
		return driver.TransferStatus{Done: false}, nil
	}
}

// diffDestByName 在转存后按文件名对齐目标目录中的新 fid（来自Trae）。
// 与 CASX guangya_adapter.py _diff_dest_dir 一致。
func (d *Driver) diffDestByName(ctx context.Context, destFID string, beforeFIDs map[string]struct{}, fileNames []string) []string {
	items, err := d.ListFiles(ctx, destFID)
	if err != nil {
		return nil
	}
	nameToFIDs := map[string][]string{}
	for _, it := range items {
		if it.IsDir {
			continue
		}
		if _, ok := beforeFIDs[it.ID]; ok {
			continue
		}
		if strings.TrimSpace(it.Name) == "" {
			continue
		}
		nameToFIDs[it.Name] = append(nameToFIDs[it.Name], it.ID)
	}
	aligned := make([]string, 0, len(fileNames))
	used := map[string]struct{}{}
	for _, name := range fileNames {
		picked := ""
		for _, fid := range nameToFIDs[name] {
			if _, ok := used[fid]; ok {
				continue
			}
			picked = fid
			break
		}
		if picked != "" {
			used[picked] = struct{}{}
		}
		aligned = append(aligned, picked)
	}
	return aligned
}

// parseGuangyaStoken 从 stoken JSON 字符串中提取 access_token（来自Trae）
func parseGuangyaStoken(stoken, pwdID, passcode string) (string, error) {
	stoken = strings.TrimSpace(stoken)
	if stoken == "" {
		return "", nil
	}
	var payload shareStokenPayload
	if err := json.Unmarshal([]byte(stoken), &payload); err != nil {
		return "", nil
	}
	return strings.TrimSpace(payload.AccessToken), nil
}

// passcodeFromStoken 从 stoken JSON 中提取 code 字段（来自Trae）
func passcodeFromStoken(stoken string) string {
	stoken = strings.TrimSpace(stoken)
	if stoken == "" {
		return ""
	}
	var payload shareStokenPayload
	_ = json.Unmarshal([]byte(stoken), &payload)
	return payload.Code
}

// normalizeGuangyaOutputFID 与 CASX _normalize_output_fid 对齐（来自Trae）
func normalizeGuangyaOutputFID(fid string) string {
	v := strings.TrimSpace(fid)
	if v == "" {
		return "0"
	}
	return v
}

// anyNonEmpty 切片中是否存在非空字符串（来自Trae）
func anyNonEmpty(s []string) bool {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

var _ driver.ShareTransferer = (*Driver)(nil)

// parseURLQuery 解析 URL query 参数为 map（来自Trae）。
// 容错：解析失败返回空 map。
func parseURLQuery(rawURL string) map[string]string {
	out := map[string]string{}
	idx := strings.Index(rawURL, "?")
	if idx < 0 {
		return out
	}
	query := rawURL[idx+1:]
	if h := strings.Index(query, "#"); h >= 0 {
		query = query[:h]
	}
	for _, kv := range strings.Split(query, "&") {
		if kv == "" {
			continue
		}
		eq := strings.Index(kv, "=")
		if eq < 0 {
			out[kv] = ""
			continue
		}
		k := kv[:eq]
		v := kv[eq+1:]
		if decoded, err := urlQueryUnescape(v); err == nil {
			out[k] = decoded
		} else {
			out[k] = v
		}
	}
	return out
}

// parseURLPath 返回 URL 的 path 部分（来自Trae）
func parseURLPath(rawURL string) string {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return ""
	}
	// 去掉 scheme
	if idx := strings.Index(u, "://"); idx >= 0 {
		u = u[idx+3:]
	}
	// 去掉 query/fragment
	if idx := strings.IndexAny(u, "?#"); idx >= 0 {
		u = u[:idx]
	}
	// 去掉 host
	if idx := strings.Index(u, "/"); idx >= 0 {
		return u[idx:]
	}
	return "/"
}

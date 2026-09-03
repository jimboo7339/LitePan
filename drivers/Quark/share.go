package quark

// 本文件实现夸克分享链接转存能力（来自Trae）。
// 接口契约见 internal/driver/driver.go 的 ShareTransferer。
// URL/payload 与 CASX quark_adapter.py 对齐，仅在错误处理与返回结构上按 LitePan 风格调整。

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
)

// 夸克分享相关路径（与 transport.go 的 baseURL 拼接）
const (
	pathShareToken   = "/share/sharepage/token"
	pathShareDetail  = "/share/sharepage/detail"
	pathShareSave    = "/share/sharepage/save"
	pathFilePathList = "/file/info/path_list"
)

// 夸克分享 detail 单项（来自Trae）
// FIDToken 同时兼容 fid_token 和 share_fid_token（夸克不同接口版本字段名不同），来自Trae
type shareDetailItem struct {
	FID       string      `json:"fid"`
	FileName  string      `json:"file_name"`
	Size      int64       `json:"size"`
	FileType  int         `json:"file_type"` // 0=文件夹，1=文件
	UpdatedAt json.Number `json:"updated_at"`
	FIDToken  string      `json:"fid_token"`
	FIDToken2 string      `json:"share_fid_token"`
	PdirFID   string      `json:"pdir_fid"`
}

func (it shareDetailItem) token() string {
	if it.FIDToken != "" {
		return it.FIDToken
	}
	return it.FIDToken2
}

func (it shareDetailItem) toShareItem() driver.ShareItem {
	return driver.ShareItem{
		FID:           it.FID,
		FileName:      it.FileName,
		Size:          it.Size,
		UpdatedAt:     parseShareEpoch(it.UpdatedAt),
		IsDir:         it.FileType == 0,
		ShareFIDToken: it.token(),
	}
}

func parseShareEpoch(n json.Number) int64 {
	v, err := n.Int64()
	if err != nil || v <= 0 {
		return 0
	}
	if v > 1e12 {
		return v / 1000 // 毫秒 -> 秒
	}
	return v
}

// shareDetailData 是 share/detail 的 data 字段
type shareDetailData struct {
	List []shareDetailItem `json:"list"`
}

// shareDetailMeta 是 share/detail 的 metadata（用于总数判断）
type shareDetailMeta struct {
	Total int `json:"_total"`
}

// shareTokenData 是 share/sharepage/token 的 data 字段
type shareTokenData struct {
	Stoken string `json:"stoken"`
}

// shareSaveData 是 share/sharepage/save 的 data 字段
type shareSaveData struct {
	TaskID        string   `json:"task_id"`
	SaveAsTopFIDs []string `json:"save_as_top_fids"`
}

// pathInfoData 是 file/info/path_list 的 data 字段（数组）
type pathInfoItem struct {
	FID      string `json:"fid"`
	FileName string `json:"file_name"`
	PdirFID  string `json:"pdir_fid"`
	FileType int    `json:"file_type"`
}

// extractURLPatterns 与 CASX quark_adapter.py 保持一致（来自Trae）
var (
	quarkShareIDRe   = regexp.MustCompile(`/s/(\w+)`)
	quarkPasscodeRe  = regexp.MustCompile(`pwd=(\w+)`)
	quarkSharePathRe = regexp.MustCompile(`/(\w{32})-?([^/]+)?`)
)

// ExtractShareURL 解析夸克分享链接（来自Trae）
func (d *Driver) ExtractShareURL(shareURL string) (string, string, string, error) {
	raw := strings.TrimSpace(shareURL)
	if raw == "" {
		return "", "", "0", domain.Errorf(domain.CodeValidation, "分享链接为空")
	}
	pwdID := ""
	if m := quarkShareIDRe.FindStringSubmatch(raw); len(m) > 1 {
		pwdID = m[1]
	}
	passcode := ""
	if m := quarkPasscodeRe.FindStringSubmatch(raw); len(m) > 1 {
		passcode = m[1]
	}
	pdirFID := "0"
	matches := quarkSharePathRe.FindAllStringSubmatch(raw, -1)
	if len(matches) > 0 {
		pdirFID = matches[len(matches)-1][1]
	}
	if pwdID == "" {
		return "", "", "0", domain.Errorf(domain.CodeValidation, "无法解析分享链接：缺少 /s/{id}")
	}
	return pwdID, passcode, pdirFID, nil
}

// GetShareToken 获取夸克分享 stoken（来自Trae）
func (d *Driver) GetShareToken(ctx context.Context, pwdID, passcode string) (string, error) {
	if strings.TrimSpace(pwdID) == "" {
		return "", domain.Errorf(domain.CodeValidation, "pwd_id 不能为空")
	}
	body := map[string]any{"pwd_id": pwdID, "passcode": passcode}
	var data shareTokenData
	if _, err := d.apiRequest(ctx, http.MethodPost, pathShareToken, nil, body, &data); err != nil {
		return "", err
	}
	if strings.TrimSpace(data.Stoken) == "" {
		return "", domain.Errorf(domain.CodeDriverError, "夸克未返回 stoken")
	}
	return data.Stoken, nil
}

// ListShareItems 分页拉取夸克分享目录下的一级条目（来自Trae）
func (d *Driver) ListShareItems(ctx context.Context, pwdID, stoken, pdirFID string) ([]driver.ShareItem, error) {
	if strings.TrimSpace(pwdID) == "" || strings.TrimSpace(stoken) == "" {
		return nil, domain.Errorf(domain.CodeValidation, "pwd_id 与 stoken 不能为空")
	}
	parent := strings.TrimSpace(pdirFID)
	if parent == "" {
		parent = "0"
	}
	var merged []driver.ShareItem
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("pwd_id", pwdID)
		query.Set("stoken", stoken)
		query.Set("pdir_fid", parent)
		query.Set("force", "0")
		query.Set("_page", strconv.Itoa(page))
		query.Set("_size", "50")
		query.Set("_fetch_banner", "0")
		query.Set("_fetch_share", "0")
		query.Set("_fetch_total", "1")
		query.Set("_sort", "file_type:asc,updated_at:desc")
		query.Set("ver", "2")
		query.Set("fetch_share_full_path", "0")

		// 夸克 detail 接口的 metadata._total 必须从 envelope.metadata 解析，
		// d.apiRequest 的 out 只解 data 字段，所以这里临时抓完整 envelope。
		env, err := d.apiRequest(ctx, http.MethodGet, pathShareDetail, query, nil, nil)
		if err != nil {
			return nil, err
		}
		var data shareDetailData
		if len(env.Data) > 0 {
			if err := json.Unmarshal(env.Data, &data); err != nil {
				return nil, domain.Wrap(domain.CodeDriverError, err)
			}
		}
		if len(data.List) == 0 {
			break
		}
		for _, it := range data.List {
			merged = append(merged, it.toShareItem())
		}
		var meta shareDetailMeta
		if len(env.Metadata) > 0 {
			_ = json.Unmarshal(env.Metadata, &meta)
		}
		if meta.Total > 0 && len(merged) >= meta.Total {
			break
		}
		if len(data.List) < 50 {
			break
		}
	}
	return merged, nil
}

// SaveShareFiles 提交夸克分享转存任务，并阻塞轮询至完成（来自Trae）。
// 夸克 save 接口立即返回 task_id，转存结果异步；这里同步等到 task 完成以匹配 LitePan 的同步风格。
// 返回 taskID 留作溯源，savedFIDs 从 task 详情中提取（夸克会把 save_as_top_fids 放进 task data）。
func (d *Driver) SaveShareFiles(ctx context.Context, req driver.SaveShareReq) (string, []string, error) {
	if len(req.FIDs) == 0 {
		return "", nil, nil
	}
	if len(req.FIDs) != len(req.FIDTokens) {
		return "", nil, domain.Errorf(domain.CodeValidation, "FIDs 与 FIDTokens 长度不一致")
	}
	toPdir := strings.TrimSpace(req.ToPdirFID)
	if toPdir == "" {
		toPdir = d.rootID()
	}

	// 夸克要求转存请求带随机 __dt / __t 反爬签名，参考 CASX quark_adapter.py:304
	// __t 与 CASX 一致使用浮点时间戳，来自Trae
	query := url.Values{}
	query.Set("uc_param_str", "")
	query.Set("app", "clouddrive")
	query.Set("__dt", strconv.Itoa(int(randUniform(1, 5)*60*1000)))
	query.Set("__t", strconv.FormatFloat(float64(time.Now().UnixNano())/1e9, 'f', 6, 64))

	body := map[string]any{
		"fid_list":       req.FIDs,
		"fid_token_list": req.FIDTokens,
		"to_pdir_fid":    toPdir,
		"pwd_id":         req.PwdID,
		"stoken":         req.Stoken,
		"pdir_fid":       "0",
		"scene":          "link",
	}
	var data shareSaveData
	if _, err := d.apiRequest(ctx, http.MethodPost, pathShareSave, query, body, &data); err != nil {
		return "", nil, err
	}
	taskID := strings.TrimSpace(data.TaskID)
	savedFIDs := normalizeIDs(data.SaveAsTopFIDs)
	if taskID == "" {
		return "", savedFIDs, nil
	}

	// 夸克 task 完成后会把 save_as_top_fids 写回 task 详情；这里轮询。
	for attempt := 0; attempt < 60; attempt++ {
		status, err := d.QueryTransferTask(ctx, taskID)
		if err != nil {
			return taskID, savedFIDs, err
		}
		if status.Failed {
			return taskID, savedFIDs, domain.Errorf(domain.CodeDriverError, "夸克转存任务失败：%s", status.Message)
		}
		if status.Done {
			if len(status.SavedFIDs) > 0 {
				savedFIDs = status.SavedFIDs
			}
			return taskID, savedFIDs, nil
		}
		select {
		case <-ctx.Done():
			return taskID, savedFIDs, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return taskID, savedFIDs, domain.Errorf(domain.CodeDriverError, "夸克转存任务超时")
}

// shareTaskData 是 /task 的 data 字段（来自Trae）
// 与 CASX _extract_saved_fids 对齐：save_as_top_fids 嵌套在 save_as 对象内
type shareTaskData struct {
	Status  int `json:"status"`
	SaveAs  *struct {
		SaveAsTopFIDs []string `json:"save_as_top_fids"`
	} `json:"save_as"`
	// 兜底：部分接口版本直接放在 data 层，来自Trae
	SaveAsTopFIDs []string `json:"save_as_top_fids"`
}

func (d shareTaskData) savedFIDs() []string {
	if d.SaveAs != nil && len(d.SaveAs.SaveAsTopFIDs) > 0 {
		return d.SaveAs.SaveAsTopFIDs
	}
	return d.SaveAsTopFIDs
}

// QueryTransferTask 查询夸克 task 状态（来自Trae）
func (d *Driver) QueryTransferTask(ctx context.Context, taskID string) (driver.TransferStatus, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return driver.TransferStatus{Done: true}, nil
	}
	query := url.Values{}
	query.Set("task_id", taskID)
	query.Set("retry_index", "0")
	query.Set("__dt", strconv.Itoa(int(randUniform(1, 5)*60*1000)))
	query.Set("__t", strconv.FormatFloat(float64(time.Now().UnixNano())/1e9, 'f', 6, 64))

	var data shareTaskData
	if _, err := d.apiRequest(ctx, http.MethodGet, pathTask, query, nil, &data); err != nil {
		return driver.TransferStatus{}, err
	}
	switch data.Status {
	case 2:
		return driver.TransferStatus{Done: true, SavedFIDs: normalizeIDs(data.savedFIDs())}, nil
	case 3, 4:
		return driver.TransferStatus{Failed: true, Message: "夸克转存任务失败"}, nil
	default:
		return driver.TransferStatus{Done: false}, nil
	}
}

// ResolvePathToFID 批量把夸克绝对路径解析为 fid（来自Trae）
// 夸克 /file/info/path_list 单次最多 50 条。
func (d *Driver) ResolvePathToFID(ctx context.Context, paths []string) ([]driver.PathFID, error) {
	var out []driver.PathFID
	for i := 0; i < len(paths); i += 50 {
		end := i + 50
		if end > len(paths) {
			end = len(paths)
		}
		batch := paths[i:end]
		body := map[string]any{"file_path": batch, "namespace": "0"}
		var items []pathInfoItem
		if _, err := d.apiRequest(ctx, http.MethodPost, pathFilePathList, nil, body, &items); err != nil {
			return nil, err
		}
		// 夸克按请求顺序返回，缺失项 fid 为空
		for idx, p := range batch {
			fid := ""
			if idx < len(items) {
				fid = strings.TrimSpace(items[idx].FID)
			}
			out = append(out, driver.PathFID{Path: p, FID: fid})
		}
	}
	return out, nil
}

// randUniform 返回 [min,max) 区间浮点数；math/rand 全局即可，反爬不要求强随机（来自Trae）
func randUniform(min, max float64) float64 {
	return min + rand.Float64()*(max-min)
}

var _ driver.ShareTransferer = (*Driver)(nil)

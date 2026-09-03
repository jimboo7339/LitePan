package quark

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
	"litepan/internal/httpx"
)

// baseURL 与 CASX 一致使用 drive-pc.quark.cn，来自Trae
const (
	baseURL           = "https://drive-pc.quark.cn/1/clouddrive"
	baseURLApp        = "https://drive-m.quark.cn/1/clouddrive"
	profileMemberURL  = "https://drive-pc.quark.cn/1/clouddrive"
	profileAccountURL = "https://pan.quark.cn"
	referer           = "https://pan.quark.cn"
	// clientUA 与 CASX 一致，来自Trae
	clientUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) quark-cloud-drive/3.14.2 Chrome/112.0.5615.165 Electron/24.1.3.8 Safari/537.36 Channel/pckk_other_ch"

	pathList         = "/file/sort"
	pathInfo         = "/file/info"
	pathDownload     = "/file/download"
	pathCreate       = "/file"
	pathRename       = "/file/rename"
	pathMove         = "/file/move"
	pathCopy         = "/file/copy"
	pathTask         = "/task"
	pathTrash        = "/file/delete"
	pathRecycleList  = "/file/recycle/list"
	pathRecycleDel   = "/file/recycle/remove"
	pathUploadPre    = "/file/upload/pre"
	pathUpdateHash   = "/file/update/hash"
	pathUploadAuth   = "/file/upload/auth"
	pathUploadFinish = "/file/upload/finish"

	listPageSize          = 200
	requestInterval       = 0
	convergeDelayMS       = 500
	downloadURLTTLSeconds = 300
	proxyPartSize         = 10 * 1024 * 1024
	proxyConcurrency      = 3
)

// matchMparamFromCookie 与 CASX _match_mparam_form_cookie 一致，使用正则提取 kps/sign/vcode，来自Trae
var (
	reKps   = regexp.MustCompile(`(?:^|[;&\s])kps=([a-zA-Z0-9%+/=]+)`)
	reSign  = regexp.MustCompile(`(?:^|[;&\s])sign=([a-zA-Z0-9%+/=]+)`)
	reVcode = regexp.MustCompile(`(?:^|[;&\s])vcode=([a-zA-Z0-9%+/=]+)`)
)

func (d *Driver) matchMparamFromCookie(cookie string) map[string]string {
	out := map[string]string{}
	if cookie == "" {
		return out
	}
	// 与 CASX 一致：正则匹配后做 %25 -> % 解码，来自Trae
	if m := reKps.FindStringSubmatch(cookie); len(m) > 1 {
		out["kps"] = strings.ReplaceAll(m[1], "%25", "%")
	}
	if m := reSign.FindStringSubmatch(cookie); len(m) > 1 {
		out["sign"] = strings.ReplaceAll(m[1], "%25", "%")
	}
	if m := reVcode.FindStringSubmatch(cookie); len(m) > 1 {
		out["vcode"] = strings.ReplaceAll(m[1], "%25", "%")
	}
	return out
}

func (d *Driver) apiBase() string { return baseURL }

func (d *Driver) rootID() string {
	if id := strings.TrimSpace(d.add.RootFolderID); id != "" {
		return id
	}
	return "0"
}

func (d *Driver) normalizeParent(parentID string) string {
	p := strings.TrimSpace(parentID)
	if p == "" || p == "/" || p == "root" || p == "0" {
		return d.rootID()
	}
	return p
}

type quarkEnvelope struct {
	Status   int             `json:"status"`
	Code     int             `json:"code"`
	Message  string          `json:"message"`
	Data     json.RawMessage `json:"data"`
	Metadata json.RawMessage `json:"metadata"`
}

func (d *Driver) apiRequest(ctx context.Context, method, path string, query url.Values, body, out any) (*quarkEnvelope, error) {
	return d.apiRequestTo(ctx, d.apiBase(), method, path, query, body, out)
}

func (d *Driver) apiRequestTo(ctx context.Context, apiBase, method, path string, query url.Values, body, out any) (*quarkEnvelope, error) {
	if err := d.waitInterval(ctx); err != nil {
		return nil, err
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("pr", "ucpro")
	query.Set("fr", "pc")

	useMobileShare := false
	// 与 CASX 一致：path 含 "share" 且 cookie 有 kps/sign/vcode 时切移动端，来自Trae
	if apiBase == baseURL && strings.Contains(path, "share") {
		ck := d.currentCookie()
		mparam := d.matchMparamFromCookie(ck)
		if ck != "" && len(mparam) == 3 {
			apiBase = baseURLApp
			query.Set("device_model", "M2011K2C")
			query.Set("entry", "default_clouddrive")
			query.Set("_t_group", "0%3A_s_vp%3A1")
			query.Set("dmn", "Mi%2B11")
			query.Set("fr", "android")
			query.Set("pf", "3300")
			query.Set("bi", "35937")
			query.Set("ve", "7.4.5.680")
			query.Set("ss", "411x875")
			query.Set("mi", "M2011K2C")
			query.Set("nt", "5")
			query.Set("nw", "0")
			query.Set("kt", "4")
			query.Set("pr", "ucpro")
			query.Set("sv", "release")
			query.Set("dt", "phone")
			query.Set("data_from", "ucapi")
			query.Set("kps", mparam["kps"])
			query.Set("sign", mparam["sign"])
			query.Set("vcode", mparam["vcode"])
			query.Set("app", "clouddrive")
			query.Set("kkkk", "1")
			useMobileShare = true
		}
	}

	req, err := httpx.NewJSONRequest(ctx, method, apiBase+path, query, body)
	if err != nil {
		return nil, domain.Wrap(domain.CodeInternal, err)
	}
	// 与 CASX 一致：仅设 User-Agent，不设 Referer/Accept（避免触发夸克 save 接口 token 校验异常），来自Trae
	req.Header.Set("User-Agent", clientUA)
	if !useMobileShare {
		if ck := d.currentCookie(); ck != "" {
			req.Header.Set("Cookie", ck)
		}
	}

	resp, data, err := httpx.Execute(d.client, req, 16<<20)
	if err != nil {
		return nil, domain.Wrap(domain.CodeDriverError, err)
	}
	d.absorbSetCookie(ctx, resp.Header)

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, domain.Errorf(domain.CodeAuthExpired, "夸克 Cookie 认证失败，请重新获取 Cookie")
	case resp.StatusCode == http.StatusForbidden:
		return nil, domain.Errorf(domain.CodePermissionDenied, "夸克访问被拒绝，Cookie 权限不足: HTTP %d", resp.StatusCode)
	case resp.StatusCode >= 400:
		return nil, domain.Errorf(domain.CodeDriverError, "夸克 HTTP %d: %s", resp.StatusCode, httpx.Truncate(data, 300))
	}

	var env quarkEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, domain.Errorf(domain.CodeDriverError, "夸克返回非 JSON 内容: %s", httpx.Truncate(data, 300))
	}
	if env.Status >= 400 || env.Code != 0 {
		return nil, mapBusinessError(env)
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return nil, domain.Wrap(domain.CodeDriverError, err)
		}
	}
	return &env, nil
}

func mapBusinessError(env quarkEnvelope) error {
	msg := strings.TrimSpace(env.Message)
	if msg == "" {
		msg = "未知错误"
	}
	switch env.Status {
	case http.StatusUnauthorized:
		return domain.Errorf(domain.CodeAuthExpired, "夸克 Cookie 认证失败：%s", msg)
	case http.StatusForbidden:
		return domain.Errorf(domain.CodePermissionDenied, "夸克访问被拒绝：%s", msg)
	}
	return domain.Errorf(domain.CodeDriverError, "夸克接口错误(%d)：%s", env.Code, msg)
}

func (d *Driver) waitInterval(ctx context.Context) error {
	return driver.WaitRequestInterval(ctx, d.intervalGate, requestInterval)
}

func (d *Driver) converge(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(convergeDelayMS * time.Millisecond):
	}
}

package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"litepan/internal/domain"
	"litepan/internal/eventbus"
	"litepan/internal/settings"
)

type Options struct {
	Repo     domain.NotificationRepository
	Accounts domain.AccountRepository
	Settings *settings.Service // 全局设置（通知渠道 webhook），来自Trae
	Log      *slog.Logger
}

type Service struct {
	repo     domain.NotificationRepository
	accounts domain.AccountRepository
	settings *settings.Service
	log      *slog.Logger

	// subs 是未读数订阅者（前端 SSE 长连接）。每个通道带 1 个缓冲，
	// 推送采用非阻塞写：慢客户端只保留最新值，不会拖住发布方。
	subMu   sync.Mutex
	subs    map[int64]chan int
	nextSub int64
}

func NewService(opts Options) *Service {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: opts.Repo, accounts: opts.Accounts, settings: opts.Settings, log: log, subs: map[int64]chan int{}}
}

// Subscribe 订阅未读数变化，返回只读通道和退订函数；退订可重复调用。
func (s *Service) Subscribe() (<-chan int, func()) {
	if s == nil {
		closed := make(chan int)
		close(closed)
		return closed, func() {}
	}
	ch := make(chan int, 1)
	s.subMu.Lock()
	id := s.nextSub
	s.nextSub++
	s.subs[id] = ch
	s.subMu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.subMu.Lock()
			if current, ok := s.subs[id]; ok {
				delete(s.subs, id)
				close(current)
			}
			s.subMu.Unlock()
		})
	}
}

// publishUnread 重算未读数并推给所有订阅者。通知已落库后才调用，因此用独立上下文，
// 避免发布方请求结束时上下文已取消导致推送丢失。
func (s *Service) publishUnread() {
	if s == nil || s.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	count, err := s.repo.UnreadCount(ctx)
	if err != nil {
		return
	}
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- count:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- count:
			default:
			}
		}
	}
}

func (s *Service) Register(bus *eventbus.Bus) {
	if s == nil || bus == nil {
		return
	}
	eventbus.Subscribe(bus, s.onAuthFailed)
	eventbus.Subscribe(bus, s.onAuthRecovered)
	eventbus.Subscribe(bus, s.onCreated)
}

func (s *Service) List(ctx context.Context, limit, offset int) ([]*domain.Notification, error) {
	if s.repo == nil {
		return nil, domain.Errorf(domain.CodeInternal, "通知仓储未就绪")
	}
	return s.repo.List(ctx, limit, offset)
}

func (s *Service) UnreadCount(ctx context.Context) (int, error) {
	if s.repo == nil {
		return 0, domain.Errorf(domain.CodeInternal, "通知仓储未就绪")
	}
	return s.repo.UnreadCount(ctx)
}

func (s *Service) MarkRead(ctx context.Context, id int64) error {
	if s.repo == nil {
		return domain.Errorf(domain.CodeInternal, "通知仓储未就绪")
	}
	if err := s.repo.MarkRead(ctx, id); err != nil {
		return err
	}
	s.publishUnread()
	return nil
}

func (s *Service) MarkAllRead(ctx context.Context) (int64, error) {
	if s.repo == nil {
		return 0, domain.Errorf(domain.CodeInternal, "通知仓储未就绪")
	}
	n, err := s.repo.MarkAllRead(ctx)
	if err != nil {
		return 0, err
	}
	s.publishUnread()
	return n, nil
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	if s.repo == nil {
		return domain.Errorf(domain.CodeInternal, "通知仓储未就绪")
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.publishUnread()
	return nil
}

func (s *Service) DeleteAll(ctx context.Context) (int64, error) {
	if s.repo == nil {
		return 0, domain.Errorf(domain.CodeInternal, "通知仓储未就绪")
	}
	n, err := s.repo.DeleteAll(ctx)
	if err != nil {
		return 0, err
	}
	s.publishUnread()
	return n, nil
}

func (s *Service) DeleteByRef(ctx context.Context, category string, refID int64) (int64, error) {
	if s.repo == nil {
		return 0, domain.Errorf(domain.CodeInternal, "通知仓储未就绪")
	}
	n, err := s.repo.DeleteByRef(ctx, category, refID)
	if err != nil {
		return 0, err
	}
	s.publishUnread()
	return n, nil
}

func (s *Service) Notify(ctx context.Context, level, category, title, message string, accountID, refID int64) {
	if s == nil {
		return
	}
	s.persist(ctx, level, category, title, message, accountID, refID)
}

func (s *Service) onAuthFailed(ctx context.Context, e eventbus.AccountAuthFailed) {
	if !e.Fatal {
		return
	}
	msg := e.Reason
	if name := s.accountName(ctx, e.AccountID); name != "" {
		msg = name + "：" + e.Reason
	}
	s.persist(ctx, "error", "auth", "存储账号认证已失效", msg, e.AccountID, 0)
}

func (s *Service) onAuthRecovered(ctx context.Context, e eventbus.AccountAuthRecovered) {
	name := s.accountName(ctx, e.AccountID)
	if name == "" {
		name = fmt.Sprintf("账号 #%d", e.AccountID)
	}
	s.persist(ctx, "success", "auth", "存储账号认证已恢复", name+" 认证已恢复正常", e.AccountID, 0)
}

func (s *Service) onCreated(ctx context.Context, e eventbus.NotificationCreated) {
	category := e.Category
	if category == "" {
		category = "system"
	}
	level := e.Level
	if level == "" {
		level = "info"
	}
	s.persist(ctx, level, category, e.Title, e.Message, e.AccountID, e.RefID)
}

func (s *Service) persist(ctx context.Context, level, category, title, message string, accountID, refID int64) {
	if s.repo == nil {
		return
	}
	_, err := s.repo.Create(ctx, &domain.Notification{
		Level:     level,
		Category:  category,
		Title:     title,
		Message:   message,
		AccountID: accountID,
		RefID:     refID,
	})
	if err != nil {
		s.log.Warn("写入通知失败", "title", title, "err", err)
		return
	}
	// 新通知落库成功即推送一次未读数，前端铃铛无需再靠轮询发现。
	s.publishUnread()
	// 推送到已配置的外部通知渠道（企微/钉钉/飞书），来自Trae
	// 仅对任务执行类通知转发，避免认证类通知刷屏；使用独立 context 避免任务 cancel 影响推送
	s.sendWebhook(level, category, title, message)
}

// sendWebhook 根据全局设置调用已配置的机器人 webhook，来自Trae
// 任意渠道配置非空即推送；推送异步执行，不阻塞通知持久化主流程
// 正文中的 ✅/❌/⚠️ emoji 原样保留，webhook 端只在其之前加 levelEmoji 结果前缀（来自Trae）
func (s *Service) sendWebhook(level, category, title, message string) {
	if s.settings == nil {
		return
	}
	// 只转发转存(drama)/STRM(strm*)/缓存(cache)类任务通知，避免认证等系统通知刷屏，来自Trae
	if !isTaskCategory(category) {
		return
	}
	// TrimSpace 兜底：避免用户在设置里粘贴 URL 时残留空格导致请求失败（来自Trae）
	wecomHook := strings.TrimSpace(s.settings.String(settings.KeyNotifyWecomWebhook))
	dingtalkHook := strings.TrimSpace(s.settings.String(settings.KeyNotifyDingtalkWebhook))
	feishuHook := strings.TrimSpace(s.settings.String(settings.KeyNotifyFeishuWebhook))
	if wecomHook == "" && dingtalkHook == "" && feishuHook == "" {
		return
	}
	go func() {
		if wecomHook != "" {
			s.sendWecom(wecomHook, level, title, message)
		}
		if dingtalkHook != "" {
			s.sendDingtalk(dingtalkHook, level, title, message)
		}
		if feishuHook != "" {
			s.sendFeishu(feishuHook, level, title, message)
		}
	}()
}

// isTaskCategory 判断是否为任务执行类通知（需要转发到 webhook），来自Trae
func isTaskCategory(category string) bool {
	switch category {
	case "drama", "cache", "strm", "strm_scan_warn", "strm_scrape":
		return true
	}
	return strings.HasPrefix(category, "strm")
}

// levelEmoji 把通知级别映射为表情前缀，方便群机器人阅读，来自Trae
func levelEmoji(level string) string {
	switch level {
	case "error":
		return "❌"
	case "warning", "warn":
		return "⚠️"
	case "success":
		return "✅"
	default:
		return "ℹ️"
	}
}

// sendWecom 推送到企业微信群机器人，来自Trae
// 文档：https://developer.work.weixin.qq.com/document/path/91770
// 使用纯文本类型（text），保留原消息中的换行与 emoji，避免用户需要切到企微才能看格式（来自Trae）
func (s *Service) sendWecom(webhook, level, title, message string) {
	payload := map[string]any{
		"msgtype": "text",
		"text": map[string]string{
			"content": formatText(level, title, message),
		},
	}
	s.postWebhook("wecom", webhook, payload)
}

// sendDingtalk 推送到钉钉群机器人，来自Trae
// 文档：https://open.dingtalk.com/document/robots/custom-robot-access
// 使用纯文本类型（text），与企微/飞书保持一致，正文中的 \n 与 emoji 都会原样展示（来自Trae）
func (s *Service) sendDingtalk(webhook, level, title, message string) {
	payload := map[string]any{
		"msgtype": "text",
		"text": map[string]string{
			"content": formatText(level, title, message),
		},
	}
	s.postWebhook("dingtalk", webhook, payload)
}

// sendFeishu 推送到飞书群机器人，来自Trae
// 文档：https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/bot-v2/add-custom-bot
func (s *Service) sendFeishu(webhook, level, title, message string) {
	payload := map[string]any{
		"msg_type": "text",
		"content": map[string]string{
			"text": formatText(level, title, message),
		},
	}
	s.postWebhook("feishu", webhook, payload)
}

// formatText 组合通知正文（来自Trae）。
// 优先保留正文自带的 ✅/❌/⚠️ 结果前缀（drama/strm 发布时已加）；
// 仅当正文没有结果前缀时才用 levelEmoji 补一个，避免群机器人收到"✅ ✅xxx"的双 emoji。
func formatText(level, title, message string) string {
	title = strings.TrimSpace(title)
	message = strings.TrimSpace(message)
	if hasResultEmoji(message) {
		if title == "" {
			return message
		}
		if message == "" {
			return title
		}
		return title + "\n" + message
	}
	prefix := levelEmoji(level)
	if title == "" && message == "" {
		return prefix
	}
	if title == "" {
		return prefix + " " + message
	}
	if message == "" {
		return prefix + " " + title
	}
	return prefix + " " + title + "\n" + message
}

// hasResultEmoji 判断消息是否已带结果类 emoji 前缀（来自Trae）
func hasResultEmoji(s string) bool {
	t := strings.TrimSpace(s)
	for _, e := range []string{"✅", "❌", "⚠️", "⚠", "ℹ️", "ℹ"} {
		if strings.HasPrefix(t, e) {
			return true
		}
	}
	return false
}

// postWebhook 统一的 HTTP POST 推送，带 10 秒超时，来自Trae
// 推送结果只记日志：后台转发不能因为 webhook 故障影响通知主流程
func (s *Service) postWebhook(channel, webhook string, payload any) {
	if err := s.doWebhook(context.Background(), channel, webhook, payload); err != nil {
		s.log.Warn("notify webhook send failed", "channel", channel, "err", err)
	}
}

// doWebhook 同步推送并返回详细错误：HTTP 状态、渠道业务返回码（errcode/code）都会检查，
// 供设置页"发送测试消息"向用户展示失败原因。
func (s *Service) doWebhook(ctx context.Context, channel, webhook string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败（超时或网络不可达）: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateBody(data))
	}
	if err := checkChannelResp(channel, data); err != nil {
		return err
	}
	return nil
}

// checkChannelResp 校验渠道业务返回码。
// 企微/钉钉失败时也返回 HTTP 200，只看状态码会误判成功，因此必须解析响应体：
//   - 企微/钉钉: {"errcode":0,"errmsg":"ok"}
//   - 飞书: {"code":0,"msg":"success"}（新版本为 {"StatusCode":0,"StatusMessage":"success"}）
func checkChannelResp(channel string, data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var resp struct {
		Errcode       int    `json:"errcode"`
		Errmsg        string `json:"errmsg"`
		Code          int    `json:"code"`
		Msg           string `json:"msg"`
		StatusCode    int    `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		// 响应不是 JSON（反代/错误页），无法确认成功，按失败处理并附上响应片段
		return fmt.Errorf("响应不是有效 JSON: %s", truncateBody(data))
	}
	switch channel {
	case "feishu":
		if resp.Code != 0 {
			return fmt.Errorf("飞书返回错误 code=%d: %s", resp.Code, resp.Msg)
		}
		if resp.StatusCode != 0 {
			return fmt.Errorf("飞书返回错误 code=%d: %s", resp.StatusCode, resp.StatusMessage)
		}
	default:
		if resp.Errcode != 0 {
			return fmt.Errorf("errcode=%d: %s", resp.Errcode, resp.Errmsg)
		}
	}
	return nil
}

// truncateBody 截断响应体用于错误信息展示，避免超长 HTML 把 toast 撑爆
func truncateBody(data []byte) string {
	const max = 200
	text := strings.TrimSpace(string(data))
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, text)
	if len(text) > max {
		return text[:max] + "…"
	}
	return text
}

// SendTest 向指定渠道发送一条测试消息，同步等待结果并把失败原因返回给调用方，
// 供设置页"发送测试消息"按钮使用。不写通知存储，不影响未读数。
// webhook 取当前表单值而非已保存设置，便于用户在保存前验证地址有效性。
func (s *Service) SendTest(ctx context.Context, channel, webhook string) error {
	if s == nil {
		return domain.Errorf(domain.CodeInternal, "通知服务未就绪")
	}
	title := "通知渠道测试"
	channelName := map[string]string{"wecom": "企业微信", "dingtalk": "钉钉", "feishu": "飞书"}[channel]
	if channelName == "" {
		return domain.Errorf(domain.CodeValidation, "不支持的通知渠道: %s", channel)
	}
	webhook = strings.TrimSpace(webhook)
	if webhook == "" {
		return domain.Errorf(domain.CodeValidation, "Webhook 地址为空，请先填写")
	}
	if !strings.HasPrefix(webhook, "http://") && !strings.HasPrefix(webhook, "https://") {
		return domain.Errorf(domain.CodeValidation, "Webhook 地址必须以 http:// 或 https:// 开头")
	}
	message := fmt.Sprintf("收到本消息即表示 LitePan %s 机器人推送正常。（%s）",
		channelName, time.Now().Format("2006-01-02 15:04:05"))
	var payload any
	switch channel {
	case "wecom":
		payload = map[string]any{"msgtype": "text", "text": map[string]string{"content": formatText("info", title, message)}}
	case "dingtalk":
		payload = map[string]any{"msgtype": "text", "text": map[string]string{"content": formatText("info", title, message)}}
	case "feishu":
		payload = map[string]any{"msg_type": "text", "content": map[string]string{"text": formatText("info", title, message)}}
	}
	if err := s.doWebhook(ctx, channel, webhook, payload); err != nil {
		// 用 DRIVER_ERROR（上游服务异常）承载详情，前端 toast 能看到具体失败原因
		return domain.Errorf(domain.CodeDriverError, "%v", err)
	}
	return nil
}

func (s *Service) accountName(ctx context.Context, accountID int64) string {
	if s.accounts == nil || accountID <= 0 {
		return ""
	}
	acc, err := s.accounts.Get(ctx, accountID)
	if err != nil || acc == nil {
		return ""
	}
	return acc.Name
}

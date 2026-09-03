package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
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
}

func NewService(opts Options) *Service {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: opts.Repo, accounts: opts.Accounts, settings: opts.Settings, log: log}
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
	return s.repo.MarkRead(ctx, id)
}

func (s *Service) MarkAllRead(ctx context.Context) (int64, error) {
	if s.repo == nil {
		return 0, domain.Errorf(domain.CodeInternal, "通知仓储未就绪")
	}
	return s.repo.MarkAllRead(ctx)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	if s.repo == nil {
		return domain.Errorf(domain.CodeInternal, "通知仓储未就绪")
	}
	return s.repo.Delete(ctx, id)
}

func (s *Service) DeleteAll(ctx context.Context) (int64, error) {
	if s.repo == nil {
		return 0, domain.Errorf(domain.CodeInternal, "通知仓储未就绪")
	}
	return s.repo.DeleteAll(ctx)
}

func (s *Service) DeleteByRef(ctx context.Context, category string, refID int64) (int64, error) {
	if s.repo == nil {
		return 0, domain.Errorf(domain.CodeInternal, "通知仓储未就绪")
	}
	return s.repo.DeleteByRef(ctx, category, refID)
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
		s.log.Warn("persist notification failed", "title", title, "err", err)
	}
	// 推送到已配置的外部通知渠道（企微/钉钉/飞书），来自Trae
	// 仅对任务执行类通知转发，避免认证类通知刷屏；使用独立 context 避免任务 cancel 影响推送
	s.sendWebhook(level, category, title, message)
}

// sendWebhook 根据全局设置调用已配置的机器人 webhook，来自Trae
// 任意渠道配置非空即推送；推送异步执行，不阻塞通知持久化主流程
func (s *Service) sendWebhook(level, category, title, message string) {
	if s.settings == nil {
		return
	}
	// 只转发转存(drama)/STRM(strm*)/缓存(cache)类任务通知，避免认证等系统通知刷屏，来自Trae
	if !isTaskCategory(category) {
		return
	}
	wecomHook := s.settings.String(settings.KeyNotifyWecomWebhook)
	dingtalkHook := s.settings.String(settings.KeyNotifyDingtalkWebhook)
	feishuHook := s.settings.String(settings.KeyNotifyFeishuWebhook)
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
func (s *Service) sendWecom(webhook, level, title, message string) {
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": fmt.Sprintf("%s **%s**\n%s", levelEmoji(level), title, message),
		},
	}
	s.postWebhook("wecom", webhook, payload)
}

// sendDingtalk 推送到钉钉群机器人，来自Trae
// 文档：https://open.dingtalk.com/document/robots/custom-robot-access
func (s *Service) sendDingtalk(webhook, level, title, message string) {
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": title,
			"text":  fmt.Sprintf("%s **%s**\n\n%s", levelEmoji(level), title, message),
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
			"text": fmt.Sprintf("%s %s\n%s", levelEmoji(level), title, message),
		},
	}
	s.postWebhook("feishu", webhook, payload)
}

// postWebhook 统一的 HTTP POST 推送，带 10 秒超时，来自Trae
func (s *Service) postWebhook(channel, webhook string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		s.log.Warn("notify webhook marshal failed", "channel", channel, "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		s.log.Warn("notify webhook request build failed", "channel", channel, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.log.Warn("notify webhook send failed", "channel", channel, "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		s.log.Warn("notify webhook non-2xx", "channel", channel, "status", resp.StatusCode)
	}
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

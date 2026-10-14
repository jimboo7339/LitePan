package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"litepan/internal/domain"
)

type fakeRepo struct {
	mu     sync.Mutex
	items  map[int64]*domain.Notification
	nextID int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{items: map[int64]*domain.Notification{}}
}

func (r *fakeRepo) Create(_ context.Context, n *domain.Notification) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	n.ID = r.nextID
	clone := *n
	r.items[n.ID] = &clone
	return n.ID, nil
}

func (r *fakeRepo) List(_ context.Context, _, _ int) ([]*domain.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*domain.Notification, 0, len(r.items))
	for _, it := range r.items {
		clone := *it
		out = append(out, &clone)
	}
	return out, nil
}

func (r *fakeRepo) UnreadCount(_ context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, it := range r.items {
		if !it.IsRead {
			count++
		}
	}
	return count, nil
}

func (r *fakeRepo) MarkRead(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if it, ok := r.items[id]; ok {
		it.IsRead = true
	}
	return nil
}

func (r *fakeRepo) MarkAllRead(_ context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, it := range r.items {
		if !it.IsRead {
			it.IsRead = true
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, id)
	return nil
}

func (r *fakeRepo) DeleteAll(_ context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := int64(len(r.items))
	r.items = map[int64]*domain.Notification{}
	return n, nil
}

func (r *fakeRepo) DeleteByRef(_ context.Context, category string, refID int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for id, it := range r.items {
		if it.Category == category && it.RefID == refID {
			delete(r.items, id)
			n++
		}
	}
	return n, nil
}

var _ domain.NotificationRepository = (*fakeRepo)(nil)

// waitCount 等待订阅通道推出期望的未读数；中途出现其它值直接跳过（推送只保证最终一致）。
func waitCount(t *testing.T, ch <-chan int, want int) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case got, ok := <-ch:
			if !ok {
				t.Fatalf("订阅通道已关闭，未等到未读数 %d", want)
			}
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("超时未收到未读数 %d", want)
		}
	}
}

func TestNotifyPublishesUnreadCount(t *testing.T) {
	svc := NewService(Options{Repo: newFakeRepo()})
	ch, unsubscribe := svc.Subscribe()
	defer unsubscribe()

	svc.Notify(context.Background(), "info", "system", "标题", "内容", 0, 0)
	waitCount(t, ch, 1)

	svc.Notify(context.Background(), "info", "system", "标题2", "内容", 0, 0)
	waitCount(t, ch, 2)

	if _, err := svc.MarkAllRead(context.Background()); err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}
	waitCount(t, ch, 0)
}

// 已读 / 删除不发 eventbus 事件，必须同样推送未读数，否则多端不会同步。
func TestReadAndDeletePublishUnreadCount(t *testing.T) {
	svc := NewService(Options{Repo: newFakeRepo()})
	ch, unsubscribe := svc.Subscribe()
	defer unsubscribe()

	id, err := svc.repo.Create(context.Background(), &domain.Notification{Level: "info", Category: "system"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.MarkRead(context.Background(), id); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	waitCount(t, ch, 0)

	svc.Notify(context.Background(), "info", "system", "标题", "内容", 0, 0)
	waitCount(t, ch, 1)

	if _, err := svc.DeleteAll(context.Background()); err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	waitCount(t, ch, 0)
}

func TestUnsubscribeClosesChannelAndStopsPush(t *testing.T) {
	svc := NewService(Options{Repo: newFakeRepo()})
	ch, unsubscribe := svc.Subscribe()
	unsubscribe()

	if _, ok := <-ch; ok {
		t.Fatal("退订后通道应已关闭")
	}
	// 退订后继续变更不应 panic，也不应再推送。
	svc.Notify(context.Background(), "info", "system", "标题", "内容", 0, 0)

	// 重复退订必须安全。
	unsubscribe()
}

// 慢客户端不能拖住发布方：通知写入不能被一个不消费的订阅者阻塞。
func TestSlowSubscriberDoesNotBlockPublish(t *testing.T) {
	svc := NewService(Options{Repo: newFakeRepo()})
	_, unsubscribe := svc.Subscribe() // 故意不消费
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			svc.Notify(context.Background(), "info", "system", "标题", "内容", 0, 0)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("慢订阅者拖住了发布方")
	}
}

func TestSlowSubscriberKeepsLatestCount(t *testing.T) {
	svc := NewService(Options{Repo: newFakeRepo()})
	ch, unsubscribe := svc.Subscribe()
	defer unsubscribe()

	// 不消费第一次推送，让缓冲保持占满；第二次推送必须替换旧值。
	svc.Notify(context.Background(), "info", "system", "标题1", "内容", 0, 0)
	svc.Notify(context.Background(), "info", "system", "标题2", "内容", 0, 0)
	waitCount(t, ch, 2)
}

// 并发订阅/退订与推送交错，配合 -race 校验通道关闭与发送不会互相踩踏。
func TestSubscribeUnsubscribeUnderPublish(t *testing.T) {
	svc := NewService(Options{Repo: newFakeRepo()})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			svc.Notify(context.Background(), "info", "system", "标题", "内容", 0, 0)
		}
	}()

	for i := 0; i < 50; i++ {
		ch, unsubscribe := svc.Subscribe()
		go func() {
			for range ch {
			}
		}()
		unsubscribe()
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("并发订阅/退订下发布阻塞")
	}
}

// SendTest 参数校验：未知渠道、空地址、非 http(s) 地址都必须直接拒绝，不发请求。
func TestSendTestValidation(t *testing.T) {
	svc := NewService(Options{Repo: newFakeRepo()})
	cases := []struct {
		name    string
		channel string
		webhook string
		want    string
	}{
		{"未知渠道", "telegram", "https://example.com/hook", "不支持的通知渠道"},
		{"空地址", "wecom", "   ", "Webhook 地址为空"},
		{"非 http 地址", "dingtalk", "ftp://example.com/hook", "必须以 http:// 或 https:// 开头"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.SendTest(context.Background(), tc.channel, tc.webhook)
			if err == nil {
				t.Fatalf("期望报错 %q，实际成功", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误信息 %q 不包含 %q", err.Error(), tc.want)
			}
		})
	}
}

// SendTest 成功路径：HTTP 200 且渠道业务码为 0 才算成功，并校验测试消息内容。
func TestSendTestSuccess(t *testing.T) {
	cases := []struct {
		channel string
		resp    string
	}{
		{"wecom", `{"errcode":0,"errmsg":"ok"}`},
		{"dingtalk", `{"errcode":0,"errmsg":"ok"}`},
		{"feishu", `{"code":0,"msg":"success"}`},
		{"feishu", `{"StatusCode":0,"StatusMessage":"success"}`},
	}
	for _, tc := range cases {
		t.Run(tc.channel+"/"+tc.resp, func(t *testing.T) {
			var gotBody map[string]any
			var gotCT string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotCT = r.Header.Get("Content-Type")
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.resp))
			}))
			defer srv.Close()

			if err := NewService(Options{Repo: newFakeRepo()}).SendTest(context.Background(), tc.channel, srv.URL); err != nil {
				t.Fatalf("期望成功，实际报错: %v", err)
			}
			if !strings.Contains(gotCT, "application/json") {
				t.Fatalf("Content-Type=%q，期望 application/json", gotCT)
			}
			text := flattenText(gotBody)
			if !strings.Contains(text, "通知渠道测试") || !strings.Contains(text, "推送正常") {
				t.Fatalf("测试消息内容不符合预期: %s", text)
			}
		})
	}
}

// SendTest 业务失败路径：渠道返回 HTTP 200 但业务码非 0，必须把业务错误透出。
func TestSendTestBusinessFailure(t *testing.T) {
	cases := []struct {
		channel string
		resp    string
		want    string
	}{
		{"wecom", `{"errcode":93000,"errmsg":"invalid webhook url"}`, "errcode=93000"},
		{"feishu", `{"code":99991663,"msg":"token expired"}`, "code=99991663"},
	}
	for _, tc := range cases {
		t.Run(tc.channel, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.resp))
			}))
			defer srv.Close()

			err := NewService(Options{Repo: newFakeRepo()}).SendTest(context.Background(), tc.channel, srv.URL)
			if err == nil {
				t.Fatalf("期望报错 %q，实际成功", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误信息 %q 不包含 %q", err.Error(), tc.want)
			}
		})
	}
}

// SendTest HTTP 失败路径：非 2xx 与不可达地址都要报错且带原因。
func TestSendTestHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	err := NewService(Options{Repo: newFakeRepo()}).SendTest(context.Background(), "wecom", srv.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("期望包含 HTTP 500 的错误，实际: %v", err)
	}

	// 端口未监听的地址：请求阶段直接失败
	if err := NewService(Options{Repo: newFakeRepo()}).SendTest(context.Background(), "wecom", "http://127.0.0.1:1/hook"); err == nil {
		t.Fatal("期望不可达地址报错，实际成功")
	}
}

// flattenText 从渠道 payload 里取出文本正文（企微/钉钉 text.content、飞书 content.text）
func flattenText(payload map[string]any) string {
	if text, ok := payload["text"].(map[string]any); ok {
		if c, ok := text["content"].(string); ok {
			return c
		}
	}
	if content, ok := payload["content"].(map[string]any); ok {
		if c, ok := content["text"].(string); ok {
			return c
		}
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

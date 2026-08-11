package push

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

func TestWorkWeixinDisabledSendNil(t *testing.T) {
	ch := NewWorkWeixinChannel(model.WorkWeixinConfig{Enabled: false})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("未启用时 Send 应返回 nil, got %v", err)
	}
	if name := ch.Name(); name != "workweixin(disabled)" {
		t.Fatalf("Name() = %q, want workweixin(disabled)", name)
	}
}

func TestWorkWeixinNoWebhookSendNil(t *testing.T) {
	ch := NewWorkWeixinChannel(model.WorkWeixinConfig{Enabled: true})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("无 WebhookURL 时 Send 应返回 nil, got %v", err)
	}
}

// TestWorkWeixinSend 验证请求端点（=配置的 webhook）、请求体 msgtype=markdown 与成功响应解析。
func TestWorkWeixinSend(t *testing.T) {
	var (
		gotPath string
		gotBody map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("请求体不是合法 JSON: %v", err)
		}
		io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer srv.Close()

	ch := NewWorkWeixinChannel(model.WorkWeixinConfig{Enabled: true, WebhookURL: srv.URL + "/cgi-bin/webhook/send?key=abc"})
	if err := ch.Send(context.Background(), "任务标题", "## 报告正文"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	if gotPath != "/cgi-bin/webhook/send" {
		t.Fatalf("请求路径 = %q", gotPath)
	}
	if gotBody["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %v, want markdown", gotBody["msgtype"])
	}
	md, ok := gotBody["markdown"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 markdown 字段: %v", gotBody)
	}
	if md["content"] != "## 报告正文" {
		t.Fatalf("content = %v", md["content"])
	}
}

// TestWorkWeixinTruncate 验证超过 4096 字节的 content 被截断（且不超过上限）。
func TestWorkWeixinTruncate(t *testing.T) {
	var content string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var b workWeixinBody
		json.Unmarshal(body, &b)
		content = b.Markdown.Content
		io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer srv.Close()

	long := strings.Repeat("x", 5000)
	ch := NewWorkWeixinChannel(model.WorkWeixinConfig{Enabled: true, WebhookURL: srv.URL})
	if err := ch.Send(context.Background(), "t", long); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	if len(content) > workWeixinMaxBytes {
		t.Fatalf("content 长度 = %d, 超过上限 %d", len(content), workWeixinMaxBytes)
	}
	if !strings.HasSuffix(content, "...") {
		t.Fatalf("截断消息应以 ... 结尾: %q", content[len(content)-10:])
	}
}

// TestWorkWeixinError 验证业务错误码返回 error。
func TestWorkWeixinError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"errcode":93000,"errmsg":"invalid webhook"}`)
	}))
	defer srv.Close()

	ch := NewWorkWeixinChannel(model.WorkWeixinConfig{Enabled: true, WebhookURL: srv.URL})
	err := ch.Send(context.Background(), "t", "m")
	if err == nil {
		t.Fatal("errcode != 0 应返回错误")
	}
	if !strings.Contains(err.Error(), "93000") {
		t.Fatalf("错误信息应包含 errcode: %v", err)
	}
}

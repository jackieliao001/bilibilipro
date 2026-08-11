package push

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

func TestDingTalkDisabledSendNil(t *testing.T) {
	ch := NewDingTalkChannel(model.DingTalkConfig{Enabled: false})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("未启用时 Send 应返回 nil, got %v", err)
	}
	if name := ch.Name(); name != "dingtalk(disabled)" {
		t.Fatalf("Name() = %q, want dingtalk(disabled)", name)
	}
}

func TestDingTalkNoWebhookSendNil(t *testing.T) {
	ch := NewDingTalkChannel(model.DingTalkConfig{Enabled: true, WebhookURL: ""})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("无 webhook 时 Send 应返回 nil, got %v", err)
	}
}

// TestDingTalkSignedSend 验证：加签参数（timestamp/sign）、请求体 msgtype=markdown、
// sign 为 HMAC-SHA256 的 base64（解码后 32 字节）。
func TestDingTalkSignedSend(t *testing.T) {
	var (
		gotQuery url.Values
		gotBody  map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("请求体不是合法 JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer srv.Close()

	ch := NewDingTalkChannel(model.DingTalkConfig{
		Enabled:    true,
		WebhookURL: srv.URL,
		Secret:     "SECRET-KEY",
	})
	if err := ch.Send(context.Background(), "任务标题", "## 报告正文"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	// 加签参数
	if gotQuery.Get("timestamp") == "" {
		t.Fatal("缺少 timestamp 参数")
	}
	sign := gotQuery.Get("sign")
	if sign == "" {
		t.Fatal("缺少 sign 参数")
	}
	raw, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		t.Fatalf("sign 不是合法 base64: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("sign 解码长度 = %d, want 32（HMAC-SHA256 输出）", len(raw))
	}

	// 请求体
	if gotBody["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %v, want markdown", gotBody["msgtype"])
	}
	md, ok := gotBody["markdown"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 markdown 字段: %v", gotBody)
	}
	if md["title"] != "任务标题" || md["text"] != "## 报告正文" {
		t.Fatalf("markdown 内容错误: %v", md)
	}
}

// TestDingTalkWebhookWithQuery 验证 webhook 本身带 query 时用 & 拼接签名参数。
func TestDingTalkWebhookWithQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "access_token=abc") {
			t.Errorf("原始 query 丢失: %q", r.URL.RawQuery)
		}
		if r.URL.Query().Get("sign") == "" {
			t.Errorf("缺少 sign: %q", r.URL.RawQuery)
		}
		io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer srv.Close()

	ch := NewDingTalkChannel(model.DingTalkConfig{
		Enabled:    true,
		WebhookURL: srv.URL + "?access_token=abc",
		Secret:     "SEC",
	})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
}

// TestDingTalkErrCode 验证业务错误码返回 error（内部有一次 2s 间隔的重试）。
func TestDingTalkErrCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"errcode":310000,"errmsg":"invalid sign"}`)
	}))
	defer srv.Close()

	ch := NewDingTalkChannel(model.DingTalkConfig{
		Enabled:    true,
		WebhookURL: srv.URL,
	})
	err := ch.Send(context.Background(), "t", "m")
	if err == nil {
		t.Fatal("errcode != 0 应返回错误")
	}
	if !strings.Contains(err.Error(), "310000") {
		t.Fatalf("错误信息应包含 errcode: %v", err)
	}
}

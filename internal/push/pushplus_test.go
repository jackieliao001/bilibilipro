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

func TestPushPlusDisabledSendNil(t *testing.T) {
	ch := NewPushPlusChannel(model.PushPlusConfig{Enabled: false})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("未启用时 Send 应返回 nil, got %v", err)
	}
	if name := ch.Name(); name != "pushplus(disabled)" {
		t.Fatalf("Name() = %q, want pushplus(disabled)", name)
	}
}

func TestPushPlusNoTokenSendNil(t *testing.T) {
	ch := NewPushPlusChannel(model.PushPlusConfig{Enabled: true})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("无 Token 时 Send 应返回 nil, got %v", err)
	}
}

// TestPushPlusSend 验证请求体字段（token/title/content/template）与成功响应解析。
func TestPushPlusSend(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("请求体不是合法 JSON: %v", err)
		}
		io.WriteString(w, `{"code":200,"msg":"ok","data":"1"}`)
	}))
	defer srv.Close()

	old := pushPlusSendURL
	pushPlusSendURL = srv.URL
	defer func() { pushPlusSendURL = old }()

	ch := NewPushPlusChannel(model.PushPlusConfig{Enabled: true, Token: "TOKEN123"})
	if err := ch.Send(context.Background(), "任务标题", "## 报告正文"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	if gotBody["token"] != "TOKEN123" || gotBody["title"] != "任务标题" || gotBody["content"] != "## 报告正文" {
		t.Fatalf("请求体字段错误: %v", gotBody)
	}
	if gotBody["template"] != "markdown" {
		t.Fatalf("template = %v, want markdown", gotBody["template"])
	}
	// 未配置的 channel/topic/webhook 应省略
	for _, k := range []string{"channel", "topic", "webhook"} {
		if _, ok := gotBody[k]; ok {
			t.Fatalf("未配置时不应出现字段 %s: %v", k, gotBody)
		}
	}
}

// TestPushPlusSendWithOptional 验证配置 channel/topic/webhook 后请求体包含对应字段。
func TestPushPlusSendWithOptional(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		io.WriteString(w, `{"code":200,"msg":"ok"}`)
	}))
	defer srv.Close()

	old := pushPlusSendURL
	pushPlusSendURL = srv.URL
	defer func() { pushPlusSendURL = old }()

	ch := NewPushPlusChannel(model.PushPlusConfig{
		Enabled: true, Token: "T", Channel: "webhook", Topic: "grp", Webhook: "wh1",
	})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	if gotBody["channel"] != "webhook" || gotBody["topic"] != "grp" || gotBody["webhook"] != "wh1" {
		t.Fatalf("可选字段未透传: %v", gotBody)
	}
}

// TestPushPlusError 验证业务错误码返回 error。
func TestPushPlusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":500,"msg":"token invalid"}`)
	}))
	defer srv.Close()

	old := pushPlusSendURL
	pushPlusSendURL = srv.URL
	defer func() { pushPlusSendURL = old }()

	ch := NewPushPlusChannel(model.PushPlusConfig{Enabled: true, Token: "T"})
	err := ch.Send(context.Background(), "t", "m")
	if err == nil {
		t.Fatal("code != 200 应返回错误")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("错误信息应包含 code: %v", err)
	}
}

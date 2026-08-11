package push

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

func TestCustomAPIDisabledSendNil(t *testing.T) {
	ch := NewCustomAPIChannel(model.CustomAPIConfig{Enabled: false})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("未启用时 Send 应返回 nil, got %v", err)
	}
	if name := ch.Name(); name != "custom_api(disabled)" {
		t.Fatalf("Name() = %q, want custom_api(disabled)", name)
	}
}

func TestCustomAPINoURLSendNil(t *testing.T) {
	ch := NewCustomAPIChannel(model.CustomAPIConfig{Enabled: true})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("无 URL 时 Send 应返回 nil, got %v", err)
	}
}

// TestCustomAPIDefaults 验证默认占位符 "#msg#" 与默认模板：消息以 JSON 字符串（带引号）替换。
func TestCustomAPIDefaults(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, `{"code":0}`)
	}))
	defer srv.Close()

	ch := NewCustomAPIChannel(model.CustomAPIConfig{Enabled: true, URL: srv.URL})
	if err := ch.Send(context.Background(), "t", "hello"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	want := `{"msgtype":"markdown","markdown":{"content":"hello"}}`
	if gotBody != want {
		t.Fatalf("请求体 = %s, want %s", gotBody, want)
	}
}

// TestCustomAPICustomPlaceholder 验证自定义占位符与模板替换。
func TestCustomAPICustomPlaceholder(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	ch := NewCustomAPIChannel(model.CustomAPIConfig{
		Enabled: true, URL: srv.URL,
		Placeholder: "#MSG#", BodyJSONTemplate: `{"content":#MSG#,"title":"x"}`,
	})
	if err := ch.Send(context.Background(), "t", "hi"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	if gotBody != `{"content":"hi","title":"x"}` {
		t.Fatalf("请求体 = %s", gotBody)
	}
}

// TestCustomAPIJSONEscape 验证消息中的引号/特殊字符按 JSON 规则转义（不破坏模板 JSON）。
func TestCustomAPIJSONEscape(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	ch := NewCustomAPIChannel(model.CustomAPIConfig{
		Enabled: true, URL: srv.URL,
		BodyJSONTemplate: `{"content":#msg#}`,
	})
	msg := `say "hi" and \ backslash`
	if err := ch.Send(context.Background(), "t", msg); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	if !strings.Contains(gotBody, `"say \"hi\" and \\ backslash"`) {
		t.Fatalf("JSON 转义错误: %s", gotBody)
	}
}

// TestCustomAPI2xxOK 验证 2xx 且业务响应体非 0 时也视为成功（仅校验 HTTP 状态码）。
func TestCustomAPI2xxOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":1,"msg":"error"}`)
	}))
	defer srv.Close()

	ch := NewCustomAPIChannel(model.CustomAPIConfig{Enabled: true, URL: srv.URL})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("2xx 响应应视为成功, got %v", err)
	}
}

// TestCustomAPIHTTPError 验证非 2xx 返回 error。
func TestCustomAPIHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ch := NewCustomAPIChannel(model.CustomAPIConfig{Enabled: true, URL: srv.URL})
	err := ch.Send(context.Background(), "t", "m")
	if err == nil {
		t.Fatal("非 2xx 应返回错误")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("错误信息应包含状态码: %v", err)
	}
}

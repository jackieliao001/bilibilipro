package push

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

func TestTelegramDisabledSendNil(t *testing.T) {
	ch := NewTelegramChannel(model.TelegramConfig{Enabled: false})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("未启用时 Send 应返回 nil, got %v", err)
	}
	if name := ch.Name(); name != "telegram(disabled)" {
		t.Fatalf("Name() = %q, want telegram(disabled)", name)
	}
}

func TestTelegramMissingParamsSendNil(t *testing.T) {
	if err := NewTelegramChannel(model.TelegramConfig{Enabled: true, BotToken: "t"}).Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("缺 ChatID 时 Send 应返回 nil, got %v", err)
	}
	if err := NewTelegramChannel(model.TelegramConfig{Enabled: true, ChatID: "c"}).Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("缺 BotToken 时 Send 应返回 nil, got %v", err)
	}
}

// TestTelegramSend 验证请求端点（/bot{token}/sendMessage）、表单字段与成功响应解析。
func TestTelegramSend(t *testing.T) {
	var (
		gotPath string
		gotForm map[string]string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm 失败: %v", err)
		}
		gotForm = make(map[string]string)
		for k, v := range r.Form {
			gotForm[k] = v[0]
		}
		io.WriteString(w, `{"ok":true,"result":{"message_id":1}}`)
	}))
	defer srv.Close()

	ch := NewTelegramChannel(model.TelegramConfig{
		Enabled: true, BotToken: "123456:ABC-DEF", ChatID: "-100123456",
		APIHost: srv.URL + "/",
	})
	if err := ch.Send(context.Background(), "任务标题", "## 报告正文"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	if gotPath != "/bot123456:ABC-DEF/sendMessage" {
		t.Fatalf("请求路径 = %q", gotPath)
	}
	if gotForm["chat_id"] != "-100123456" {
		t.Fatalf("chat_id = %q", gotForm["chat_id"])
	}
	if gotForm["text"] != "## 报告正文" {
		t.Fatalf("text = %q", gotForm["text"])
	}
	if gotForm["parse_mode"] != "HTML" {
		t.Fatalf("parse_mode = %q, want HTML", gotForm["parse_mode"])
	}
	if gotForm["disable_web_page_preview"] != "true" {
		t.Fatalf("disable_web_page_preview = %q", gotForm["disable_web_page_preview"])
	}
}

// TestTelegramHTMLEscape 验证 HTML 保留字符（< > &）已转义。
func TestTelegramHTMLEscape(t *testing.T) {
	var text string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		text = r.Form.Get("text")
		io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	ch := NewTelegramChannel(model.TelegramConfig{
		Enabled: true, BotToken: "B", ChatID: "C", APIHost: srv.URL,
	})
	if err := ch.Send(context.Background(), "t", "a < b > c & d"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	if text != "a &lt; b &gt; c &amp; d" {
		t.Fatalf("text = %q, want 已转义的 HTML", text)
	}
}

// TestTelegramError 验证业务失败（ok=false）返回 error。
func TestTelegramError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`)
	}))
	defer srv.Close()

	ch := NewTelegramChannel(model.TelegramConfig{
		Enabled: true, BotToken: "B", ChatID: "C", APIHost: srv.URL,
	})
	err := ch.Send(context.Background(), "t", "m")
	if err == nil {
		t.Fatal("ok=false 应返回错误")
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("错误信息应包含 description: %v", err)
	}
}

// TestParseTelegramProxy 验证代理字符串解析。
func TestParseTelegramProxy(t *testing.T) {
	// 无 scheme：user:password@host:port
	u, err := parseTelegramProxy("user:password@host:8080")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if u.Scheme != "http" {
		t.Fatalf("scheme = %q, want http", u.Scheme)
	}
	if u.Host != "host:8080" {
		t.Fatalf("host = %q", u.Host)
	}
	if usr := u.User; usr == nil || usr.Username() != "user" {
		t.Fatalf("user 解析错误: %v", usr)
	} else if pw, _ := usr.Password(); pw != "password" {
		t.Fatalf("password 解析错误: %q", pw)
	}

	// 带 scheme
	u2, err := parseTelegramProxy("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("带 scheme 解析失败: %v", err)
	}
	if u2.Host != "127.0.0.1:7890" {
		t.Fatalf("host = %q", u2.Host)
	}
	if u2.User != nil {
		t.Fatalf("不应有 userinfo: %v", u2.User)
	}

	// socks5 允许
	if _, err := parseTelegramProxy("socks5://127.0.0.1:1080"); err != nil {
		t.Fatalf("socks5 应支持: %v", err)
	}

	// 空串 → nil
	if u, err := parseTelegramProxy(""); err != nil || u != nil {
		t.Fatalf("空串应返回 (nil, nil), got (%v, %v)", u, err)
	}

	// 不支持的 scheme / 缺 host → 错误
	if _, err := parseTelegramProxy("ftp://host:21"); err == nil {
		t.Fatal("ftp scheme 应报错")
	}
	if _, err := parseTelegramProxy("http://"); err == nil {
		t.Fatal("缺 host 应报错")
	}
}

// TestTelegramInvalidProxySendError 验证配置了非法代理时 Send 直接报错（不发请求）。
func TestTelegramInvalidProxySendError(t *testing.T) {
	ch := NewTelegramChannel(model.TelegramConfig{
		Enabled: true, BotToken: "B", ChatID: "C", Proxy: "ftp://bad", APIHost: "http://127.0.0.1:1",
	})
	if err := ch.Send(context.Background(), "t", "m"); err == nil {
		t.Fatal("非法代理应返回错误")
	}
}

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

func TestServerChanDisabledSendNil(t *testing.T) {
	ch := NewServerChanChannel(model.ServerChanConfig{Enabled: false})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("未启用时 Send 应返回 nil, got %v", err)
	}
	if name := ch.Name(); name != "serverchan(disabled)" {
		t.Fatalf("Name() = %q, want serverchan(disabled)", name)
	}
}

func TestServerChanNoKeySendNil(t *testing.T) {
	ch := NewServerChanChannel(model.ServerChanConfig{Enabled: true})
	if err := ch.Send(context.Background(), "t", "m"); err != nil {
		t.Fatalf("无 SendKey 时 Send 应返回 nil, got %v", err)
	}
}

// TestServerChanSend 验证请求端点（/{key}.send）、表单字段 title/desp 与成功响应解析。
func TestServerChanSend(t *testing.T) {
	var (
		gotPath string
		gotCT   string
		gotForm map[string]string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm 失败: %v", err)
		}
		gotForm = make(map[string]string)
		for k, v := range r.Form {
			gotForm[k] = v[0]
		}
		io.WriteString(w, `{"code":0,"message":"","data":{}}`)
	}))
	defer srv.Close()

	old := serverChanSendURL
	serverChanSendURL = srv.URL + "/%s.send"
	defer func() { serverChanSendURL = old }()

	ch := NewServerChanChannel(model.ServerChanConfig{Enabled: true, SendKey: "SCT123456"})
	if err := ch.Send(context.Background(), "任务标题", "## 报告正文"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	if gotPath != "/SCT123456.send" {
		t.Fatalf("请求路径 = %q, want /SCT123456.send", gotPath)
	}
	if !strings.Contains(gotCT, "application/x-www-form-urlencoded") {
		t.Fatalf("Content-Type = %q, want form-urlencoded", gotCT)
	}
	if gotForm["title"] != "任务标题" {
		t.Fatalf("title = %q", gotForm["title"])
	}
	if gotForm["desp"] != "## 报告正文" {
		t.Fatalf("desp = %q", gotForm["desp"])
	}
}

// TestServerChanError 验证业务错误码返回 error（内部有一次 2s 间隔的重试）。
func TestServerChanError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":40001,"message":"bad key","data":null}`)
	}))
	defer srv.Close()

	old := serverChanSendURL
	serverChanSendURL = srv.URL + "/%s.send"
	defer func() { serverChanSendURL = old }()

	ch := NewServerChanChannel(model.ServerChanConfig{Enabled: true, SendKey: "SCT"})
	err := ch.Send(context.Background(), "t", "m")
	if err == nil {
		t.Fatal("code != 0 应返回错误")
	}
	if !strings.Contains(err.Error(), "40001") {
		t.Fatalf("错误信息应包含 code: %v", err)
	}
}

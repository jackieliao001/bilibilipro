package push

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// serverChanSendURL Server酱 Turbo 发送接口（包级变量，测试可覆盖）。
var serverChanSendURL = "https://sctapi.ftqq.com/%s.send"

// ServerChanChannel Server酱（Turbo 版）推送渠道。
type ServerChanChannel struct {
	cfg model.ServerChanConfig
}

// NewServerChanChannel 创建 Server酱推送渠道。
func NewServerChanChannel(cfg model.ServerChanConfig) *ServerChanChannel {
	return &ServerChanChannel{cfg: cfg}
}

// Name 返回渠道名称；未启用或未配置 SendKey 时返回 "serverchan(disabled)"。
func (s *ServerChanChannel) Name() string {
	if s == nil || !s.cfg.Enabled || s.cfg.SendKey == "" {
		return "serverchan(disabled)"
	}
	return "serverchan"
}

// Send 通过 Server酱 Turbo 接口推送消息（title + markdown 正文 desp）。
// 未启用 / 未配置 SendKey 时直接返回 nil；发送失败重试一次（间隔 2s）。
// desp 超过 32KB 时截断。
func (s *ServerChanChannel) Send(ctx context.Context, title, message string) error {
	if s == nil || !s.cfg.Enabled || s.cfg.SendKey == "" {
		return nil
	}

	form := url.Values{}
	form.Set("title", title)
	form.Set("desp", truncateMessage(message, serverChanMaxBytes))

	endpoint := fmt.Sprintf(serverChanSendURL, s.cfg.SendKey)
	client := &http.Client{Timeout: 10 * time.Second}
	return doPost(ctx, client, endpoint, "application/x-www-form-urlencoded", []byte(form.Encode()), func(status int, data []byte) error {
		if status != http.StatusOK {
			return fmt.Errorf("serverchan http status %d: %s", status, strings.TrimSpace(string(data)))
		}
		var r serverChanResponse
		if err := json.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("parse serverchan response: %w", err)
		}
		if r.Code != 0 {
			return fmt.Errorf("serverchan code=%d message=%s", r.Code, r.Message)
		}
		return nil
	})
}

type serverChanResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

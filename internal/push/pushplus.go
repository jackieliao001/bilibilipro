package push

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// pushPlusSendURL PushPlus 发送接口（包级变量，测试可覆盖）。
var pushPlusSendURL = "https://www.pushplus.plus/send"

// PushPlusChannel PushPlus 推送渠道。
type PushPlusChannel struct {
	cfg model.PushPlusConfig
}

// NewPushPlusChannel 创建 PushPlus 推送渠道。
func NewPushPlusChannel(cfg model.PushPlusConfig) *PushPlusChannel {
	return &PushPlusChannel{cfg: cfg}
}

// Name 返回渠道名称；未启用或未配置 Token 时返回 "pushplus(disabled)"。
func (p *PushPlusChannel) Name() string {
	if p == nil || !p.cfg.Enabled || p.cfg.Token == "" {
		return "pushplus(disabled)"
	}
	return "pushplus"
}

// Send 通过 PushPlus 发送 markdown 消息。
// 未启用 / 未配置 Token 时直接返回 nil；发送失败重试一次（间隔 2s）。
// channel/topic/webhook 为空时请求体省略对应字段。
func (p *PushPlusChannel) Send(ctx context.Context, title, message string) error {
	if p == nil || !p.cfg.Enabled || p.cfg.Token == "" {
		return nil
	}

	body := pushPlusBody{
		Token:    p.cfg.Token,
		Title:    title,
		Content:  message,
		Template: "markdown",
		Channel:  p.cfg.Channel,
		Topic:    p.cfg.Topic,
		Webhook:  p.cfg.Webhook,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal pushplus body: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	return doPost(ctx, client, pushPlusSendURL, "application/json", payload, func(status int, data []byte) error {
		if status != http.StatusOK {
			return fmt.Errorf("pushplus http status %d: %s", status, strings.TrimSpace(string(data)))
		}
		var r pushPlusResponse
		if err := json.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("parse pushplus response: %w", err)
		}
		if r.Code != 200 {
			return fmt.Errorf("pushplus code=%d msg=%s", r.Code, r.Msg)
		}
		return nil
	})
}

type pushPlusBody struct {
	Token    string `json:"token"`
	Title    string `json:"title,omitempty"`
	Content  string `json:"content,omitempty"`
	Template string `json:"template,omitempty"`
	Channel  string `json:"channel,omitempty"`
	Topic    string `json:"topic,omitempty"`
	Webhook  string `json:"webhook,omitempty"`
}

type pushPlusResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

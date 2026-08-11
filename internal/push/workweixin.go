package push

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

// WorkWeixinChannel 企业微信群机器人推送渠道。
type WorkWeixinChannel struct {
	cfg model.WorkWeixinConfig
}

// NewWorkWeixinChannel 创建企业微信推送渠道。
func NewWorkWeixinChannel(cfg model.WorkWeixinConfig) *WorkWeixinChannel {
	return &WorkWeixinChannel{cfg: cfg}
}

// Name 返回渠道名称；未启用或未配置 WebhookURL 时返回 "workweixin(disabled)"。
func (w *WorkWeixinChannel) Name() string {
	if w == nil || !w.cfg.Enabled || w.cfg.WebhookURL == "" {
		return "workweixin(disabled)"
	}
	return "workweixin"
}

// Send 通过企业微信群机器人 Webhook 发送 markdown 消息。
// 未启用 / 未配置 WebhookURL 时直接返回 nil；发送失败重试一次（间隔 2s）。
// content 超过 4096 字节时截断。
func (w *WorkWeixinChannel) Send(ctx context.Context, title, message string) error {
	if w == nil || !w.cfg.Enabled || w.cfg.WebhookURL == "" {
		return nil
	}

	body := workWeixinBody{
		MsgType: "markdown",
		Markdown: workWeixinMarkdown{
			Content: truncateMessage(message, workWeixinMaxBytes),
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal workweixin body: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	return doPost(ctx, client, w.cfg.WebhookURL, "application/json", payload, func(status int, data []byte) error {
		if status != http.StatusOK {
			return fmt.Errorf("workweixin http status %d: %s", status, strings.TrimSpace(string(data)))
		}
		var r workWeixinResponse
		if err := json.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("parse workweixin response: %w", err)
		}
		if r.ErrCode != 0 {
			return fmt.Errorf("workweixin errcode=%d errmsg=%s", r.ErrCode, r.ErrMsg)
		}
		return nil
	})
}

type workWeixinBody struct {
	MsgType  string             `json:"msgtype"`
	Markdown workWeixinMarkdown `json:"markdown"`
}

type workWeixinMarkdown struct {
	Content string `json:"content"`
}

type workWeixinResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

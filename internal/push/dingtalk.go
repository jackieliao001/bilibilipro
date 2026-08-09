package push

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// DingTalkChannel 钉钉机器人推送渠道（支持加签）。
type DingTalkChannel struct {
	cfg model.DingTalkConfig
}

// NewDingTalkChannel 创建钉钉推送渠道。
func NewDingTalkChannel(cfg model.DingTalkConfig) *DingTalkChannel {
	return &DingTalkChannel{cfg: cfg}
}

// Name 返回渠道名称；未启用或未配置 Webhook 时返回 "dingtalk(disabled)"。
func (d *DingTalkChannel) Name() string {
	if d == nil || !d.cfg.Enabled || d.cfg.WebhookURL == "" {
		return "dingtalk(disabled)"
	}
	return "dingtalk"
}

// Send 发送 markdown 消息到钉钉机器人。
// 未启用 / 未配置 Webhook 时直接返回 nil。
// 加签模式（Secret 非空）：timestamp + HMAC-SHA256 签名后追加到 URL。
// 发送失败重试一次（间隔 2s）。
func (d *DingTalkChannel) Send(ctx context.Context, title, message string) error {
	if d == nil || !d.cfg.Enabled || d.cfg.WebhookURL == "" {
		return nil
	}

	endpoint := d.cfg.WebhookURL
	if d.cfg.Secret != "" {
		ts := time.Now().UnixMilli()
		stringToSign := fmt.Sprintf("%d\n%s", ts, d.cfg.Secret)
		mac := hmac.New(sha256.New, []byte(d.cfg.Secret))
		mac.Write([]byte(stringToSign))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		sep := "?"
		if strings.Contains(endpoint, "?") {
			sep = "&"
		}
		endpoint = fmt.Sprintf("%s%stimestamp=%d&sign=%s", endpoint, sep, ts, url.QueryEscape(sign))
	}

	body := dingtalkBody{
		MsgType: "markdown",
		Markdown: dingtalkMarkdown{
			Title: title,
			Text:  message,
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal dingtalk body: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
		if err != nil {
			return fmt.Errorf("create dingtalk request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("dingtalk request failed: %w", err)
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read dingtalk response: %w", readErr)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("dingtalk http status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
			continue
		}

		var r dingtalkResponse
		if err := json.Unmarshal(data, &r); err != nil {
			lastErr = fmt.Errorf("parse dingtalk response: %w", err)
			continue
		}
		if r.ErrCode != 0 {
			lastErr = fmt.Errorf("dingtalk errcode=%d errmsg=%s", r.ErrCode, r.ErrMsg)
			continue
		}
		return nil
	}
	return lastErr
}

type dingtalkBody struct {
	MsgType  string           `json:"msgtype"`
	Markdown dingtalkMarkdown `json:"markdown"`
}

type dingtalkMarkdown struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

type dingtalkResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

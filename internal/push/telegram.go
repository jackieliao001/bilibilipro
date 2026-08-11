package push

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

// defaultTelegramAPIHost Telegram 官方 Bot API 地址。
const defaultTelegramAPIHost = "https://api.telegram.org"

// TelegramChannel Telegram Bot 推送渠道（parse_mode=HTML）。
type TelegramChannel struct {
	cfg model.TelegramConfig
}

// NewTelegramChannel 创建 Telegram 推送渠道。
func NewTelegramChannel(cfg model.TelegramConfig) *TelegramChannel {
	return &TelegramChannel{cfg: cfg}
}

// Name 返回渠道名称；未启用或未配置 BotToken/ChatID 时返回 "telegram(disabled)"。
func (t *TelegramChannel) Name() string {
	if t == nil || !t.cfg.Enabled || t.cfg.BotToken == "" || t.cfg.ChatID == "" {
		return "telegram(disabled)"
	}
	return "telegram"
}

// Send 通过 Telegram Bot API 发送消息。
// parse_mode 使用 HTML（仅需转义 < > &，比 MarkdownV2 的复杂转义更稳）。
// 未启用 / 缺少 BotToken 或 ChatID 时直接返回 nil；发送失败重试一次（间隔 2s）。
// text 超过 4096 字符时截断；proxy 配置后走代理发送。
func (t *TelegramChannel) Send(ctx context.Context, title, message string) error {
	if t == nil || !t.cfg.Enabled || t.cfg.BotToken == "" || t.cfg.ChatID == "" {
		return nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	if proxyURL, err := parseTelegramProxy(t.cfg.Proxy); err != nil {
		return fmt.Errorf("parse telegram proxy: %w", err)
	} else if proxyURL != nil {
		client.Transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	}

	apiHost := t.cfg.APIHost
	if apiHost == "" {
		apiHost = defaultTelegramAPIHost
	}
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(apiHost, "/"), t.cfg.BotToken)

	form := url.Values{}
	form.Set("chat_id", t.cfg.ChatID)
	form.Set("text", truncateChars(escapeTelegramHTML(message), telegramMaxChars))
	form.Set("parse_mode", "HTML")
	form.Set("disable_web_page_preview", "true")

	return doPost(ctx, client, endpoint, "application/x-www-form-urlencoded", []byte(form.Encode()), func(status int, data []byte) error {
		if status != http.StatusOK {
			return fmt.Errorf("telegram http status %d: %s", status, strings.TrimSpace(string(data)))
		}
		var r telegramResponse
		if err := json.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("parse telegram response: %w", err)
		}
		if !r.Ok {
			return fmt.Errorf("telegram ok=false description=%s", r.Description)
		}
		return nil
	})
}

// escapeTelegramHTML 转义 HTML parse_mode 下的保留字符（& < >）；& 必须最先替换。
func escapeTelegramHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// parseTelegramProxy 解析代理字符串，如 "user:password@host:port" 或
// "http://user:password@host:port"（未带 scheme 时按 http 处理）。
// 空串返回 (nil, nil)；不支持的 scheme 或缺少 host 返回错误。
func parseTelegramProxy(proxy string) (*url.URL, error) {
	if strings.TrimSpace(proxy) == "" {
		return nil, nil
	}
	raw := proxy
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5":
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("proxy host is empty")
	}
	return u, nil
}

type telegramResponse struct {
	Ok          bool   `json:"ok"`
	Description string `json:"description"`
}

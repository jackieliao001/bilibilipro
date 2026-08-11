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

// 自定义 API 渠道默认占位符与默认请求体模板（与原 C# 项目 OtherApi 一致）。
const (
	defaultCustomAPIPlaceholder = "#msg#"
	defaultCustomAPITemplate    = `{"msgtype":"markdown","markdown":{"content":#msg#}}`
)

// CustomAPIChannel 自定义 API 推送渠道（将消息按模板 POST 到任意接口）。
type CustomAPIChannel struct {
	cfg model.CustomAPIConfig
}

// NewCustomAPIChannel 创建自定义 API 推送渠道。
func NewCustomAPIChannel(cfg model.CustomAPIConfig) *CustomAPIChannel {
	return &CustomAPIChannel{cfg: cfg}
}

// Name 返回渠道名称；未启用或未配置 URL 时返回 "custom_api(disabled)"。
func (c *CustomAPIChannel) Name() string {
	if c == nil || !c.cfg.Enabled || c.cfg.URL == "" {
		return "custom_api(disabled)"
	}
	return "custom_api"
}

// Send 将 body_json_template 中的 placeholder 替换为消息的 JSON 字符串
// （json.Marshal 结果自带引号，模板中占位符不加引号包裹，与原 C# 语义一致），
// 然后 POST 到目标地址。仅校验 HTTP 2xx，不解析业务响应。
// 未启用 / 未配置 URL 时直接返回 nil；发送失败重试一次（间隔 2s）。
func (c *CustomAPIChannel) Send(ctx context.Context, title, message string) error {
	if c == nil || !c.cfg.Enabled || c.cfg.URL == "" {
		return nil
	}

	placeholder := c.cfg.Placeholder
	if placeholder == "" {
		placeholder = defaultCustomAPIPlaceholder
	}
	tmpl := c.cfg.BodyJSONTemplate
	if tmpl == "" {
		tmpl = defaultCustomAPITemplate
	}

	msgJSON, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal custom api message: %w", err)
	}
	payload := []byte(strings.ReplaceAll(tmpl, placeholder, string(msgJSON)))

	client := &http.Client{Timeout: 10 * time.Second}
	return doPost(ctx, client, c.cfg.URL, "application/json", payload, func(status int, data []byte) error {
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			return fmt.Errorf("custom_api http status %d: %s", status, strings.TrimSpace(string(data)))
		}
		return nil
	})
}

package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/raywangqvq/bilitoolgo/internal/api"
)

// QRCodeData 扫码登录二维码信息。
type QRCodeData struct {
	URL       string `json:"url"`
	QrcodeKey string `json:"qrcode_key"`
}

// PollResult 扫码状态轮询结果。
// Code 取值：0=成功，86101=未扫码，86090=已扫码未确认，86038=二维码已失效。
type PollResult struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	URL       string `json:"url"`
	CookieStr string `json:"-"` // 从响应 Set-Cookie 提取，登录成功时携带
}

// qrCodeResponse 二维码生成接口响应（该接口 message 字段为数字，需单独定义）。
type qrCodeResponse struct {
	Code    int         `json:"code"`
	Message interface{} `json:"message"`
	Data    QRCodeData  `json:"data"`
}

// GenerateQRCode 获取登录二维码（无需 cookie）。
func (c *BiliClient) GenerateQRCode(ctx context.Context) (*QRCodeData, error) {
	const qrURL = "http://passport.bilibili.com/x/passport-login/web/qrcode/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qrURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Referer", refererBili)

	resp, err := api.RetryDo(c.http, req, maxRetries)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bili http error: status=%s", resp.Status)
	}
	var out qrCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("bili api error: code=%d, message=%v", out.Code, out.Message)
	}
	return &out.Data, nil
}

// PollQRCode 轮询二维码扫描状态，并把响应 Set-Cookie 提取到 PollResult.CookieStr。
func (c *BiliClient) PollQRCode(ctx context.Context, qrcodeKey string) (*PollResult, error) {
	pollURL := "http://passport.bilibili.com/x/passport-login/web/qrcode/poll?qrcode_key=" +
		url.QueryEscape(qrcodeKey) + "&source=main_mini"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Referer", refererBili)

	resp, err := api.RetryDo(c.http, req, maxRetries)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bili http error: status=%s", resp.Status)
	}
	var out struct {
		Code    int         `json:"code"`
		Message interface{} `json:"message"`
		Data    PollResult  `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("bili api error: code=%d, message=%v", out.Code, out.Message)
	}
	// 提取 Set-Cookie
	parts := make([]string, 0, len(resp.Cookies()))
	for _, ck := range resp.Cookies() {
		parts = append(parts, ck.Name+"="+ck.Value)
	}
	out.Data.CookieStr = strings.Join(parts, "; ")
	return &out.Data, nil
}

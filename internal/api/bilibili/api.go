// Package bilibili 封装 B 站各业务 API 端点，提供泛型请求辅助与 WBI 签名支持。
package bilibili

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackieliao001/bilibilipro/internal/api"
	"github.com/jackieliao001/bilibilipro/internal/model"
)

const (
	// defaultUserAgent 默认 User-Agent。
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	// refererBili 默认 Referer。
	refererBili = "https://www.bilibili.com/"
	// maxRetries 请求最大重试次数。
	maxRetries = 3
)

// BiliClient B 站 API 客户端。
type BiliClient struct {
	http   *http.Client
	cfg    *model.Config
	logger *slog.Logger
	wbi    *api.WbiService
}

// New 创建 B 站 API 客户端（含 HTTP 客户端与 WBI 签名服务）。
func New(cfg *model.Config, logger *slog.Logger) *BiliClient {
	if logger == nil {
		logger = slog.Default()
	}
	c := &BiliClient{
		http:   api.NewClient(&cfg.Bilibili, logger),
		cfg:    cfg,
		logger: logger,
	}
	c.wbi = api.NewWbiService(c.fetchWbiKeys)
	return c
}

// SetTransportForTest 替换客户端的 HTTP Transport（仅测试用途）。
// 用于注入 roundTripFunc 等 mock transport，避免在测试中发起真实网络请求；
// 业务代码请勿调用。
func SetTransportForTest(c *BiliClient, rt http.RoundTripper) {
	c.http = &http.Client{Transport: rt}
}

// fetchWbiKeys 通过 nav 接口获取 WBI 签名 keys（由 WbiService 回调注入）。
func (c *BiliClient) fetchWbiKeys(ctx context.Context, ck *model.Cookie) (*model.WbiImg, error) {
	resp, err := c.GetUserInfo(ctx, ck)
	if err != nil {
		return nil, err
	}
	return &resp.Data.WbiImg, nil
}

// Get 泛型 GET 辅助：发起 GET 请求并解码为 model.BiliApiResponse[T]。
// wbi 为 true 时对 query 做 WBI 签名（w_rid/wts 追加进 query）；
// ck 为 nil 时不携带 Cookie。code != 0 时返回 (resp, err)，
// err 形如 "bili api error: code=..., message=..."，调用方可检查 resp.Code 区分具体错误码。
// 注意：Go 不支持泛型方法，故以包级函数形式导出（契约中 Get[T] 方法的等价实现）。
func Get[T any](c *BiliClient, ctx context.Context, reqURL string, ck *model.Cookie, wbi bool) (*model.BiliApiResponse[T], error) {
	if wbi {
		u, err := url.Parse(reqURL)
		if err != nil {
			return nil, fmt.Errorf("解析 URL 失败: %w", err)
		}
		params := make(map[string]string, len(u.Query()))
		for k, vs := range u.Query() {
			if len(vs) > 0 {
				params[k] = vs[0]
			}
		}
		if err := c.signWbi(ctx, ck, params); err != nil {
			return nil, err
		}
		q := u.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
		reqURL = u.String()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setHeaders(req, ck)
	return doAndDecode[T](c, req)
}

// PostForm 泛型 POST 表单辅助：发起 POST 请求并解码为 model.BiliApiResponse[T]。
// wbi 为 true 时对表单参数做 WBI 签名（w_rid/wts 追加进 form）。
// 注意：Go 不支持泛型方法，故以包级函数形式导出（契约中 PostForm[T] 方法的等价实现）。
func PostForm[T any](c *BiliClient, ctx context.Context, reqURL string, form url.Values, ck *model.Cookie, wbi bool) (*model.BiliApiResponse[T], error) {
	if wbi {
		params := make(map[string]string, len(form))
		for k, vs := range form {
			if len(vs) > 0 {
				params[k] = vs[0]
			}
		}
		if err := c.signWbi(ctx, ck, params); err != nil {
			return nil, err
		}
		form = make(url.Values, len(params))
		for k, v := range params {
			form.Set(k, v)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req, ck)
	return doAndDecode[T](c, req)
}

// signWbi 对参数做 WBI 签名，并把 w_rid/wts 写回 params。
func (c *BiliClient) signWbi(ctx context.Context, ck *model.Cookie, params map[string]string) error {
	imgKey, subKey, err := c.wbi.GetKeys(ctx, ck)
	if err != nil {
		return err
	}
	wrid, wts := api.EncWbi(params, imgKey, subKey)
	params["w_rid"] = wrid
	params["wts"] = wts
	return nil
}

// setHeaders 设置公共请求头：User-Agent、Referer、Cookie、Accept-Language。
func (c *BiliClient) setHeaders(req *http.Request, ck *model.Cookie) {
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("Referer", refererBili)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if ck != nil {
		if s := ck.String(); s != "" {
			req.Header.Set("Cookie", s)
		}
	}
}

// userAgent 返回配置的 User-Agent，未配置时用默认值。
func (c *BiliClient) userAgent() string {
	if c.cfg.Bilibili.UserAgent != "" {
		return c.cfg.Bilibili.UserAgent
	}
	return defaultUserAgent
}

// doAndDecode 执行请求（带重试）、校验 HTTP 状态并解码 B 站响应；
// code != 0 时返回 (resp, err)，err 包含 code/message。
func doAndDecode[T any](c *BiliClient, req *http.Request) (*model.BiliApiResponse[T], error) {
	resp, err := api.RetryDo(c.http, req, maxRetries)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bili http error: status=%s", resp.Status)
	}
	var out model.BiliApiResponse[T]
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if out.Code != 0 {
		return &out, fmt.Errorf("bili api error: code=%d, message=%s", out.Code, out.Message)
	}
	return &out, nil
}

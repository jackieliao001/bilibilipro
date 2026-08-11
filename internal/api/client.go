// Package api 提供 B 站 HTTP 客户端、请求中间件与 WBI 签名等基础能力，
// 供 internal/api/bilibili 及其他上层包使用。
package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

const (
	// clientTimeout HTTP 客户端整体超时。
	clientTimeout = 30 * time.Second
	// defaultMaxRetries 默认最大重试次数。
	defaultMaxRetries = 3
)

// NewClient 创建 B 站 HTTP 客户端。
// RoundTripper 链由外到内为：IntervalRoundTripper → LogRoundTripper → http.Transport，
// 若配置了代理则 Transport 使用 http.ProxyURL。
// 注意：WBI 签名由 bilibili 包在请求前显式调用 WbiService 处理，不在此处做全局拦截。
func NewClient(cfg *model.BilibiliConfig, logger *slog.Logger) *http.Client {
	if logger == nil {
		logger = slog.Default()
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg != nil && cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err != nil {
			logger.Warn("解析代理地址失败，将使用直连", "proxy", cfg.Proxy, "error", err)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	var rt http.RoundTripper = transport
	rt = NewLogRoundTripper(rt, logger)
	if cfg != nil {
		rt = NewIntervalRoundTripper(rt, cfg.IntervalSeconds)
	}
	return &http.Client{
		Transport: rt,
		Timeout:   clientTimeout,
	}
}

// RetryDo 带指数退避重试地执行一次请求。
// 仅对网络错误、HTTP 429 与 5xx 重试；退避时间为 2^n 秒（1s、2s、4s…）；
// maxRetries <= 0 时使用默认值 3。重试前会通过 req.GetBody 重建请求体。
func RetryDo(client *http.Client, req *http.Request, maxRetries int) (*http.Response, error) {
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	backoff := time.Second
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if req.Body != nil && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("重试前重建请求体失败: %w", err)
			}
			req.Body = body
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < http.StatusInternalServerError {
			return resp, nil
		} else {
			lastErr = fmt.Errorf("http status %s", resp.Status)
			resp.Body.Close()
		}
		if attempt < maxRetries {
			time.Sleep(backoff)
			backoff *= 2
		}
	}
	return nil, fmt.Errorf("请求在重试 %d 次后仍失败: %w", maxRetries, lastErr)
}

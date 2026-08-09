package api

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// logRoundTripper 请求/响应日志中间件。
type logRoundTripper struct {
	next   http.RoundTripper
	logger *slog.Logger
}

// NewLogRoundTripper 包装 next，在 Debug 级别记录请求方法/URL/请求体与响应体；
// 响应体读取后会恢复，不影响后续读取。
func NewLogRoundTripper(next http.RoundTripper, logger *slog.Logger) http.RoundTripper {
	if logger == nil {
		logger = slog.Default()
	}
	return &logRoundTripper{next: next, logger: logger}
}

func (l *logRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var reqBody string
	if req.Body != nil {
		if b, err := io.ReadAll(req.Body); err == nil {
			reqBody = string(b)
			req.Body = io.NopCloser(bytes.NewReader(b))
		}
	}
	l.logger.Debug("http 请求", "method", req.Method, "url", req.URL.String(), "body", reqBody)

	resp, err := l.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(b))
	l.logger.Debug("http 响应", "status", resp.Status, "body", string(b))
	return resp, nil
}

// intervalRoundTripper 请求间隔控制中间件。
type intervalRoundTripper struct {
	next     http.RoundTripper
	interval time.Duration // 允许的最大间隔
	mu       sync.Mutex
	last     time.Time // 上次请求发出的时间
}

// NewIntervalRoundTripper 包装 next，控制相邻请求的最小间隔（并发安全）：
// 每次请求的目标间隔为 [interval/2, interval] 内的随机值，距上次请求不足则 sleep 补足；
// intervalSeconds <= 0 时直接透传。
func NewIntervalRoundTripper(next http.RoundTripper, intervalSeconds int) http.RoundTripper {
	if intervalSeconds <= 0 {
		return next
	}
	return &intervalRoundTripper{
		next:     next,
		interval: time.Duration(intervalSeconds) * time.Second,
	}
}

func (i *intervalRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	i.mu.Lock()
	now := time.Now()
	half := i.interval / 2
	target := half + time.Duration(rand.Int63n(int64(half)+1))
	var wait time.Duration
	if !i.last.IsZero() {
		if d := now.Sub(i.last); d < target {
			wait = target - d
		}
	}
	i.last = now.Add(wait)
	i.mu.Unlock()

	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
	return i.next.RoundTrip(req)
}

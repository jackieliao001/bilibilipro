package push

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// doPost 发送一次 POST 请求。
// 网络错误、响应读取失败或 check 返回错误时重试一次（间隔 2s，可被 ctx 取消），
// 与钉钉渠道的重试模式一致。check 在每次响应后调用，用于校验 HTTP 状态码与业务响应码。
func doPost(ctx context.Context, client *http.Client, endpoint, contentType string, payload []byte, check func(statusCode int, body []byte) error) error {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", contentType)

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response: %w", readErr)
			continue
		}
		if check != nil {
			if err := check(resp.StatusCode, data); err != nil {
				lastErr = err
				continue
			}
		}
		return nil
	}
	return lastErr
}

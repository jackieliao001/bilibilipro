package bilibili

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackieliao001/bilibilipro/internal/api"
	"github.com/jackieliao001/bilibilipro/internal/model"
)

// GetHomePage 访问 B 站首页，返回 Set-Cookie 响应头列表（用于补全 buvid3 等设备 Cookie）。
// 该方法不使用泛型响应解码，因为需要读取原始响应头而非业务 JSON。
func (c *BiliClient) GetHomePage(ctx context.Context, ck *model.Cookie) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, refererBili, nil)
	if err != nil {
		return nil, fmt.Errorf("创建首页请求失败: %w", err)
	}
	c.setHeaders(req, ck)

	resp, err := api.RetryDo(c.http, req, maxRetries)
	if err != nil {
		return nil, fmt.Errorf("访问首页失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bili http error: status=%s", resp.Status)
	}
	return resp.Header.Values("Set-Cookie"), nil
}

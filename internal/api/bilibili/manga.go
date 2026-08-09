package bilibili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/raywangqvq/bilitoolgo/internal/api"
	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// mangaOrigin 漫画站接口的 Origin/Referer。
const mangaOrigin = "https://manga.bilibili.com"

// mangaPlatform 漫画接口 platform 参数（原版 DailyTaskConfig.DevicePlatform 默认 "android"）。
const mangaPlatform = "android"

// ErrMangaAlreadySignedIn 漫画重复签到错误：B 站返回 HTTP 400（非业务错误码），
// 视为"今日已签到"，调用方可按跳过处理。
var ErrMangaAlreadySignedIn = errors.New("manga already signed in (http 400)")

// MangaClockIn 漫画签到。重复签到返回 ErrMangaAlreadySignedIn。
func (c *BiliClient) MangaClockIn(ctx context.Context, ck *model.Cookie) error {
	return c.mangaPost(ctx, ck, "https://manga.bilibili.com/twirp/activity.v1.Activity/ClockIn?platform="+mangaPlatform, true)
}

// MangaAddHistory 漫画阅读打卡（添加阅读历史）。
func (c *BiliClient) MangaAddHistory(ctx context.Context, ck *model.Cookie, comicID, epID int64) error {
	u := fmt.Sprintf("https://manga.bilibili.com/twirp/bookshelf.v1.Bookshelf/AddHistory?platform=%s&comic_id=%d&ep_id=%d", mangaPlatform, comicID, epID)
	return c.mangaPost(ctx, ck, u, false)
}

// mangaPost 发起漫画站 POST 请求（空 body）并校验响应。
// http400IsSignIn 为 true 时，HTTP 400 视为重复签到，返回 ErrMangaAlreadySignedIn。
func (c *BiliClient) mangaPost(ctx context.Context, ck *model.Cookie, reqURL string, http400IsSignIn bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	c.setHeaders(req, ck)
	req.Header.Set("Origin", mangaOrigin)
	req.Header.Set("Referer", mangaOrigin)

	resp, err := api.RetryDo(c.http, req, maxRetries)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusBadRequest && http400IsSignIn {
		return ErrMangaAlreadySignedIn
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bili http error: status=%s", resp.Status)
	}
	var out model.BiliApiResponse[any]
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}
	if out.Code != 0 {
		return fmt.Errorf("bili api error: code=%d, message=%s", out.Code, out.Message)
	}
	return nil
}

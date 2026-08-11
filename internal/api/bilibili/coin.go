package bilibili

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

// getCoinData account.bilibili.com/site/getCoin 接口响应 data 结构。
type getCoinData struct {
	Money *float64 `json:"money"`
}

// GetCoinBalance 获取硬币余额。
func (c *BiliClient) GetCoinBalance(ctx context.Context, ck *model.Cookie) (float64, error) {
	resp, err := Get[getCoinData](c, ctx, "https://account.bilibili.com/site/getCoin", ck, false)
	if err != nil {
		return 0, err
	}
	if resp.Data.Money == nil {
		return 0, nil
	}
	return *resp.Data.Money, nil
}

// AddCoin 给视频/专栏投币。
// isArticle 为 true 时按专栏投币（avtype=2，Referer 用专栏链接）。
func (c *BiliClient) AddCoin(ctx context.Context, ck *model.Cookie, aid int64, multiply int, selectLike bool, isArticle bool) error {
	form := url.Values{}
	form.Set("aid", strconv.FormatInt(aid, 10))
	form.Set("multiply", strconv.Itoa(multiply))
	if selectLike {
		form.Set("select_like", "1")
	} else {
		form.Set("select_like", "0")
	}
	form.Set("cross_domain", "true")
	form.Set("csrf", ck.BiliJCT)
	if isArticle {
		form.Set("avtype", "2")
	} else {
		form.Set("avtype", "1")
	}
	form.Set("eab_x", "2")
	form.Set("ramval", "3")
	form.Set("source", "web_normal")
	form.Set("ga", "1")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.bilibili.com/x/web-interface/coin/add", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req, ck)
	if isArticle {
		req.Header.Set("Referer", fmt.Sprintf("https://www.bilibili.com/read/cv%d/", aid))
	} else {
		req.Header.Set("Referer", fmt.Sprintf("https://www.bilibili.com/video/av%d/", aid))
	}
	_, err = doAndDecode[any](c, req)
	return err
}

// archiveCoinsData /x/web-interface/archive/coins 接口响应 data 结构。
// data 为对象（非数字）：multiply=当前用户已投该视频的硬币枚数（未投为 0），
// count/favoured 一并声明以容忍字段差异。
type archiveCoinsData struct {
	Multiply int  `json:"multiply"`
	Count    int  `json:"count"`
	Favoured bool `json:"favoured"`
}

// GetArchiveCoins 获取已给指定视频投的硬币数（未投为 0）。
func (c *BiliClient) GetArchiveCoins(ctx context.Context, ck *model.Cookie, aid int64) (int, error) {
	resp, err := Get[archiveCoinsData](c, ctx, fmt.Sprintf("https://api.bilibili.com/x/web-interface/archive/coins?aid=%d", aid), ck, false)
	if err != nil {
		return 0, err
	}
	return resp.Data.Multiply, nil
}

package bilibili

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// silverOrigin 直播钱包接口的 Origin/Referer（原版 LiveDomainService 同款）。
const silverOrigin = "https://link.bilibili.com"

// silverStatusData /xlive/revenue/v1/wallet/getStatus 响应 data 结构。
// silver=银瓜子、coin=硬币、gold=金瓜子、silver_2_coin_left=今日剩余兑换次数、
// coin_2_silver_left=今日剩余兑换银瓜子次数。
type silverStatusData struct {
	Silver          float64 `json:"silver"`
	Coin            float64 `json:"coin"`
	Gold            float64 `json:"gold"`
	Silver2CoinLeft int     `json:"silver_2_coin_left"`
	Coin2SilverLeft int     `json:"coin_2_silver_left"`
}

// GetSilverStatus 获取直播钱包状态，返回今日剩余银瓜子兑换硬币次数（data.silver_2_coin_left）。
func (c *BiliClient) GetSilverStatus(ctx context.Context, ck *model.Cookie) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.live.bilibili.com/xlive/revenue/v1/wallet/getStatus", nil)
	if err != nil {
		return 0, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setHeaders(req, ck)
	req.Header.Set("Origin", silverOrigin)
	resp, err := doAndDecode[silverStatusData](c, req)
	if err != nil {
		return 0, err
	}
	return resp.Data.Silver2CoinLeft, nil
}

// silver2CoinData /xlive/revenue/v1/wallet/silver2coin 响应 data 结构（兑换后钱包快照）。
type silver2CoinData struct {
	Coin   float64 `json:"coin"`
	Gold   float64 `json:"gold"`
	Silver float64 `json:"silver"`
	Tid    string  `json:"tid"`
}

// ExchangeSilver2Coin 银瓜子兑换硬币。
// form 同时携带 csrf 与 csrf_token（均取 bili_jct）、visit_id（见 silverVisitID）、
// platform=pc；Referer/Origin 为 https://link.bilibili.com。
func (c *BiliClient) ExchangeSilver2Coin(ctx context.Context, ck *model.Cookie) error {
	form := url.Values{}
	form.Set("csrf", ck.BiliJCT)
	form.Set("csrf_token", ck.BiliJCT)
	form.Set("visit_id", silverVisitID())
	form.Set("platform", "pc")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.live.bilibili.com/xlive/revenue/v1/wallet/silver2coin", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req, ck)
	req.Header.Set("Origin", silverOrigin)
	req.Header.Set("Referer", silverOrigin)
	_, err = doAndDecode[silver2CoinData](c, req)
	return err
}

// silverVisitIDOnce / silverVisitIDCache 缓存进程内唯一的 visit_id（与原版 _visitId 静态缓存行为一致）。
var (
	silverVisitIDOnce  sync.Once
	silverVisitIDCache string
)

// silverVisitID 生成 visit_id："{first}{10 位随机小写字母数字串}{last}"，
// first=rand(1,9)、last 恒为 0；随机串偶数位为数字 0-9、奇数位为大写字母 A-Z，随后整体转小写。
// 进程内只生成一次并缓存（原版 §0.7 算法，Silver2Coin/Join/WearMedal 共用）。
func silverVisitID() string {
	silverVisitIDOnce.Do(func() {
		silverVisitIDCache = randomSilverVisitID()
	})
	return silverVisitIDCache
}

// randomSilverVisitID 按原版 RandomHelper.GenerateCode(10) 规则生成 visit_id。
func randomSilverVisitID() string {
	code := make([]byte, 10)
	for i := range code {
		if i%2 == 0 {
			code[i] = byte('0' + rand.IntN(10))
		} else {
			code[i] = byte('A' + rand.IntN(26))
		}
	}
	return fmt.Sprintf("%d%s0", rand.IntN(9)+1, strings.ToLower(string(code)))
}

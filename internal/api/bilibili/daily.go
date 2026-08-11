package bilibili

import (
	"context"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

// GetDailyTaskRewardInfo 获取每日任务奖励状态（登录/观看/投币/分享）。
func (c *BiliClient) GetDailyTaskRewardInfo(ctx context.Context, ck *model.Cookie) (*model.BiliApiResponse[model.DailyTaskInfo], error) {
	return Get[model.DailyTaskInfo](c, ctx, "https://api.bilibili.com/x/member/web/exp/reward", ck, false)
}

// GetDonatedCoinsToday 获取今日已投币数。
// 接口返回今日投币经验值，除以 10 取整得到硬币数。
func (c *BiliClient) GetDonatedCoinsToday(ctx context.Context, ck *model.Cookie) (int, error) {
	resp, err := Get[int](c, ctx, "https://api.bilibili.com/x/web-interface/coin/today/exp", ck, false)
	if err != nil {
		return 0, err
	}
	return resp.Data / 10, nil
}

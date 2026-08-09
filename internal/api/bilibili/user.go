package bilibili

import (
	"context"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// GetUserInfo 获取当前登录用户信息（nav 接口，响应中含 WBI 签名 keys）。
// 未登录时 code=-101，作为错误返回，由上层处理。
func (c *BiliClient) GetUserInfo(ctx context.Context, ck *model.Cookie) (*model.BiliApiResponse[model.UserInfo], error) {
	return Get[model.UserInfo](c, ctx, "https://api.bilibili.com/x/web-interface/nav", ck, false)
}

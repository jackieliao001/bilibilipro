package bilibili

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// followingsData 关注列表接口响应 data 结构。
type followingsData struct {
	List []model.UpInfo `json:"list"`
}

// GetRelationTags 获取用户自定义关注分组。
func (c *BiliClient) GetRelationTags(ctx context.Context, ck *model.Cookie) ([]model.TagInfo, error) {
	resp, err := Get[[]model.TagInfo](c, ctx, "https://api.bilibili.com/x/relation/tags?jsonp=jsonp", ck, false)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetFollowingsByTag 获取指定分组下的关注列表（一页 20 条）。
// 注意：/x/relation/tag 的 data 是【裸数组】（成员对象数组），不是 {list:[...]}；
// 与 /x/relation/followings（data.list）不同，勿再包一层。
func (c *BiliClient) GetFollowingsByTag(ctx context.Context, ck *model.Cookie, tagID int64, pn int) ([]model.UpInfo, error) {
	reqURL := fmt.Sprintf("https://api.bilibili.com/x/relation/tag?mid=%d&tagid=%d&pn=%d&ps=20", ck.UserID(), tagID, pn)
	resp, err := Get[[]model.UpInfo](c, ctx, reqURL, ck, false)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// ModifyRelation 修改关注关系（act=2 取关，act=1 关注）。
func (c *BiliClient) ModifyRelation(ctx context.Context, ck *model.Cookie, fid int64, act int) error {
	form := url.Values{}
	form.Set("fid", strconv.FormatInt(fid, 10))
	form.Set("act", strconv.Itoa(act))
	form.Set("re_src", "11")
	form.Set("csrf", ck.BiliJCT)
	form.Set("jsonp", "jsonp")
	_, err := PostForm[any](c, ctx, "https://api.bilibili.com/x/relation/modify", form, ck, false)
	return err
}

// GetFollowings 获取当前账号关注列表第一页（按最新关注排序，供 Daily 选视频使用）。
func (c *BiliClient) GetFollowings(ctx context.Context, ck *model.Cookie) ([]model.UpInfo, error) {
	reqURL := fmt.Sprintf("https://api.bilibili.com/x/relation/followings?vmid=%d&pn=1&ps=20&order=desc&order_type=attention", ck.UserID())
	resp, err := Get[followingsData](c, ctx, reqURL, ck, false)
	if err != nil {
		return nil, err
	}
	return resp.Data.List, nil
}

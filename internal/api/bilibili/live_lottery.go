package bilibili

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// 天选时刻角标 Pendent_id（504=天选时刻，426=百人成就，397=新星主播）。
const tianXuanPendentID = 504

// lotteryVisitIDOnce / lotteryVisitIDCache 缓存进程内唯一的 visit_id
// （与原版 JoinTianXuanRequest 静态字段 _visitId 行为一致）。
var (
	lotteryVisitIDOnce  sync.Once
	lotteryVisitIDCache string
)

// lotteryVisitID 返回进程内唯一的 visit_id。
func lotteryVisitID() string {
	lotteryVisitIDOnce.Do(func() {
		lotteryVisitIDCache = randomLotteryVisitID()
	})
	return lotteryVisitIDCache
}

// randomLotteryVisitID 按原版规则生成 visit_id："{first}{10 位随机小写字母数字串}{last}"，
// first = rand(1,9)、last 恒为 0；随机串按 RandomHelper.GenerateCode(10)：
// 偶数→数字 0-9，奇数→大写字母 A-Z，最后 ToLower。
func randomLotteryVisitID() string {
	var b strings.Builder
	b.WriteByte(byte('0' + rand.IntN(9) + 1))
	for i := 0; i < 10; i++ {
		if rand.IntN(2) == 0 {
			b.WriteByte(byte('0' + rand.IntN(10)))
		} else {
			b.WriteByte(byte('A' + rand.IntN(26)))
		}
	}
	b.WriteByte('0')
	return strings.ToLower(b.String())
}

// LiveArea 直播分区（getWebAreaList 的 data.data 条目）。
type LiveArea struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// liveAreasData getWebAreaList 响应 data（注意嵌套 data 字段）。
type liveAreasData struct {
	Data []LiveArea `json:"data"`
}

// GetLiveAreas 获取直播分区列表（source_id=2，不带 Cookie）。
func (c *BiliClient) GetLiveAreas(ctx context.Context) ([]LiveArea, error) {
	resp, err := Get[liveAreasData](c, ctx, "https://api.live.bilibili.com/xlive/web-interface/v1/index/getWebAreaList?source_id=2", nil, false)
	if err != nil {
		return nil, err
	}
	return resp.Data.Data, nil
}

// PendantInfo 直播间角标（Pendant_info 字典的 value）。
type PendantInfo struct {
	PendentID int64  `json:"Pendent_id"`
	Content   string `json:"Content"`
}

// TianXuanRoom getList 列表条目。
type TianXuanRoom struct {
	RoomID      int64                  `json:"roomid"`
	Uid         int64                  `json:"uid"`
	Title       string                 `json:"title"`
	Uname       string                 `json:"uname"`
	PendantInfo map[string]PendantInfo `json:"Pendant_info"`
}

// IsTianXuan 是否带"天选时刻"角标：Pendant_info 非空、含键 "2" 且 Pendent_id == 504。
func (r *TianXuanRoom) IsTianXuan() bool {
	if r == nil || len(r.PendantInfo) == 0 {
		return false
	}
	p, ok := r.PendantInfo["2"]
	return ok && p.PendentID == tianXuanPendentID
}

// TianXuanTag getList 的 new_tags 条目（携带下一页 sort_type）。
type TianXuanTag struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	SortType string `json:"sort_type"`
}

// TianXuanListData getList 响应 data。
type TianXuanListData struct {
	NewTags []TianXuanTag  `json:"new_tags"`
	List    []TianXuanRoom `json:"list"`
	HasMore int            `json:"has_more"`
}

// GetTianXuanList 获取直播分区下的直播列表（一页）。
// 参数与原版 GetListRequest 一致：platform=web、area_id=0、wts=当前 Unix 秒；
// sortType 第一页传空串，后续页传上一页 new_tags[0].sort_type。
func (c *BiliClient) GetTianXuanList(ctx context.Context, ck *model.Cookie, areaID int64, page int, sortType string) (*TianXuanListData, error) {
	params := url.Values{}
	params.Set("platform", "web")
	params.Set("parent_area_id", strconv.FormatInt(areaID, 10))
	params.Set("area_id", "0")
	params.Set("sort_type", sortType)
	params.Set("page", strconv.Itoa(page))
	params.Set("wts", strconv.FormatInt(time.Now().Unix(), 10))
	reqURL := "https://api.live.bilibili.com/xlive/web-interface/v1/second/getList?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setHeaders(req, ck)
	req.Header.Set("Referer", "https://live.bilibili.com/")
	req.Header.Set("Origin", "https://live.bilibili.com")

	resp, err := doAndDecode[TianXuanListData](c, req)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// TianXuanCheck 天选抽奖检查结果（Anchor/Check）。
// Status: 1=可参与 / 2=已结束；RequireType: 0=无 / 1=关注 / 2=粉丝牌等级 / 3=提督舰长。
type TianXuanCheck struct {
	ID             int64  `json:"id"`
	RoomID         int64  `json:"room_id"`
	Status         int    `json:"status"`
	AwardName      string `json:"award_name"`
	AwardNum       int    `json:"award_num"`
	Danmu          string `json:"danmu"`
	JoinType       int    `json:"join_type"`
	RequireType    *int   `json:"require_type"`
	RequireValue   int    `json:"require_value"`
	RequireText    string `json:"require_text"`
	GiftID         int64  `json:"gift_id"`
	GiftName       string `json:"gift_name"`
	GiftNum        int    `json:"gift_num"`
	GiftPrice      int    `json:"gift_price"`
	CurGiftNum     int    `json:"cur_gift_num"`
	SendGiftEnsure int    `json:"send_gift_ensure"`
}

// CheckTianXuan 检查直播间当前天选抽奖状态。
// 返回 ok=false 表示接口返回 data 为 null（无抽奖/数据异常），err 为请求或业务错误。
func (c *BiliClient) CheckTianXuan(ctx context.Context, ck *model.Cookie, roomID int64) (bool, *TianXuanCheck, error) {
	reqURL := fmt.Sprintf("https://api.live.bilibili.com/xlive/lottery-interface/v1/Anchor/Check?roomid=%d", roomID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return false, nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setHeaders(req, ck)
	req.Header.Set("Referer", "https://live.bilibili.com/")
	req.Header.Set("Origin", "https://live.bilibili.com")

	resp, err := doAndDecode[TianXuanCheck](c, req)
	if err != nil {
		return false, nil, err
	}
	// data 为 null 时解码为零值（id 恒大于 0，可用零值判定）。
	if resp.Data == (TianXuanCheck{}) {
		return false, nil, nil
	}
	return true, &resp.Data, nil
}

// JoinTianXuan 参与天选抽奖。form 携带 id/gift_id/gift_num（均取自 Check 结果）、
// csrf/csrf_token（=bili_jct）、visit_id、platform=pc。
// 注意：该接口有固定 3s 请求限流（原版 IntervalDelegatingHandler 特殊路径），
// 由调用方（task 层）在启用请求间隔时自行控制。
func (c *BiliClient) JoinTianXuan(ctx context.Context, ck *model.Cookie, check *TianXuanCheck) error {
	form := url.Values{}
	form.Set("id", strconv.FormatInt(check.ID, 10))
	form.Set("gift_id", strconv.FormatInt(check.GiftID, 10))
	form.Set("gift_num", strconv.Itoa(check.GiftNum))
	form.Set("csrf", ck.BiliJCT)
	form.Set("csrf_token", ck.BiliJCT)
	form.Set("visit_id", lotteryVisitID())
	form.Set("platform", "pc")
	_, err := PostForm[any](c, ctx, "https://api.live.bilibili.com/xlive/lottery-interface/v1/Anchor/Join", form, ck, false)
	return err
}

// GetRecentFollowings 获取关注列表第一页（按关注时间倒序，order_type 为空，
// 与原版 FollowingsOrderType.TimeDesc 一致）。供天选抽奖分组使用：
// 首项即"最近一次关注"。
func (c *BiliClient) GetRecentFollowings(ctx context.Context, ck *model.Cookie) ([]model.UpInfo, error) {
	params := url.Values{}
	params.Set("vmid", ck.DedeUserID)
	params.Set("order_type", "")
	params.Set("pn", "1")
	params.Set("ps", "20")
	params.Set("order", "desc")
	params.Set("jsonp", "jsonp")
	reqURL := "https://api.bilibili.com/x/relation/followings?" + params.Encode()

	resp, err := Get[followingsData](c, ctx, reqURL, ck, false)
	if err != nil {
		return nil, err
	}
	return resp.Data.List, nil
}

// createRelationTagData /x/relation/tag/create 响应 data。
type createRelationTagData struct {
	TagID int64 `json:"tagid"`
}

// CreateRelationTag 创建关注分组，返回新分组 tagid。
func (c *BiliClient) CreateRelationTag(ctx context.Context, ck *model.Cookie, tag string) (int64, error) {
	form := url.Values{}
	form.Set("tag", tag)
	form.Set("csrf", ck.BiliJCT)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.bilibili.com/x/relation/tag/create?cross_domain=true", strings.NewReader(form.Encode()))
	if err != nil {
		return 0, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req, ck)
	req.Header.Set("Origin", "https://space.bilibili.com")
	req.Header.Set("Referer", fmt.Sprintf("https://space.bilibili.com/%s/fans/follow", ck.DedeUserID))

	resp, err := doAndDecode[createRelationTagData](c, req)
	if err != nil {
		return 0, err
	}
	return resp.Data.TagID, nil
}

// CopyUpsToGroup 批量把多个 UID 加入指定关注分组（fids 逗号拼接）。
func (c *BiliClient) CopyUpsToGroup(ctx context.Context, ck *model.Cookie, fids []int64, tagID int64) error {
	fidStrs := make([]string, 0, len(fids))
	for _, f := range fids {
		fidStrs = append(fidStrs, strconv.FormatInt(f, 10))
	}
	form := url.Values{}
	form.Set("fids", strings.Join(fidStrs, ","))
	form.Set("tagids", strconv.FormatInt(tagID, 10))
	form.Set("csrf", ck.BiliJCT)
	form.Set("jsonp", "jsonp")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.bilibili.com/x/relation/tags/copyUsers", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req, ck)
	req.Header.Set("Origin", "https://space.bilibili.com")
	req.Header.Set("Referer", fmt.Sprintf("https://space.bilibili.com/%s/fans/follow?tagid=-1", ck.DedeUserID))

	_, err = doAndDecode[any](c, req)
	return err
}

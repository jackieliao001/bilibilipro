package bilibili

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

const (
	// liveHeartBeatEnterPath / liveHeartBeatPath 心跳接口路径
	// （原版 IntervalDelegatingHandler 特殊限流路径：固定 1s，由 task 层控制）。
	liveHeartBeatEnterPath = "/xlive/data-interface/v1/x25Kn/E"
	liveHeartBeatPath      = "/xlive/data-interface/v1/x25Kn/X"
	// danmakuRnd 弹幕请求固定 rnd（原版 SendLiveDanmukuRequest 硬编码常量）。
	danmakuRnd = "1672305761"
)

// RoomInfo 直播间信息（room/v1/Room/get_info；该接口原版签名不带 Cookie）。
type RoomInfo struct {
	RoomID       int64 `json:"room_id"`
	AreaID       int64 `json:"area_id"`
	ParentAreaID int64 `json:"parent_area_id"`
	LiveStatus   int   `json:"live_status"`
	Uid          int64 `json:"uid"`
}

// GetRoomInfo 获取直播间信息（不带 Cookie）。
func (c *BiliClient) GetRoomInfo(ctx context.Context, roomID int64) (*RoomInfo, error) {
	reqURL := fmt.Sprintf("https://api.live.bilibili.com/room/v1/Room/get_info?room_id=%d&from=room", roomID)
	resp, err := Get[RoomInfo](c, ctx, reqURL, nil, false)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// MedalInfo 粉丝勋章信息。
type MedalInfo struct {
	MedalName string `json:"medal_name"`
	MedalID   int64  `json:"medal_id"`
	TargetID  int64  `json:"target_id"`
	Level     int    `json:"level"`
}

// MedalWallItem 粉丝勋章墙条目。
type MedalWallItem struct {
	LiveStatus int       `json:"live_status"`
	TargetName string    `json:"target_name"`
	Link       string    `json:"link"`
	MedalInfo  MedalInfo `json:"medal_info"`
}

// medalWallData MedalWall 响应 data。
type medalWallData struct {
	List []MedalWallItem `json:"list"`
}

// GetMedalWall 获取粉丝勋章墙（target_id 为目标用户 UID，page/page_size 分页）。
func (c *BiliClient) GetMedalWall(ctx context.Context, ck *model.Cookie, targetID int64, page, pageSize int) ([]MedalWallItem, error) {
	params := url.Values{}
	params.Set("target_id", strconv.FormatInt(targetID, 10))
	params.Set("page", strconv.Itoa(page))
	params.Set("page_size", strconv.Itoa(pageSize))
	reqURL := "https://api.live.bilibili.com/xlive/web-ucenter/user/MedalWall?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	c.setHeaders(req, ck)
	req.Header.Set("Referer", "https://live.bilibili.com/")
	req.Header.Set("Origin", "https://live.bilibili.com")

	resp, err := doAndDecode[medalWallData](c, req)
	if err != nil {
		return nil, err
	}
	return resp.Data.List, nil
}

// SendDanmaku 向直播间发送一条弹幕（bubble=0/color=16777215/mode=1/fontsize=25/rnd 固定）。
func (c *BiliClient) SendDanmaku(ctx context.Context, ck *model.Cookie, roomID int64, content string) error {
	form := url.Values{}
	form.Set("bubble", "0")
	form.Set("msg", content)
	form.Set("color", "16777215")
	form.Set("mode", "1")
	form.Set("fontsize", "25")
	form.Set("rnd", danmakuRnd)
	form.Set("roomid", strconv.FormatInt(roomID, 10))
	form.Set("csrf", ck.BiliJCT)
	form.Set("csrf_token", ck.BiliJCT)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.live.bilibili.com/msg/send", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req, ck)

	_, err = doAndDecode[any](c, req)
	return err
}

// LikeRoom 点赞直播间。body 为 raw form 字符串（字段名与原版
// LikeLiveRoomRequest.RawTextBuild 一致）：click_time/room_id/uid/anchor_id/csrf_token/csrf。
func (c *BiliClient) LikeRoom(ctx context.Context, ck *model.Cookie, roomID, anchorID int64, clickTime int) error {
	body := fmt.Sprintf("click_time=%d&room_id=%d&uid=%s&anchor_id=%d&csrf_token=%s&csrf=%s",
		clickTime, roomID, ck.DedeUserID, anchorID, ck.BiliJCT, ck.BiliJCT)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.live.bilibili.com/xlive/app-ucenter/v1/like_info_v3/like/likeReportV3", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req, ck)
	req.Header.Set("Referer", "https://live.bilibili.com/")
	req.Header.Set("Origin", "https://live.bilibili.com")

	_, err = doAndDecode[any](c, req)
	return err
}

// HeartBeatData 心跳接口响应 data（首包响应提供后续签名所需三元组
// secret_key / secret_rule / timestamp）。
type HeartBeatData struct {
	HeartbeatInterval int    `json:"heartbeat_interval"`
	SecretKey         string `json:"secret_key"`
	SecretRule        []int  `json:"secret_rule"`
	Timestamp         int64  `json:"timestamp"`
}

// SendHeartbeat 发送直播间心跳包。
// seq==0 时走 EnterRoom（X25Kn/E 首包，无签名，需 ruid/is_patch/heart_beat）；
// seq>0 时走 HeartBeat（X25Kn/X 后续包，用 prev 的 secret_key/secret_rule/timestamp
// 生成 HMAC 签名 s）。seq 编号即心跳包编号，进入 id 数组与签名 JSON 的 seq_id。
// 返回响应 data；data 为 null 时返回 (nil, nil)（对应原版 Data==null 的判定）。
// 注意：E/X 接口有固定 1s 请求限流（原版特殊路径），由调用方（task 层）控制。
func (c *BiliClient) SendHeartbeat(ctx context.Context, ck *model.Cookie, room *RoomInfo, seq int, uuid string, prev *HeartBeatData) (*HeartBeatData, error) {
	buvid := cookieValue(ck, "LIVE_BUVID")
	ts := time.Now().UnixMilli()

	form := url.Values{}
	form.Set("id", heartbeatID(room.ParentAreaID, room.AreaID, seq, room.RoomID))
	form.Set("ts", strconv.FormatInt(ts, 10))
	form.Set("ua", c.userAgent())
	form.Set("csrf", ck.BiliJCT)
	form.Set("csrf_token", ck.BiliJCT)
	form.Set("visit_id", "")
	form.Set("device", fmt.Sprintf("[\"%s\",\"%s\"]", buvid, uuid))

	path := liveHeartBeatEnterPath
	if seq > 0 {
		path = liveHeartBeatPath
		if prev == nil {
			return nil, fmt.Errorf("心跳第 %d 包缺少上一次响应（secret_key/secret_rule）", seq)
		}
		s, err := (LiveHeartBeatCrypto{}).Sign(HeartBeatPayload{
			Platform: "web",
			ParentID: room.ParentAreaID,
			AreaID:   room.AreaID,
			SeqID:    seq,
			RoomID:   room.RoomID,
			Buvid:    buvid,
			UUID:     uuid,
			Ets:      prev.Timestamp,
			Time:     60,
			Ts:       ts,
		}, prev.SecretKey, prev.SecretRule)
		if err != nil {
			return nil, fmt.Errorf("生成心跳签名失败: %w", err)
		}
		form.Set("s", s)
		form.Set("ets", strconv.FormatInt(prev.Timestamp, 10))
		form.Set("benchmark", prev.SecretKey)
		form.Set("time", "60")
	} else {
		form.Set("ruid", strconv.FormatInt(room.Uid, 10))
		form.Set("is_patch", "0")
		form.Set("heart_beat", "[]")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://live-trace.bilibili.com"+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.setHeaders(req, ck)

	resp, err := doAndDecode[HeartBeatData](c, req)
	if err != nil {
		return nil, err
	}
	if isEmptyHeartBeatData(resp.Data) {
		return nil, nil
	}
	return &resp.Data, nil
}

// heartbeatID 构造 id 字段：JSON 数组 [parent_area_id, area_id, seq, room_id]。
func heartbeatID(parentAreaID, areaID int64, seq int, roomID int64) string {
	return fmt.Sprintf("[%d,%d,%d,%d]", parentAreaID, areaID, seq, roomID)
}

// isEmptyHeartBeatData data 是否为空（对应原版响应 data 为 null 的场景：
// 此时不更新签名三元组，但包仍算发送成功）。
func isEmptyHeartBeatData(d HeartBeatData) bool {
	return d.SecretKey == "" && d.Timestamp == 0 && d.HeartbeatInterval == 0 && len(d.SecretRule) == 0
}

// cookieValue 从 cookie 串中读取指定键的值（用于 LIVE_BUVID 等未映射为
// model.Cookie 命名属性的字段）。
func cookieValue(ck *model.Cookie, key string) string {
	if ck == nil {
		return ""
	}
	for _, seg := range strings.Split(ck.String(), ";") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		kv := strings.SplitN(seg, "=", 2)
		if len(kv) == 2 && strings.EqualFold(strings.TrimSpace(kv[0]), key) {
			return strings.TrimSpace(kv[1])
		}
	}
	return ""
}

// HasLiveBuvid Cookie 是否已包含 LIVE_BUVID。
func HasLiveBuvid(ck *model.Cookie) bool {
	return cookieValue(ck, "LIVE_BUVID") != ""
}

// EnsureLiveBuvid 补齐 LIVE_BUVID：缺失时访问 B 站首页并合并 Set-Cookie 响应头
// （对应原版 CheckLiveCookie 自动配置直播 Cookie）。
// 返回补齐后是否已包含 LIVE_BUVID。
func (c *BiliClient) EnsureLiveBuvid(ctx context.Context, ck *model.Cookie) (bool, error) {
	if HasLiveBuvid(ck) {
		return true, nil
	}
	setCookies, err := c.GetHomePage(ctx, ck)
	if err != nil {
		return false, fmt.Errorf("访问首页补齐 LIVE_BUVID 失败: %w", err)
	}
	ck.MergeSetCookie(setCookies)
	return HasLiveBuvid(ck), nil
}

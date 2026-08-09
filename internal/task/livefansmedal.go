package task

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/raywangqvq/bilitoolgo/internal/api/bilibili"
	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// 编译期接口断言。
var _ Task = (*LiveFansMedalTask)(nil)

// LiveFansMedalTask 直播间挂机任务：向粉丝牌直播间发送弹幕、点赞，
// 并以 65s 节流窗口持续发送心跳包（首包 X25Kn/E + 后续 X25Kn/X HMAC 签名）。
type LiveFansMedalTask struct {
	cfg    *model.Config
	client *bilibili.BiliClient
	*MultiAccountTask

	heartBeatNumber int           // 每直播间心跳包个数（默认 70，即约 70 分钟挂机）
	beatInterval    time.Duration // 心跳节流窗口（默认 65s = HeartBeatInterval 60 + 5）
	reqPacing       time.Duration // 心跳请求最小间隔（默认 1s，仅启用请求间隔时生效）
	likeNumber      int           // 点赞次数 click_time（默认 30）
	danmakuContent  string        // 弹幕内容（默认 "OvO"）
	danmakuNumber   int           // 弹幕发送次数（默认 1）
	danmakuGiveUp   int           // 弹幕连续失败放弃阈值（默认 3）
}

// 心跳与弹幕的固定参数（对应原版 LiveFansMedalTaskOptions / 常量）。
const (
	heartBeatGiveUpThreshold = 5    // 心跳连续失败放弃阈值
	skipLevel20Medal         = true // 跳过粉丝牌等级 >= 20 的主播（观看不再增长亲密度）
	medalWallPageSize        = 50   // 粉丝牌墙分页大小
)

// NewLiveFansMedalTask 创建直播间挂机任务。
func NewLiveFansMedalTask(cfg *model.Config, client *bilibili.BiliClient, cookies []*model.Cookie, logger *slog.Logger) Task {
	t := &LiveFansMedalTask{
		cfg:             cfg,
		client:          client,
		heartBeatNumber: 70,
		beatInterval:    65 * time.Second,
		reqPacing:       time.Second,
		likeNumber:      30,
		danmakuContent:  "OvO",
		danmakuNumber:   1,
		danmakuGiveUp:   3,
	}
	t.MultiAccountTask = &MultiAccountTask{
		Name:    "LiveFansMedal",
		Cookies: cookies,
		Logger:  logger,
		DoFunc:  t.doForAccount,
	}
	return t
}

// Name 任务名。
func (t *LiveFansMedalTask) Name() string { return t.MultiAccountTask.Name }

// medalRoom 挂机目标直播间（粉丝牌 + 直播间信息）。
type medalRoom struct {
	roomID   int64
	roomInfo *bilibili.RoomInfo
	medal    bilibili.MedalWallItem
}

// doForAccount 单个账号的直播间挂机流程。
func (t *LiveFansMedalTask) doForAccount(ctx context.Context, ck *model.Cookie, idx int) (*model.AccountResult, error) {
	ar := &model.AccountResult{UserID: ck.DedeUserID, Success: true}
	fm := t.cfg.Tasks.LiveFansMedal

	// 1. 启用检查。
	if !fm.Enabled {
		ar.Steps = append(ar.Steps, model.StepResult{Name: "直播间挂机", Status: "skip", Message: "直播间挂机未启用"})
		return ar, nil
	}

	// 2. 登录验证：失败直接中止该账号。
	if ok, msg := verifyLogin(ctx, t.client, ck, t.Logger); !ok {
		ar.Success = false
		ar.Steps = append(ar.Steps, model.StepResult{Name: "登录验证", Status: "fail", Message: msg})
		return ar, nil
	}
	ar.Steps = append(ar.Steps, model.StepResult{Name: "登录验证", Status: "ok", Message: "登录成功"})

	// 3. 补齐 LIVE_BUVID：缺失时访问首页合并 Set-Cookie（失败放弃后续子任务）。
	liveBuvidOK := true
	step := t.runStepNoThrow(ar, "补齐直播Cookie", func() error {
		ok, err := t.client.EnsureLiveBuvid(ctx, ck)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("访问首页后仍未获取到 LIVE_BUVID")
		}
		if err := model.SaveCookiesToFile(t.Cookies, cookieFilePath()); err != nil {
			t.Logger.Warn("保存 Cookie 失败", "err", err)
		}
		return nil
	})
	if step.Status != "ok" {
		liveBuvidOK = false
	}

	// 4. 获取粉丝牌目标列表（Uid 配置或自动使用当前账号 UID）。
	var rooms []medalRoom
	t.runStepNoThrow(ar, "获取粉丝牌列表", func() error {
		targetID := fm.Uid
		if targetID <= 0 {
			targetID = ck.UserID()
		}
		items, err := t.client.GetMedalWall(ctx, ck, targetID, 1, medalWallPageSize)
		if err != nil {
			return err
		}
		rooms, err = t.buildRooms(ctx, ck, items)
		return err
	})

	// 5. 弹幕 + 点赞 + 心跳：三个子任务相互独立（对应原版 rethrowWhenException:false）。
	if !liveBuvidOK {
		for _, name := range []string{"发送弹幕", "点赞直播间", "直播时长挂机"} {
			t.skipStep(ar, name, "LIVE_BUVID 补齐失败，放弃直播相关任务")
		}
		return ar, nil
	}
	liveRooms := filterLiveRooms(rooms)
	t.runStepNoThrow(ar, "发送弹幕", func() error {
		return t.sendDanmakuToRooms(ctx, ck, rooms)
	})
	t.runStepNoThrow(ar, "点赞直播间", func() error {
		return t.likeRooms(ctx, ck, liveRooms)
	})
	t.runStepNoThrow(ar, "直播时长挂机", func() error {
		return t.heartBeatRooms(ctx, ck, liveRooms)
	})

	return ar, nil
}

// buildRooms 粉丝牌 → 挂机目标列表：过滤等级 >= 20 的粉丝牌，
// 从链接解析直播间 ID 并获取房间信息（任一步失败跳过该主播）。
func (t *LiveFansMedalTask) buildRooms(ctx context.Context, ck *model.Cookie, items []bilibili.MedalWallItem) ([]medalRoom, error) {
	var rooms []medalRoom
	for _, item := range items {
		if skipLevel20Medal && item.MedalInfo.Level >= 20 {
			t.Logger.Info("粉丝牌等级已满，观看不再增长亲密度，跳过", "up", item.TargetName, "level", item.MedalInfo.Level)
			continue
		}
		roomID, err := parseRoomID(item.Link)
		if err != nil {
			t.Logger.Warn("解析直播间链接失败，跳过", "up", item.TargetName, "link", item.Link, "err", err)
			continue
		}
		info, err := t.client.GetRoomInfo(ctx, roomID)
		if err != nil {
			t.Logger.Warn("获取直播间信息失败，跳过", "up", item.TargetName, "room", roomID, "err", err)
			continue
		}
		rooms = append(rooms, medalRoom{roomID: roomID, roomInfo: info, medal: item})
	}
	return rooms, nil
}

// parseRoomID 从粉丝牌链接（如 //live.bilibili.com/22547141）解析直播间 ID。
func parseRoomID(link string) (int64, error) {
	link = strings.TrimSpace(link)
	idx := strings.LastIndex(link, "/")
	if idx < 0 {
		return 0, fmt.Errorf("链接无路径: %s", link)
	}
	id, err := strconv.ParseInt(strings.TrimSpace(link[idx+1:]), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("链接尾部不是有效房间号: %s", link)
	}
	return id, nil
}

// filterLiveRooms 只保留开播中的直播间（live_status != 0）。
func filterLiveRooms(rooms []medalRoom) []medalRoom {
	var out []medalRoom
	for _, r := range rooms {
		if r.roomInfo != nil && r.roomInfo.LiveStatus != 0 {
			out = append(out, r)
		}
	}
	return out
}

// sendDanmakuToRooms 向每个粉丝牌直播间发送弹幕；
// 失败重试，重试间随机休息 2~4s，连续失败 danmakuGiveUp 次放弃该房间。
func (t *LiveFansMedalTask) sendDanmakuToRooms(ctx context.Context, ck *model.Cookie, rooms []medalRoom) error {
	if len(rooms) == 0 {
		return fmt.Errorf("%w: 未获取到粉丝牌直播间", errSkip)
	}
	for _, room := range rooms {
		success, failed := 0, 0
		for success < t.danmakuNumber && failed < t.danmakuGiveUp {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if err := t.client.SendDanmaku(ctx, ck, room.roomID, t.danmakuContent); err != nil {
				failed++
				t.Logger.Warn("弹幕发送失败", "room", room.roomID, "err", err)
			} else {
				success++
			}
			// 仅在可能继续请求时休息（原版每次尝试后均随机休息 2~4s）。
			if success < t.danmakuNumber && failed < t.danmakuGiveUp {
				time.Sleep(time.Duration(rand.IntN(2000)+2000) * time.Millisecond)
			}
		}
		t.Logger.Info("弹幕发送完成", "room", room.roomID, "up", room.medal.TargetName,
			"success", success, "total", success+failed)
	}
	return nil
}

// likeRooms 点赞开播中的直播间（click_time = likeNumber）；房间间随机休息 5~8s。
func (t *LiveFansMedalTask) likeRooms(ctx context.Context, ck *model.Cookie, rooms []medalRoom) error {
	if len(rooms) == 0 {
		return fmt.Errorf("%w: 未检测到开播的直播间", errSkip)
	}
	for i, room := range rooms {
		if err := t.client.LikeRoom(ctx, ck, room.roomID, room.roomInfo.Uid, t.likeNumber); err != nil {
			t.Logger.Warn("点赞直播间失败", "room", room.roomID, "err", err)
		} else {
			t.Logger.Info("点赞直播间完成", "room", room.roomID)
		}
		if i < len(rooms)-1 {
			time.Sleep(time.Duration(rand.IntN(3000)+5000) * time.Millisecond)
		}
	}
	return nil
}

// heartBeatState 单直播间心跳状态。
type heartBeatState struct {
	room       medalRoom
	count      int       // 已成功发送的心跳包数（同时作为下一包的 seq）
	lastBeat   time.Time // 上次心跳发送时间（用于 65s 节流）
	failed     int       // 连续失败次数
	secretKey  string    // 服务端下发的签名密钥
	secretRule []int     // 服务端下发的签名规则
	timestamp  int64     // 服务端下发的 ets
}

// heartBeatRooms 心跳挂机循环：每个直播间发送 heartBeatNumber 个心跳包
// （首包 seq=0 走 X25Kn/E，后续 seq=1.. 走 X25Kn/X 并携带 HMAC 签名）；
// 相邻心跳间隔不小于 beatInterval（65s）；单房间连续失败 5 次放弃；
// 所有房间达标或放弃后退出。
func (t *LiveFansMedalTask) heartBeatRooms(ctx context.Context, ck *model.Cookie, rooms []medalRoom) error {
	if len(rooms) == 0 {
		return fmt.Errorf("%w: 未检测到开播的直播间", errSkip)
	}
	states := make([]*heartBeatState, 0, len(rooms))
	for _, r := range rooms {
		states = append(states, &heartBeatState{room: r})
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		allDone := true
		for _, st := range states {
			if st.failed >= heartBeatGiveUpThreshold || st.count >= t.heartBeatNumber {
				continue
			}
			allDone = false
			t.beatOnce(ctx, ck, st)
		}
		if allDone {
			break
		}
	}

	success := 0
	for _, st := range states {
		if st.count >= t.heartBeatNumber {
			success++
		}
	}
	t.Logger.Info("心跳挂机完成", "success", success, "total", len(states))
	return nil
}

// beatOnce 给单个直播间发送一个心跳包。
// 节流等待与请求间隔等待均可被 ctx 取消（取消时直接返回，不发包）。
func (t *LiveFansMedalTask) beatOnce(ctx context.Context, ck *model.Cookie, st *heartBeatState) {
	// 节流：距上次心跳不足 beatInterval 则补齐剩余时间。
	now := time.Now()
	if wait := t.beatInterval - now.Sub(st.lastBeat); wait > 0 {
		t.Logger.Debug("心跳节流休眠", "room", st.room.roomID, "ms", wait.Milliseconds())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	t.paceHeartBeat(ctx)

	seq := st.count
	uuid := newUUID()
	var prev *bilibili.HeartBeatData
	if seq > 0 {
		prev = &bilibili.HeartBeatData{
			SecretKey:  st.secretKey,
			SecretRule: st.secretRule,
			Timestamp:  st.timestamp,
		}
	}
	data, err := t.client.SendHeartbeat(ctx, ck, st.room.roomInfo, seq, uuid, prev)
	st.lastBeat = time.Now()

	if err != nil {
		st.failed++
		t.Logger.Warn("心跳包发送失败", "room", st.room.roomID, "seq", seq, "err", err)
		return
	}
	if data != nil {
		// 响应携带新的签名三元组，供下一包使用。
		st.secretKey = data.SecretKey
		st.secretRule = data.SecretRule
		st.timestamp = data.Timestamp
	}
	st.count++
	st.failed = 0
	t.Logger.Info("心跳包发送成功", "room", st.room.roomID, "seq", seq)
}

// paceHeartBeat 心跳请求前的固定 1s 限流（原版 E/X 特殊路径；
// 仅当配置了请求间隔时生效，与 C# 行为一致）。ctx 取消时提前返回。
func (t *LiveFansMedalTask) paceHeartBeat(ctx context.Context) {
	if t.cfg.Bilibili.IntervalSeconds > 0 && t.reqPacing > 0 {
		timer := time.NewTimer(t.reqPacing)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// newUUID 生成 v4 风格 UUID 字符串（对应原版 Guid.NewGuid()）。
func newUUID() string {
	b := make([]byte, 16)
	if _, err := cryptorand.Read(b); err != nil {
		// 极端情况下退回时间戳方案，避免挂机任务中断。
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

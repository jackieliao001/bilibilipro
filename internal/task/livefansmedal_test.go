package task

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raywangqvq/bilitoolgo/internal/api/bilibili"
	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// medalTestConfig 构造直播间挂机测试配置。
func medalTestConfig(modify func(*model.LiveFansMedalConfig)) *model.Config {
	fm := model.LiveFansMedalConfig{Enabled: true, Uid: 0}
	if modify != nil {
		modify(&fm)
	}
	return &model.Config{
		Bilibili: model.BilibiliConfig{IntervalSeconds: 0, UserAgent: "test"},
		Tasks:    model.TasksConfig{LiveFansMedal: fm},
	}
}

// medalTestCookie 直播间挂机测试 Cookie（无 LIVE_BUVID，触发补齐流程）。
func medalTestCookie() *model.Cookie {
	ck, err := model.ParseCookie("DedeUserID=100; SESSDATA=s; bili_jct=jct")
	if err != nil {
		panic(err)
	}
	return ck
}

// medalTestCookieWithBuvid 已含 LIVE_BUVID 的测试 Cookie。
func medalTestCookieWithBuvid() *model.Cookie {
	ck, err := model.ParseCookie("DedeUserID=100; SESSDATA=s; bili_jct=jct; LIVE_BUVID=lbv0")
	if err != nil {
		panic(err)
	}
	return ck
}

// newMedalClient 构造注入 mock transport 的 BiliClient。
func newMedalClient(t *testing.T, rt http.RoundTripper) *bilibili.BiliClient {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := bilibili.New(&model.Config{Bilibili: model.BilibiliConfig{IntervalSeconds: 0, UserAgent: "test"}}, logger)
	bilibili.SetTransportForTest(client, rt)
	return client
}

// runMedal 执行一次完整的挂机任务（单账号）；opts 可调整任务参数（心跳次数/节流窗口等）。
func runMedal(t *testing.T, cfg *model.Config, client *bilibili.BiliClient, ck *model.Cookie, opts ...func(*LiveFansMedalTask)) *model.TaskResult {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tt := NewLiveFansMedalTask(cfg, client, []*model.Cookie{ck}, logger)
	lt := tt.(*LiveFansMedalTask)
	for _, o := range opts {
		o(lt)
	}
	res, err := tt.Run(context.Background())
	if err != nil {
		t.Fatalf("LiveFansMedalTask.Run 失败: %v", err)
	}
	if len(res.Accounts) != 1 {
		t.Fatalf("应产生 1 个账号结果, got %d", len(res.Accounts))
	}
	return res
}

// medalTransport 直播间挂机 mock transport。
// 记录各 POST 路径的表单与 Cookie 头，便于断言。
type medalTransport struct {
	t      *testing.T
	counts *reqCounts

	navCode       int
	homeSetCookie string
	medalWallBody string
	roomInfoBody  string
	danmakuCode   int
	likeCode      int
	heartBeatCode int
	heartBeatBody string

	forms   map[string]url.Values
	cookies map[string]string
}

func newMedalTransport(t *testing.T, counts *reqCounts) *medalTransport {
	return &medalTransport{
		t:       t,
		counts:  counts,
		forms:   map[string]url.Values{},
		cookies: map[string]string{},
	}
}

func (m *medalTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	path := r.URL.Path
	m.counts.inc(path)
	m.cookies[path] = r.Header.Get("Cookie")
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			m.t.Fatalf("parse form: %v", err)
		}
		m.forms[path] = r.PostForm
	}

	switch path {
	case "/":
		resp := jsonResponse(`{"code":0,"message":"0","ttl":1,"data":null}`)
		if m.homeSetCookie != "" {
			resp.Header.Set("Set-Cookie", m.homeSetCookie)
		}
		return resp, nil
	case "/x/web-interface/nav":
		if m.navCode != 0 {
			return jsonResponse(`{"code":` + strconv.Itoa(m.navCode) + `,"message":"未登录","ttl":1,"data":null}`), nil
		}
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"isLogin":true,"uname":"测试用户","money":100,"level_info":{"current_level":5},"wbi_img":{"img_url":"","sub_url":""}}}`), nil
	case "/xlive/web-ucenter/user/MedalWall":
		return jsonResponse(m.medalWallBody), nil
	case "/room/v1/Room/get_info":
		return jsonResponse(m.roomInfoBody), nil
	case "/msg/send":
		return jsonResponse(`{"code":` + strconv.Itoa(m.danmakuCode) + `,"message":"m","ttl":1,"data":null}`), nil
	case "/xlive/app-ucenter/v1/like_info_v3/like/likeReportV3":
		return jsonResponse(`{"code":` + strconv.Itoa(m.likeCode) + `,"message":"m","ttl":1,"data":null}`), nil
	case "/xlive/data-interface/v1/x25Kn/E", "/xlive/data-interface/v1/x25Kn/X":
		if m.heartBeatCode != 0 {
			return jsonResponse(`{"code":` + strconv.Itoa(m.heartBeatCode) + `,"message":"m","ttl":1,"data":null}`), nil
		}
		return jsonResponse(m.heartBeatBody), nil
	default:
		m.t.Errorf("unexpected request path: %s", path)
		return serverErrorResponse(), nil
	}
}

// medalWallBody 构造粉丝勋章墙响应。
func medalWallBody(items ...string) string {
	return `{"code":0,"message":"0","ttl":1,"data":{"list":[` + strings.Join(items, ",") + `]}}`
}

// medalItem 构造粉丝牌条目。
func medalItem(liveStatus, level int, roomID int64, targetID int64) string {
	return `{"live_status":` + strconv.Itoa(liveStatus) + `,"target_name":"主播A","link":"//live.bilibili.com/` +
		strconv.FormatInt(roomID, 10) + `","medal_info":{"medal_name":"A牌","medal_id":1,"target_id":` +
		strconv.FormatInt(targetID, 10) + `,"level":` + strconv.Itoa(level) + `}}`
}

// roomInfoBody 构造直播间信息响应。
func roomInfoBody(roomID, areaID, parentAreaID int64, liveStatus int, uid int64) string {
	return `{"code":0,"message":"0","ttl":1,"data":{"room_id":` + strconv.FormatInt(roomID, 10) +
		`,"area_id":` + strconv.FormatInt(areaID, 10) + `,"parent_area_id":` + strconv.FormatInt(parentAreaID, 10) +
		`,"live_status":` + strconv.Itoa(liveStatus) + `,"uid":` + strconv.FormatInt(uid, 10) + `}}`
}

// heartBeatBody 构造心跳响应（首包返回签名三元组）。
func heartBeatBody(secretKey string, rules string, ts int64) string {
	return `{"code":0,"message":"0","ttl":1,"data":{"heartbeat_interval":60,"secret_key":"` + secretKey +
		`","secret_rule":[` + rules + `],"timestamp":` + strconv.FormatInt(ts, 10) + `}}`
}

// TestLiveFansMedalDisabled 未启用：跳过且不发起任何请求。
func TestLiveFansMedalDisabled(t *testing.T) {
	cfg := medalTestConfig(func(fm *model.LiveFansMedalConfig) { fm.Enabled = false })
	counts := newReqCounts()
	client := newMedalClient(t, newMedalTransport(t, counts))

	ar := runMedal(t, cfg, client, medalTestCookie()).Accounts[0]
	step := findStep(&ar, "直播间挂机")
	if step == nil || step.Status != "skip" || step.Message != "直播间挂机未启用" {
		t.Fatalf("应 skip(未启用): %+v", ar.Steps)
	}
	for path, n := range counts.m {
		t.Fatalf("未启用时不应发起请求: %s x%d", path, n)
	}
}

// TestLiveFansMedalFullFlow 全流程：LIVE_BUVID 补齐 → 弹幕 → 点赞 → 心跳 E+X。
func TestLiveFansMedalFullFlow(t *testing.T) {
	t.Setenv("RAY_COOKIE_FILE", filepath.Join(t.TempDir(), "cookies.json"))
	cfg := medalTestConfig(nil)
	counts := newReqCounts()
	mt := newMedalTransport(t, counts)
	mt.homeSetCookie = "LIVE_BUVID=lbv-123; Path=/"
	mt.medalWallBody = medalWallBody(medalItem(1, 5, 1001, 2002))
	mt.roomInfoBody = roomInfoBody(1001, 2, 1, 1, 2002)
	mt.heartBeatBody = heartBeatBody("skey", "0,2", 100)
	client := newMedalClient(t, mt)

	res := runMedal(t, cfg, client, medalTestCookie(),
		func(lt *LiveFansMedalTask) { lt.heartBeatNumber = 2; lt.beatInterval = 0 },
	)
	ar := res.Accounts[0]
	if !ar.Success {
		t.Fatalf("全流程应成功: %+v", ar.Steps)
	}
	for _, name := range []string{"登录验证", "补齐直播Cookie", "获取粉丝牌列表", "发送弹幕", "点赞直播间", "直播时长挂机"} {
		if s := findStep(&ar, name); s == nil || s.Status != "ok" {
			t.Fatalf("步骤 %s 应 ok: %+v", name, ar.Steps)
		}
	}

	// 首页补齐：仅 1 次，且后续请求 Cookie 已携带 LIVE_BUVID。
	if n := counts.get("/"); n != 1 {
		t.Fatalf("首页应请求 1 次, 实际 %d", n)
	}
	if ck := mt.cookies["/xlive/web-ucenter/user/MedalWall"]; !strings.Contains(ck, "LIVE_BUVID=lbv-123") {
		t.Fatalf("MedalWall 请求应携带补齐后的 LIVE_BUVID: %q", ck)
	}
	// MedalWall 自动选择当前账号 UID。
	if got := mt.cookies["/xlive/web-ucenter/user/MedalWall"]; !strings.Contains(got, "DedeUserID=100") {
		t.Fatalf("MedalWall 应携带账号 Cookie: %q", got)
	}
	// 直播间信息不带 Cookie。
	if ck := mt.cookies["/room/v1/Room/get_info"]; ck != "" {
		t.Fatalf("get_info 不应携带 Cookie: %q", ck)
	}
	// 弹幕 1 次 + 点赞 1 次。
	if n := counts.get("/msg/send"); n != 1 {
		t.Fatalf("弹幕应 1 次, 实际 %d", n)
	}
	if n := counts.get("/xlive/app-ucenter/v1/like_info_v3/like/likeReportV3"); n != 1 {
		t.Fatalf("点赞应 1 次, 实际 %d", n)
	}
	if got := mt.forms["/msg/send"].Get("msg"); got != "OvO" {
		t.Fatalf("弹幕内容 = %q, want OvO", got)
	}
	if got := mt.forms["/xlive/app-ucenter/v1/like_info_v3/like/likeReportV3"].Get("click_time"); got != "30" {
		t.Fatalf("点赞 click_time = %q, want 30", got)
	}
	// 心跳：E（seq=0）→ X（seq=1，携带签名）。
	if n := counts.get("/xlive/data-interface/v1/x25Kn/E"); n != 1 {
		t.Fatalf("首包 E 应 1 次, 实际 %d", n)
	}
	if n := counts.get("/xlive/data-interface/v1/x25Kn/X"); n != 1 {
		t.Fatalf("后续包 X 应 1 次, 实际 %d", n)
	}
	eForm := mt.forms["/xlive/data-interface/v1/x25Kn/E"]
	if eForm.Get("id") != "[1,2,0,1001]" {
		t.Fatalf("E 包 id = %q, want [1,2,0,1001]", eForm.Get("id"))
	}
	if eForm.Get("ruid") != "2002" || eForm.Get("heart_beat") != "[]" {
		t.Fatalf("E 包 ruid/heart_beat 错误: %s", eForm.Encode())
	}
	if dev := eForm.Get("device"); !strings.Contains(dev, "lbv-123") {
		t.Fatalf("E 包 device 应含补齐后的 LIVE_BUVID: %q", dev)
	}
	if eForm.Get("s") != "" {
		t.Fatalf("E 包不应有签名: %q", eForm.Get("s"))
	}
	xForm := mt.forms["/xlive/data-interface/v1/x25Kn/X"]
	if xForm.Get("id") != "[1,2,1,1001]" {
		t.Fatalf("X 包 id = %q（seq 应从 0 递增到 1）", xForm.Get("id"))
	}
	if xForm.Get("ets") != "100" || xForm.Get("benchmark") != "skey" || xForm.Get("time") != "60" {
		t.Fatalf("X 包签名参数错误: %s", xForm.Encode())
	}
	// 用请求中的 ts/uuid 复算签名，验证 HMAC 链端到端正确。
	dev := xForm.Get("device")
	parts := strings.Split(strings.Trim(dev, "[]"), ",")
	if len(parts) != 2 {
		t.Fatalf("device 解析失败: %q", dev)
	}
	uuid := strings.Trim(parts[1], `"`)
	ts, err := strconv.ParseInt(xForm.Get("ts"), 10, 64)
	if err != nil {
		t.Fatalf("ts 解析失败: %v", err)
	}
	want, err := (bilibili.LiveHeartBeatCrypto{}).Sign(bilibili.HeartBeatPayload{
		Platform: "web", ParentID: 1, AreaID: 2, SeqID: 1, RoomID: 1001,
		Buvid: "lbv-123", UUID: uuid, Ets: 100, Time: 60, Ts: ts,
	}, "skey", []int{0, 2})
	if err != nil {
		t.Fatalf("Sign error: %v", err)
	}
	if got := xForm.Get("s"); got != want {
		t.Fatalf("X 包签名不一致:\n got: %s\nwant: %s", got, want)
	}
}

// TestLiveFansMedalExistingLiveBuvid 已有 LIVE_BUVID：不请求首页。
func TestLiveFansMedalExistingLiveBuvid(t *testing.T) {
	cfg := medalTestConfig(nil)
	counts := newReqCounts()
	mt := newMedalTransport(t, counts)
	mt.medalWallBody = medalWallBody()
	mt.roomInfoBody = roomInfoBody(1001, 2, 1, 1, 2002)
	client := newMedalClient(t, mt)

	res := runMedal(t, cfg, client, medalTestCookieWithBuvid(),
		func(lt *LiveFansMedalTask) { lt.heartBeatNumber = 1; lt.beatInterval = 0 },
	)
	if !res.Accounts[0].Success {
		t.Fatalf("应成功: %+v", res.Accounts[0].Steps)
	}
	if n := counts.get("/"); n != 0 {
		t.Fatalf("已有 LIVE_BUVID 不应请求首页, 实际 %d", n)
	}
}

// TestLiveFansMedalSkipLevel20 粉丝牌等级 >= 20 跳过：不查房间、不弹幕/点赞/心跳。
func TestLiveFansMedalSkipLevel20(t *testing.T) {
	cfg := medalTestConfig(nil)
	counts := newReqCounts()
	mt := newMedalTransport(t, counts)
	mt.homeSetCookie = "LIVE_BUVID=lbv-123; Path=/"
	mt.medalWallBody = medalWallBody(medalItem(1, 25, 1001, 2002))
	mt.roomInfoBody = roomInfoBody(1001, 2, 1, 1, 2002)
	client := newMedalClient(t, mt)

	ar := runMedal(t, cfg, client, medalTestCookie()).Accounts[0]
	if !ar.Success {
		t.Fatalf("应成功: %+v", ar.Steps)
	}
	if n := counts.get("/room/v1/Room/get_info"); n != 0 {
		t.Fatalf("等级>=20 不应查直播间信息, 实际 %d", n)
	}
	if n := counts.get("/msg/send"); n != 0 {
		t.Fatalf("不应发弹幕, 实际 %d", n)
	}
	if n := counts.get("/xlive/app-ucenter/v1/like_info_v3/like/likeReportV3"); n != 0 {
		t.Fatalf("不应点赞, 实际 %d", n)
	}
	if n := counts.get("/xlive/data-interface/v1/x25Kn/E"); n != 0 {
		t.Fatalf("不应心跳, 实际 %d", n)
	}
	for _, name := range []string{"发送弹幕", "点赞直播间", "直播时长挂机"} {
		if s := findStep(&ar, name); s == nil || s.Status != "skip" {
			t.Fatalf("无目标时步骤 %s 应 skip: %+v", name, ar.Steps)
		}
	}
}

// TestLiveFansMedalOfflineRoom 未开播直播间：仅弹幕，不点赞/心跳。
func TestLiveFansMedalOfflineRoom(t *testing.T) {
	cfg := medalTestConfig(nil)
	counts := newReqCounts()
	mt := newMedalTransport(t, counts)
	mt.homeSetCookie = "LIVE_BUVID=lbv-123; Path=/"
	mt.medalWallBody = medalWallBody(medalItem(0, 5, 1001, 2002))
	mt.roomInfoBody = roomInfoBody(1001, 2, 1, 0, 2002)
	client := newMedalClient(t, mt)

	ar := runMedal(t, cfg, client, medalTestCookie()).Accounts[0]
	if !ar.Success {
		t.Fatalf("应成功: %+v", ar.Steps)
	}
	if n := counts.get("/msg/send"); n != 1 {
		t.Fatalf("未开播房间仍应发弹幕, 实际 %d", n)
	}
	if n := counts.get("/xlive/app-ucenter/v1/like_info_v3/like/likeReportV3"); n != 0 {
		t.Fatalf("未开播不应点赞, 实际 %d", n)
	}
	if n := counts.get("/xlive/data-interface/v1/x25Kn/E"); n != 0 {
		t.Fatalf("未开播不应心跳, 实际 %d", n)
	}
}

// timedBeatTransport 记录心跳请求到达时间（节流窗口断言用）；其他路径一律 500。
type timedBeatTransport struct {
	mu    sync.Mutex
	times []time.Time
	body  string // 心跳响应体
}

func (t *timedBeatTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.Contains(r.URL.Path, "x25Kn") {
		t.mu.Lock()
		t.times = append(t.times, time.Now())
		t.mu.Unlock()
		return jsonResponse(t.body), nil
	}
	return serverErrorResponse(), nil
}

func (t *timedBeatTransport) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.times)
}

// newHeartBeatLoopTask 构造只跑心跳循环的任务（跳过登录/粉丝牌等前置步骤）。
func newHeartBeatLoopTask(cfg *model.Config, client *bilibili.BiliClient, ck *model.Cookie) *LiveFansMedalTask {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewLiveFansMedalTask(cfg, client, []*model.Cookie{ck}, logger).(*LiveFansMedalTask)
}

// singleMedalRoom 单个开播直播间（心跳循环直接使用的目标）。
func singleMedalRoom() []medalRoom {
	return []medalRoom{{
		roomID:   1001,
		roomInfo: &bilibili.RoomInfo{RoomID: 1001, AreaID: 2, ParentAreaID: 1, LiveStatus: 1, Uid: 2002},
		medal:    bilibili.MedalWallItem{TargetName: "主播A"},
	}}
}

// TestLiveFansMedalHeartBeatThrottleWindow 65s 节流窗口：窗口内不发出第 2 个心跳包；
// 取消后循环快速退出。
func TestLiveFansMedalHeartBeatThrottleWindow(t *testing.T) {
	cfg := medalTestConfig(nil) // IntervalSeconds=0，paceHeartBeat 不生效
	ck := medalTestCookieWithBuvid()
	rt := &timedBeatTransport{body: heartBeatBody("skey", "0,2", 100)}
	client := newMedalClient(t, rt)
	tt := newHeartBeatLoopTask(cfg, client, ck)
	tt.heartBeatNumber = 2 // 需 2 包才完成 → 第 2 包落在 65s 窗口内
	tt.beatInterval = 65 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tt.heartBeatRooms(ctx, ck, singleMedalRoom()) }()

	time.Sleep(3 * time.Second) // 首包立即发出后进入 65s 节流休眠
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消后应返回 context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ctx 取消后心跳循环未退出")
	}
	if n := rt.count(); n != 1 {
		t.Fatalf("65s 节流窗口内应仅发送 1 个心跳包, 实际 %d", n)
	}
}

// TestLiveFansMedalHeartBeatThrottleSpacing 相邻心跳间隔 >= beatInterval（2s 可观测窗口）。
func TestLiveFansMedalHeartBeatThrottleSpacing(t *testing.T) {
	cfg := medalTestConfig(nil)
	ck := medalTestCookieWithBuvid()
	rt := &timedBeatTransport{body: heartBeatBody("skey", "0,2", 100)}
	client := newMedalClient(t, rt)
	tt := newHeartBeatLoopTask(cfg, client, ck)
	tt.heartBeatNumber = 2
	tt.beatInterval = 2 * time.Second

	if err := tt.heartBeatRooms(context.Background(), ck, singleMedalRoom()); err != nil {
		t.Fatalf("heartBeatRooms 失败: %v", err)
	}
	if n := rt.count(); n != 2 {
		t.Fatalf("heartBeatNumber=2 应恰好 2 个心跳包, 实际 %d", n)
	}
	gap := rt.times[1].Sub(rt.times[0])
	if gap < 1700*time.Millisecond {
		t.Fatalf("相邻心跳间隔应 >= 2s（节流窗口）, 实际 %v", gap)
	}
}

// TestLiveFansMedalHeartBeatCancelDuringThrottleSleep 节流休眠中 ctx 取消：
// 立即退出且不再发包（不阻塞 65s、不空转）。
func TestLiveFansMedalHeartBeatCancelDuringThrottleSleep(t *testing.T) {
	cfg := medalTestConfig(nil)
	ck := medalTestCookieWithBuvid()
	rt := &timedBeatTransport{body: heartBeatBody("skey", "0,2", 100)}
	client := newMedalClient(t, rt)
	tt := newHeartBeatLoopTask(cfg, client, ck)
	tt.heartBeatNumber = 1_000_000 // 永不达标，循环只能靠 ctx 退出
	tt.beatInterval = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tt.heartBeatRooms(ctx, ck, singleMedalRoom()) }()

	time.Sleep(300 * time.Millisecond) // 首包已发，正处于 5s 节流休眠
	cancel()
	start := time.Now()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消后应返回 context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("节流休眠中 ctx 取消后 3s 内未退出（节流休眠不可中断？）")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("取消后退出过慢: %v", d)
	}
	if n := rt.count(); n != 1 {
		t.Fatalf("取消后不应再发送心跳包, 实际 %d", n)
	}
}

// TestLiveFansMedalHeartBeatGiveUp 心跳连续失败 5 次后放弃该直播间并退出。
func TestLiveFansMedalHeartBeatGiveUp(t *testing.T) {
	cfg := medalTestConfig(nil)
	counts := newReqCounts()
	mt := newMedalTransport(t, counts)
	mt.homeSetCookie = "LIVE_BUVID=lbv-123; Path=/"
	mt.medalWallBody = medalWallBody(medalItem(1, 5, 1001, 2002))
	mt.roomInfoBody = roomInfoBody(1001, 2, 1, 1, 2002)
	mt.heartBeatCode = 1 // 心跳接口返回业务错误
	client := newMedalClient(t, mt)

	ar := runMedal(t, cfg, client, medalTestCookie(),
		func(lt *LiveFansMedalTask) { lt.heartBeatNumber = 100; lt.beatInterval = 0 },
	).Accounts[0]
	if !ar.Success {
		t.Fatalf("应成功（心跳放弃不算任务失败）: %+v", ar.Steps)
	}
	if n := counts.get("/xlive/data-interface/v1/x25Kn/E"); n != 5 {
		t.Fatalf("连续失败 5 次应放弃, E 实际 %d 次", n)
	}
	if n := counts.get("/xlive/data-interface/v1/x25Kn/X"); n != 0 {
		t.Fatalf("首包失败不应发 X 包, 实际 %d", n)
	}
}

// TestLiveFansMedalAbortOnCookieInvalid nav=-101：登录失败中止该账号，后续零请求。
func TestLiveFansMedalAbortOnCookieInvalid(t *testing.T) {
	cfg := medalTestConfig(nil)
	counts := newReqCounts()
	mt := newMedalTransport(t, counts)
	mt.navCode = -101
	client := newMedalClient(t, mt)

	ar := runMedal(t, cfg, client, medalTestCookie()).Accounts[0]
	if ar.Success {
		t.Fatal("Cookie 失效时账号应标记失败")
	}
	if len(ar.Steps) != 1 {
		t.Fatalf("账号中止后应只有登录验证步骤, got %+v", ar.Steps)
	}
	if n := counts.get("/"); n != 0 {
		t.Fatalf("登录失败后不应请求首页, 实际 %d", n)
	}
	if n := counts.get("/xlive/web-ucenter/user/MedalWall"); n != 0 {
		t.Fatalf("登录失败后不应查粉丝牌, 实际 %d", n)
	}
}

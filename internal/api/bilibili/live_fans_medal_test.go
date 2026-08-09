package bilibili

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// liveBuvidTestCookie 含 LIVE_BUVID 的测试 Cookie。
func liveBuvidTestCookie() *model.Cookie {
	ck, err := model.ParseCookie("DedeUserID=100; SESSDATA=test-session; bili_jct=test-jct; LIVE_BUVID=test-buvid")
	if err != nil {
		panic(err)
	}
	return ck
}

// TestGetMedalWall 粉丝勋章墙：target_id/page/page_size、Referer/Origin、响应解析。
func TestGetMedalWall(t *testing.T) {
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/xlive/web-ucenter/user/MedalWall" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("target_id") != "100" || q.Get("page") != "1" || q.Get("page_size") != "50" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		if got := r.Header.Get("Referer"); got != "https://live.bilibili.com/" {
			t.Errorf("Referer = %q", got)
		}
		if got := r.Header.Get("Origin"); got != "https://live.bilibili.com" {
			t.Errorf("Origin = %q", got)
		}
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"list":[{"live_status":1,"target_name":"主播A","link":"//live.bilibili.com/1001","medal_info":{"medal_name":"A牌","medal_id":7,"target_id":2002,"level":5}}]}}`), nil
	}))

	items, err := c.GetMedalWall(context.Background(), biliTestCookie(), 100, 1, 50)
	if err != nil {
		t.Fatalf("GetMedalWall error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	it := items[0]
	if it.LiveStatus != 1 || it.TargetName != "主播A" || it.Link != "//live.bilibili.com/1001" {
		t.Fatalf("unexpected item: %+v", it)
	}
	if it.MedalInfo.MedalName != "A牌" || it.MedalInfo.MedalID != 7 || it.MedalInfo.TargetID != 2002 || it.MedalInfo.Level != 5 {
		t.Fatalf("unexpected medal info: %+v", it.MedalInfo)
	}
}

// TestGetRoomInfoNoCookie 直播间信息：room_id/from 参数，且不带 Cookie。
func TestGetRoomInfoNoCookie(t *testing.T) {
	var gotCookie string
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/room/v1/Room/get_info" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("room_id") != "1001" || q.Get("from") != "room" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		gotCookie = r.Header.Get("Cookie")
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"room_id":1001,"area_id":2,"parent_area_id":1,"live_status":1,"uid":2002}}`), nil
	}))

	info, err := c.GetRoomInfo(context.Background(), 1001)
	if err != nil {
		t.Fatalf("GetRoomInfo error: %v", err)
	}
	if gotCookie != "" {
		t.Fatalf("get_info 不应携带 Cookie: %q", gotCookie)
	}
	if info.RoomID != 1001 || info.AreaID != 2 || info.ParentAreaID != 1 || info.LiveStatus != 1 || info.Uid != 2002 {
		t.Fatalf("unexpected info: %+v", info)
	}
}

// TestSendDanmaku 弹幕端点：bubble/msg/color/mode/fontsize/rnd/roomid/csrf/csrf_token。
func TestSendDanmaku(t *testing.T) {
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/msg/send" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		p := r.PostForm
		if p.Get("bubble") != "0" || p.Get("msg") != "OvO" || p.Get("color") != "16777215" ||
			p.Get("mode") != "1" || p.Get("fontsize") != "25" || p.Get("rnd") != danmakuRnd {
			t.Errorf("unexpected danmaku fields: %s", r.PostForm.Encode())
		}
		if p.Get("roomid") != "1001" {
			t.Errorf("roomid = %q", p.Get("roomid"))
		}
		if p.Get("csrf") != "test-jct" || p.Get("csrf_token") != "test-jct" {
			t.Errorf("csrf 字段错误: %s", r.PostForm.Encode())
		}
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":null}`), nil
	}))

	if err := c.SendDanmaku(context.Background(), biliTestCookie(), 1001, "OvO"); err != nil {
		t.Fatalf("SendDanmaku error: %v", err)
	}
}

// TestLikeRoom 点赞端点：raw form 字符串字段序与值、Referer/Origin。
func TestLikeRoom(t *testing.T) {
	var gotBody string
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/xlive/app-ucenter/v1/like_info_v3/like/likeReportV3" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		gotBody = string(body)
		if got := r.Header.Get("Referer"); got != "https://live.bilibili.com/" {
			t.Errorf("Referer = %q", got)
		}
		if got := r.Header.Get("Origin"); got != "https://live.bilibili.com" {
			t.Errorf("Origin = %q", got)
		}
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":null}`), nil
	}))

	if err := c.LikeRoom(context.Background(), biliTestCookie(), 1001, 2002, 30); err != nil {
		t.Fatalf("LikeRoom error: %v", err)
	}
	want := "click_time=30&room_id=1001&uid=100&anchor_id=2002&csrf_token=test-jct&csrf=test-jct"
	if gotBody != want {
		t.Fatalf("raw form 错误:\n got: %s\nwant: %s", gotBody, want)
	}
}

// TestSendHeartbeatEnter 首包 X25Kn/E：seq=0 无签名，id 数组 [parent,area,0,room]、
// ruid/is_patch/heart_beat/ua/device/visit_id。
func TestSendHeartbeatEnter(t *testing.T) {
	ck := liveBuvidTestCookie()
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/xlive/data-interface/v1/x25Kn/E" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		p := r.PostForm
		if p.Get("id") != "[1,2,0,1001]" {
			t.Errorf("id = %q, want [1,2,0,1001]", p.Get("id"))
		}
		if p.Get("ruid") != "2002" {
			t.Errorf("ruid = %q", p.Get("ruid"))
		}
		if p.Get("is_patch") != "0" || p.Get("heart_beat") != "[]" || p.Get("visit_id") != "" {
			t.Errorf("unexpected fields: %s", p.Encode())
		}
		if p.Get("ua") != "test" {
			t.Errorf("ua = %q", p.Get("ua"))
		}
		if _, err := strconv.ParseInt(p.Get("ts"), 10, 64); err != nil {
			t.Errorf("ts 应为 Unix 毫秒: %q", p.Get("ts"))
		}
		if p.Get("device") != `["test-buvid","test-uuid"]` {
			t.Errorf("device = %q", p.Get("device"))
		}
		if p.Get("s") != "" {
			t.Errorf("首包不应有签名 s: %q", p.Get("s"))
		}
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"heartbeat_interval":60,"secret_key":"skey","secret_rule":[0,2],"timestamp":123456}}`), nil
	}))

	room := &RoomInfo{RoomID: 1001, AreaID: 2, ParentAreaID: 1, LiveStatus: 1, Uid: 2002}
	data, err := c.SendHeartbeat(context.Background(), ck, room, 0, "test-uuid", nil)
	if err != nil {
		t.Fatalf("SendHeartbeat(E) error: %v", err)
	}
	if data.SecretKey != "skey" || len(data.SecretRule) != 2 || data.Timestamp != 123456 {
		t.Fatalf("unexpected data: %+v", data)
	}
}

// TestSendHeartbeatX 后续包 X25Kn/X：seq 递增（id 数组 [1,2,1,1001]）、
// s 签名可复算（固定键序 JSON + HMAC 链）、ets/benchmark/time 字段。
func TestSendHeartbeatX(t *testing.T) {
	ck := liveBuvidTestCookie()
	var gotS, gotTS, gotID string
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/xlive/data-interface/v1/x25Kn/X" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		p := r.PostForm
		gotID, gotS, gotTS = p.Get("id"), p.Get("s"), p.Get("ts")
		if p.Get("ets") != "123456" {
			t.Errorf("ets = %q, want 123456（上一次响应 timestamp）", p.Get("ets"))
		}
		if p.Get("benchmark") != "skey" {
			t.Errorf("benchmark = %q", p.Get("benchmark"))
		}
		if p.Get("time") != "60" {
			t.Errorf("time = %q", p.Get("time"))
		}
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"heartbeat_interval":60,"secret_key":"skey2","secret_rule":[1],"timestamp":789}}`), nil
	}))

	room := &RoomInfo{RoomID: 1001, AreaID: 2, ParentAreaID: 1, LiveStatus: 1, Uid: 2002}
	prev := &HeartBeatData{SecretKey: "skey", SecretRule: []int{0}, Timestamp: 123456}
	data, err := c.SendHeartbeat(context.Background(), ck, room, 1, "test-uuid", prev)
	if err != nil {
		t.Fatalf("SendHeartbeat(X) error: %v", err)
	}
	if data.SecretKey != "skey2" || data.Timestamp != 789 {
		t.Fatalf("unexpected data: %+v", data)
	}
	if gotID != "[1,2,1,1001]" {
		t.Fatalf("id = %q（seq 应从 0 递增到 1）", gotID)
	}
	if gotS == "" {
		t.Fatal("后续包应有签名 s")
	}
	// 用请求时刻 ts 复算签名，验证 HMAC 链与键序。
	ts, err := strconv.ParseInt(gotTS, 10, 64)
	if err != nil {
		t.Fatalf("ts 解析失败: %v", err)
	}
	text, err := json.Marshal(HeartBeatPayload{
		Platform: "web", ParentID: 1, AreaID: 2, SeqID: 1, RoomID: 1001,
		Buvid: "test-buvid", UUID: "test-uuid", Ets: 123456, Time: 60, Ts: ts,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := (LiveHeartBeatCrypto{}).Sypder(string(text), []int{0}, "skey")
	if err != nil {
		t.Fatalf("sypder: %v", err)
	}
	if gotS != want {
		t.Fatalf("签名不一致:\n got: %s\nwant: %s", gotS, want)
	}
}

// TestSendHeartbeatNullData 响应 data 为 null：返回 (nil, nil)（包算成功但不更新签名三元组）。
func TestSendHeartbeatNullData(t *testing.T) {
	ck := liveBuvidTestCookie()
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":null}`), nil
	}))

	room := &RoomInfo{RoomID: 1001, AreaID: 2, ParentAreaID: 1, LiveStatus: 1, Uid: 2002}
	data, err := c.SendHeartbeat(context.Background(), ck, room, 0, "test-uuid", nil)
	if err != nil {
		t.Fatalf("SendHeartbeat error: %v", err)
	}
	if data != nil {
		t.Fatalf("data null 应返回 nil: %+v", data)
	}
}

// TestHasLiveBuvid LIVE_BUVID 判定。
func TestHasLiveBuvid(t *testing.T) {
	if HasLiveBuvid(biliTestCookie()) {
		t.Fatal("无 LIVE_BUVID 不应判定存在")
	}
	if !HasLiveBuvid(liveBuvidTestCookie()) {
		t.Fatal("应判定存在 LIVE_BUVID")
	}
}

// TestEnsureLiveBuvidMergesSetCookie 缺失 LIVE_BUVID 时访问首页并合并 Set-Cookie。
func TestEnsureLiveBuvidMergesSetCookie(t *testing.T) {
	ck := biliTestCookie()
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/" {
			t.Errorf("应请求首页, got path: %s", r.URL.Path)
		}
		resp := jsonResponse(`{"code":0,"message":"0","ttl":1,"data":null}`)
		resp.Header.Set("Set-Cookie", "LIVE_BUVID=lbv-123; Path=/")
		return resp, nil
	}))

	ok, err := c.EnsureLiveBuvid(context.Background(), ck)
	if err != nil {
		t.Fatalf("EnsureLiveBuvid error: %v", err)
	}
	if !ok {
		t.Fatal("补齐后应包含 LIVE_BUVID")
	}
	if !HasLiveBuvid(ck) {
		t.Fatal("cookie 应已合并 LIVE_BUVID")
	}
	if !strings.Contains(ck.String(), "LIVE_BUVID=lbv-123") {
		t.Fatalf("cookie 串应含 LIVE_BUVID=lbv-123: %q", ck.String())
	}
}

// TestEnsureLiveBuvidAlreadyPresent 已含 LIVE_BUVID 时不再请求首页。
func TestEnsureLiveBuvidAlreadyPresent(t *testing.T) {
	ck := liveBuvidTestCookie()
	hit := false
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		hit = true
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":null}`), nil
	}))

	ok, err := c.EnsureLiveBuvid(context.Background(), ck)
	if err != nil {
		t.Fatalf("EnsureLiveBuvid error: %v", err)
	}
	if !ok {
		t.Fatal("应返回 ok=true")
	}
	if hit {
		t.Fatal("已含 LIVE_BUVID 不应发起请求")
	}
}

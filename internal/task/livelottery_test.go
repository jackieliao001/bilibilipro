package task

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/jackieliao001/bilibilipro/internal/api/bilibili"
	"github.com/jackieliao001/bilibilipro/internal/model"
)

// lotteryTestConfig 构造天选抽奖测试配置。
func lotteryTestConfig(modify func(*model.LiveLotteryConfig)) *model.Config {
	lc := model.LiveLotteryConfig{Enabled: true, NumberOfDraw: 2, FollowGroupName: "天选时刻"}
	if modify != nil {
		modify(&lc)
	}
	return &model.Config{
		Bilibili: model.BilibiliConfig{IntervalSeconds: 0, UserAgent: "test"},
		Tasks:    model.TasksConfig{LiveLottery: lc},
	}
}

// newLotteryClient 构造注入 mock transport 的 BiliClient。
func newLotteryClient(t *testing.T, rt http.RoundTripper) *bilibili.BiliClient {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := bilibili.New(&model.Config{Bilibili: model.BilibiliConfig{IntervalSeconds: 0, UserAgent: "test"}}, logger)
	bilibili.SetTransportForTest(client, rt)
	return client
}

// runLottery 执行一次完整的天选抽奖任务（单账号）。
func runLottery(t *testing.T, cfg *model.Config, client *bilibili.BiliClient) *model.TaskResult {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	task := NewLiveLotteryTask(cfg, client, []*model.Cookie{dailyTestCookie()}, logger)
	res, err := task.Run(context.Background())
	if err != nil {
		t.Fatalf("LiveLotteryTask.Run 失败: %v", err)
	}
	if len(res.Accounts) != 1 {
		t.Fatalf("应产生 1 个账号结果, got %d", len(res.Accounts))
	}
	return res
}

// lotteryNavBody 构造 nav 登录响应。
func lotteryNavBody(code int) string {
	if code != 0 {
		return `{"code":` + strconv.Itoa(code) + `,"message":"未登录","ttl":1,"data":null}`
	}
	return `{"code":0,"message":"0","ttl":1,"data":{"isLogin":true,"uname":"测试用户","money":100,"level_info":{"current_level":5},"wbi_img":{"img_url":"","sub_url":""}}}`
}

// lotteryTransport 天选抽奖 mock transport。
// followings 为依次返回的关注列表 JSON（第 1 次=抽奖前取最近关注，第 2 次=分组时取新增关注）。
type lotteryTransport struct {
	t           *testing.T
	counts      *reqCounts
	areasBody   string
	pages       map[string]string // "areaID_page" → getList 响应
	checks      map[string]string // roomid → Check 响应
	tagsBody    string
	createTag   string
	followings  []string
	joinCode    int
	copyCode    int
	joinBodies  []string
	copyBodies  []string
	followTimes int
}

func (m *lotteryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	path := r.URL.Path
	m.counts.inc(path)
	switch path {
	case "/x/web-interface/nav":
		return jsonResponse(lotteryNavBody(0)), nil
	case "/xlive/web-interface/v1/index/getWebAreaList":
		return jsonResponse(m.areasBody), nil
	case "/xlive/web-interface/v1/second/getList":
		key := r.URL.Query().Get("parent_area_id") + "_" + r.URL.Query().Get("page")
		body, ok := m.pages[key]
		if !ok {
			m.t.Errorf("unexpected getList page: %s", key)
			return serverErrorResponse(), nil
		}
		return jsonResponse(body), nil
	case "/xlive/lottery-interface/v1/Anchor/Check":
		body, ok := m.checks[r.URL.Query().Get("roomid")]
		if !ok {
			m.t.Errorf("unexpected Check roomid: %s", r.URL.Query().Get("roomid"))
			return serverErrorResponse(), nil
		}
		return jsonResponse(body), nil
	case "/xlive/lottery-interface/v1/Anchor/Join":
		if err := r.ParseForm(); err != nil {
			m.t.Fatalf("parse form: %v", err)
		}
		m.joinBodies = append(m.joinBodies, r.PostForm.Encode())
		return jsonResponse(`{"code":` + strconv.Itoa(m.joinCode) + `,"message":"m","ttl":1,"data":null}`), nil
	case "/x/relation/followings":
		if m.followTimes >= len(m.followings) {
			m.t.Errorf("followings 请求超出预期次数")
			return serverErrorResponse(), nil
		}
		body := m.followings[m.followTimes]
		m.followTimes++
		return jsonResponse(body), nil
	case "/x/relation/tags":
		return jsonResponse(m.tagsBody), nil
	case "/x/relation/tag/create":
		if err := r.ParseForm(); err != nil {
			m.t.Fatalf("parse form: %v", err)
		}
		return jsonResponse(m.createTag), nil
	case "/x/relation/tags/copyUsers":
		if err := r.ParseForm(); err != nil {
			m.t.Fatalf("parse form: %v", err)
		}
		m.copyBodies = append(m.copyBodies, r.PostForm.Encode())
		return jsonResponse(`{"code":` + strconv.Itoa(m.copyCode) + `,"message":"m","ttl":1,"data":null}`), nil
	default:
		m.t.Errorf("unexpected request path: %s", path)
		return serverErrorResponse(), nil
	}
}

// checkBody 构造 Check 响应。
func checkBody(id int64, status, requireType, giftPrice int, awardName string) string {
	return `{"code":0,"message":"0","ttl":1,"data":{"id":` + strconv.FormatInt(id, 10) +
		`,"room_id":0,"status":` + strconv.Itoa(status) +
		`,"award_name":"` + awardName + `","award_num":1,"require_type":` + strconv.Itoa(requireType) +
		`,"require_value":0,"require_text":"条件","gift_id":0,"gift_name":"礼物","gift_num":1,"gift_price":` + strconv.Itoa(giftPrice) + `}}`
}

// pendantRoomBody 构造带天选角标的房间 JSON。
func pendantRoomBody(roomID, uid int64, title string) string {
	return `{"roomid":` + strconv.FormatInt(roomID, 10) + `,"uid":` + strconv.FormatInt(uid, 10) +
		`,"title":"` + title + `","uname":"up` + strconv.FormatInt(uid, 10) + `","Pendant_info":{"2":{"Pendent_id":504,"Content":"天选时刻"}}}`
}

// listBody 构造 getList 响应（含多个房间）。
func listBody(rooms string, hasMore int, sortType string) string {
	return `{"code":0,"message":"0","ttl":1,"data":{"new_tags":[{"id":1,"name":"x","sort_type":"` + sortType + `"}],"list":[` + rooms + `],"has_more":` + strconv.Itoa(hasMore) + `}}`
}

// TestLiveLotteryDisabled 未启用：跳过且不发起任何请求。
func TestLiveLotteryDisabled(t *testing.T) {
	cfg := lotteryTestConfig(func(lc *model.LiveLotteryConfig) { lc.Enabled = false })
	counts := newReqCounts()
	client := newLotteryClient(t, &lotteryTransport{t: t, counts: counts, areasBody: `{"code":0,"message":"0","ttl":1,"data":{"data":[]}}`})

	ar := runLottery(t, cfg, client).Accounts[0]
	step := findStep(&ar, "天选时刻抽奖")
	if step == nil || step.Status != "skip" || step.Message != "天选时刻抽奖未启用" {
		t.Fatalf("应 skip(未启用): %+v", ar.Steps)
	}
	for path, n := range counts.m {
		t.Fatalf("未启用时不应发起请求: %s x%d", path, n)
	}
}

// TestLiveLotteryFullFlow 全流程：扫描→过滤→Join×2→自动分组（已有分组）。
func TestLiveLotteryFullFlow(t *testing.T) {
	cfg := lotteryTestConfig(func(lc *model.LiveLotteryConfig) {
		lc.NumberOfDraw = 2
	})
	counts := newReqCounts()
	mt := &lotteryTransport{
		t:         t,
		counts:    counts,
		areasBody: `{"code":0,"message":"0","ttl":1,"data":{"data":[{"id":1,"name":"娱乐"}]}}`,
		pages:     map[string]string{"1_1": listBody(pendantRoomBody(1001, 1001, "房间A")+","+pendantRoomBody(1002, 1002, "房间B"), 0, "")},
		checks: map[string]string{
			"1001": checkBody(555, 1, 1, 0, "蓝牙耳机"),
			"1002": checkBody(556, 1, 1, 0, "蓝牙耳机"),
		},
		followings: []string{
			`{"code":0,"message":"0","ttl":1,"data":{"list":[{"mid":1,"uname":"旧关注"}]}}`,
			`{"code":0,"message":"0","ttl":1,"data":{"list":[{"mid":1001,"uname":"up1001"},{"mid":1002,"uname":"up1002"},{"mid":1,"uname":"旧关注"}]}}`,
		},
		tagsBody: `{"code":0,"message":"0","ttl":1,"data":[{"tagid":55,"name":"天选时刻","count":2}]}`,
		joinCode: 0,
		copyCode: 0,
	}
	client := newLotteryClient(t, mt)

	ar := runLottery(t, cfg, client).Accounts[0]
	if !ar.Success {
		t.Fatalf("全流程应成功: %+v", ar.Steps)
	}
	for _, name := range []string{"登录验证", "天选时刻抽奖", "自动分组关注的主播"} {
		if s := findStep(&ar, name); s == nil || s.Status != "ok" {
			t.Fatalf("步骤 %s 应 ok: %+v", name, ar.Steps)
		}
	}

	if n := counts.get("/xlive/lottery-interface/v1/Anchor/Join"); n != 2 {
		t.Fatalf("应 Join 2 次, 实际 %d", n)
	}
	if n := counts.get("/xlive/web-interface/v1/second/getList"); n != 1 {
		t.Fatalf("has_more=0 时 getList 应仅 1 次, 实际 %d", n)
	}
	// Join form 校验。
	for i, body := range mt.joinBodies {
		wantID := "555"
		if i == 1 {
			wantID = "556"
		}
		if !strings.Contains(body, "id="+wantID) || !strings.Contains(body, "platform=pc") ||
			!strings.Contains(body, "csrf=jct") || !strings.Contains(body, "csrf_token=jct") {
			t.Fatalf("Join form 错误[%d]: %s", i, body)
		}
	}
	// 分组校验：应把抽奖新增关注 1001,1002 批量移入已有分组 55。
	if n := counts.get("/x/relation/tags/copyUsers"); n != 1 {
		t.Fatalf("应 copyUsers 1 次, 实际 %d", n)
	}
	if len(mt.copyBodies) != 1 {
		t.Fatalf("copyUsers 应 1 次: %v", mt.copyBodies)
	}
	form, _ := url.ParseQuery(mt.copyBodies[0])
	if form.Get("fids") != "1001,1002" || form.Get("tagids") != "55" {
		t.Fatalf("copyUsers form 错误: %v", mt.copyBodies)
	}
	if n := counts.get("/x/relation/tag/create"); n != 0 {
		t.Fatalf("分组已存在不应创建, 实际 %d", n)
	}
}

// TestLiveLotteryNumberOfDrawEarlyExit NumberOfDraw=1：参与 1 次后停止，不再翻页。
func TestLiveLotteryNumberOfDrawEarlyExit(t *testing.T) {
	cfg := lotteryTestConfig(func(lc *model.LiveLotteryConfig) {
		lc.NumberOfDraw = 1
		lc.FollowGroupName = ""
	})
	counts := newReqCounts()
	mt := &lotteryTransport{
		t:         t,
		counts:    counts,
		areasBody: `{"code":0,"message":"0","ttl":1,"data":{"data":[{"id":1,"name":"娱乐"}]}}`,
		pages: map[string]string{
			"1_1": listBody(pendantRoomBody(1001, 1001, "房间A")+","+pendantRoomBody(1002, 1002, "房间B"), 1, ""),
			"1_2": listBody(pendantRoomBody(1003, 1003, "房间C"), 0, "hot"),
		},
		checks: map[string]string{
			"1001": checkBody(555, 1, 0, 0, "蓝牙耳机"),
			"1002": checkBody(556, 1, 0, 0, "蓝牙耳机"),
			"1003": checkBody(557, 1, 0, 0, "蓝牙耳机"),
		},
		joinCode: 0,
	}
	client := newLotteryClient(t, mt)

	ar := runLottery(t, cfg, client).Accounts[0]
	if !ar.Success {
		t.Fatalf("应成功: %+v", ar.Steps)
	}
	if n := counts.get("/xlive/lottery-interface/v1/Anchor/Join"); n != 1 {
		t.Fatalf("NumberOfDraw=1 应 Join 1 次, 实际 %d", n)
	}
	if n := counts.get("/xlive/web-interface/v1/second/getList"); n != 1 {
		t.Fatalf("达到次数后不应再翻页, getList 实际 %d 次", n)
	}
	if n := counts.get("/x/relation/followings"); n != 0 {
		t.Fatalf("FollowGroupName 为空时不应查关注列表, 实际 %d", n)
	}
}

// TestLiveLotteryFilters 过滤链：黑名单/已开奖/粉丝勋章/赠礼/奖品名排除/包含不满足。
func TestLiveLotteryFilters(t *testing.T) {
	cfg := lotteryTestConfig(func(lc *model.LiveLotteryConfig) {
		lc.NumberOfDraw = 0
		lc.FollowGroupName = ""
		lc.RetainUids = "1111,2222"
		lc.IncludeRewardName = "耳机"
		lc.ExcludeRewardName = "舰|船"
	})
	counts := newReqCounts()
	rooms := strings.Join([]string{
		pendantRoomBody(1111, 1111, "黑名单"),
		pendantRoomBody(2002, 2002, "已开奖"),
		pendantRoomBody(3003, 3003, "粉丝勋章"),
		pendantRoomBody(4004, 4004, "需赠礼"),
		pendantRoomBody(5005, 5005, "奖品被排除"),
		pendantRoomBody(6006, 6006, "可参与"),
		pendantRoomBody(7007, 7007, "奖品不匹配"),
	}, ",")
	mt := &lotteryTransport{
		t:         t,
		counts:    counts,
		areasBody: `{"code":0,"message":"0","ttl":1,"data":{"data":[{"id":1,"name":"娱乐"}]}}`,
		pages:     map[string]string{"1_1": listBody(rooms, 0, "")},
		checks: map[string]string{
			"2002": checkBody(1, 2, 0, 0, "任意"),
			"3003": checkBody(2, 1, 2, 0, "任意"),
			"4004": checkBody(3, 1, 0, 100, "任意"),
			"5005": checkBody(4, 1, 0, 0, "舰长专属礼物"),
			"6006": checkBody(5, 1, 0, 0, "蓝牙耳机"),
			"7007": checkBody(6, 1, 0, 0, "手机支架"),
		},
		joinCode: 0,
	}
	client := newLotteryClient(t, mt)

	ar := runLottery(t, cfg, client).Accounts[0]
	if !ar.Success {
		t.Fatalf("应成功: %+v", ar.Steps)
	}
	if n := counts.get("/xlive/lottery-interface/v1/Anchor/Join"); n != 1 {
		t.Fatalf("仅可参与的 6006 应 Join 1 次, 实际 %d", n)
	}
	if n := counts.get("/xlive/lottery-interface/v1/Anchor/Check"); n != 6 {
		t.Fatalf("黑名单房间不应 Check, Check 应 6 次, 实际 %d", n)
	}
	if len(mt.joinBodies) != 1 || !strings.Contains(mt.joinBodies[0], "id=5") {
		t.Fatalf("Join 的应为 6006 的抽奖 id=5: %v", mt.joinBodies)
	}
}

// TestLiveLotteryCreateGroupWhenMissing 分组不存在时自动创建再批量分组。
func TestLiveLotteryCreateGroupWhenMissing(t *testing.T) {
	cfg := lotteryTestConfig(func(lc *model.LiveLotteryConfig) {
		lc.NumberOfDraw = 1
	})
	counts := newReqCounts()
	mt := &lotteryTransport{
		t:         t,
		counts:    counts,
		areasBody: `{"code":0,"message":"0","ttl":1,"data":{"data":[{"id":1,"name":"娱乐"}]}}`,
		pages:     map[string]string{"1_1": listBody(pendantRoomBody(1001, 1001, "房间A"), 0, "")},
		checks:    map[string]string{"1001": checkBody(555, 1, 1, 0, "蓝牙耳机")},
		followings: []string{
			`{"code":0,"message":"0","ttl":1,"data":{"list":[{"mid":1,"uname":"旧关注"}]}}`,
			`{"code":0,"message":"0","ttl":1,"data":{"list":[{"mid":1001,"uname":"up1001"},{"mid":1,"uname":"旧关注"}]}}`,
		},
		tagsBody:  `{"code":0,"message":"0","ttl":1,"data":[]}`,
		createTag: `{"code":0,"message":"0","ttl":1,"data":{"tagid":99}}`,
		joinCode:  0,
		copyCode:  0,
	}
	client := newLotteryClient(t, mt)

	ar := runLottery(t, cfg, client).Accounts[0]
	if !ar.Success {
		t.Fatalf("应成功: %+v", ar.Steps)
	}
	if n := counts.get("/x/relation/tag/create"); n != 1 {
		t.Fatalf("应创建分组 1 次, 实际 %d", n)
	}
	if len(mt.copyBodies) != 1 || !strings.Contains(mt.copyBodies[0], "tagids=99") ||
		!strings.Contains(mt.copyBodies[0], "fids=1001") {
		t.Fatalf("copyUsers 应使用新分组 99: %v", mt.copyBodies)
	}
}

// TestLiveLotteryAreaFilter 按 AreaHostID 过滤分区。
func TestLiveLotteryAreaFilter(t *testing.T) {
	cfg := lotteryTestConfig(func(lc *model.LiveLotteryConfig) {
		lc.NumberOfDraw = 0
		lc.FollowGroupName = ""
		lc.AreaHostID = "2"
	})
	counts := newReqCounts()
	mt := &lotteryTransport{
		t:         t,
		counts:    counts,
		areasBody: `{"code":0,"message":"0","ttl":1,"data":{"data":[{"id":1,"name":"娱乐"},{"id":2,"name":"游戏"}]}}`,
		pages:     map[string]string{"2_1": listBody(pendantRoomBody(1001, 1001, "房间A"), 0, "")},
		checks:    map[string]string{"1001": checkBody(555, 1, 0, 0, "蓝牙耳机")},
		joinCode:  0,
	}
	client := newLotteryClient(t, mt)

	ar := runLottery(t, cfg, client).Accounts[0]
	if !ar.Success {
		t.Fatalf("应成功: %+v", ar.Steps)
	}
	if n := counts.get("/xlive/web-interface/v1/second/getList"); n != 1 {
		t.Fatalf("仅分区 2 应 getList 1 次, 实际 %d", n)
	}
	if n := counts.get("/xlive/lottery-interface/v1/Anchor/Join"); n != 1 {
		t.Fatalf("应 Join 1 次, 实际 %d", n)
	}
}

// TestLiveLotteryPaginationCap 分页上限：has_more 恒为 1 时每分区最多翻 5 页，不会无限翻页。
func TestLiveLotteryPaginationCap(t *testing.T) {
	cfg := lotteryTestConfig(func(lc *model.LiveLotteryConfig) {
		lc.NumberOfDraw = 0 // 不限参与次数，避免提前退出，专门验证页数上限
		lc.FollowGroupName = ""
	})
	counts := newReqCounts()
	pages := map[string]string{}
	for p := 1; p <= 5; p++ {
		pages["1_"+strconv.Itoa(p)] = listBody("", 1, "") // 空列表 + has_more=1
	}
	mt := &lotteryTransport{
		t:         t,
		counts:    counts,
		areasBody: `{"code":0,"message":"0","ttl":1,"data":{"data":[{"id":1,"name":"娱乐"}]}}`,
		pages:     pages,
	}
	client := newLotteryClient(t, mt)

	ar := runLottery(t, cfg, client).Accounts[0]
	if n := counts.get("/xlive/web-interface/v1/second/getList"); n != 5 {
		t.Fatalf("has_more 恒为 1 时每分区应最多请求 5 页, 实际 %d", n)
	}
	if s := findStep(&ar, "天选时刻抽奖"); s == nil || s.Status != "skip" {
		t.Fatalf("空列表应 skip: %+v", ar.Steps)
	}
}

// TestLiveLotteryAbortOnCookieInvalid nav=-101：登录失败中止该账号，后续零请求。
func TestLiveLotteryAbortOnCookieInvalid(t *testing.T) {
	cfg := lotteryTestConfig(nil)
	counts := newReqCounts()
	client := newLotteryClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		counts.inc(r.URL.Path)
		if r.URL.Path != "/x/web-interface/nav" {
			t.Errorf("Cookie 失效时不应发起请求: %s", r.URL.Path)
			return serverErrorResponse(), nil
		}
		return jsonResponse(lotteryNavBody(-101)), nil
	}))

	ar := runLottery(t, cfg, client).Accounts[0]
	if ar.Success {
		t.Fatal("Cookie 失效时账号应标记失败")
	}
	if len(ar.Steps) != 1 {
		t.Fatalf("账号中止后应只有登录验证步骤, got %+v", ar.Steps)
	}
	login := ar.Steps[0]
	if login.Name != "登录验证" || login.Status != "fail" || !strings.Contains(login.Message, "-101") {
		t.Fatalf("登录验证应 fail(-101): %+v", login)
	}
	if n := counts.get("/xlive/web-interface/v1/index/getWebAreaList"); n != 0 {
		t.Fatalf("登录失败后不应查分区, 实际 %d", n)
	}
}

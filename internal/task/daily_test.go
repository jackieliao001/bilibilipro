package task

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/raywangqvq/bilitoolgo/internal/api/bilibili"
	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// roundTripFunc 将函数适配为 http.RoundTripper，用于 mock 任意 URL 的响应
// （task 包内自建副本：bilibili 包的 roundTripFunc 不导出）。
type roundTripFunc func(r *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// jsonResponse 构造一个 200 JSON 响应。
func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// serverErrorResponse 构造一个 500 响应（未匹配路径时返回，触发 RetryDo 重试）。
func serverErrorResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

// reqCounts 请求计数（带锁，安全）。
type reqCounts struct {
	mu sync.Mutex
	m  map[string]int
}

func newReqCounts() *reqCounts { return &reqCounts{m: make(map[string]int)} }

func (c *reqCounts) inc(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[path]++
}

func (c *reqCounts) get(path string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[path]
}

// dailyTestConfig 构造每日任务测试配置。
// 三个开关默认为 nil（即开启）；需要显式关闭时在 modify 中设置 *bool。
func dailyTestConfig(modify func(*model.DailyConfig)) *model.Config {
	d := model.DailyConfig{
		Enabled:        true,
		DonateCoins:    1,
		ProtectCoins:   0,
		DevicePlatform: "android",
	}
	if modify != nil {
		modify(&d)
	}
	return &model.Config{Tasks: model.TasksConfig{Daily: d}}
}

// boolPtr 返回指向 b 的指针。
func boolPtr(b bool) *bool { return &b }

// mockDailyTransport 按 URL path 分发 mock 响应，并统计每个 path 的请求次数。
// money 同时用于 nav 与 getCoin 的余额；未匹配的 path 记为测试错误并返回 500。
func mockDailyTransport(t *testing.T, counts *reqCounts, money float64) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		counts.inc(path)
		switch path {
		case "/x/web-interface/nav":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"isLogin":true,"uname":"测试用户","money":` + strconv.FormatFloat(money, 'f', -1, 64) + `,"level_info":{"current_level":5},"wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/a.png","sub_url":"https://i0.hdslb.com/bfs/wbi/b.png"}}}`), nil
		case "/x/member/web/exp/reward":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"login":true,"watch":false,"share":false,"coins":0}}`), nil
		case "/x/web-interface/ranking/v2":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"list":[{"aid":1001,"bvid":"BV1","cid":10,"title":"测试视频"}]}}`), nil
		case "/x/click-interface/web/heartbeat":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":null}`), nil
		case "/x/web-interface/share/add":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":null}`), nil
		case "/x/web-interface/coin/today/exp":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":0}`), nil
		case "/site/getCoin":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"money":` + strconv.FormatFloat(money, 'f', -1, 64) + `}}`), nil
		case "/x/web-interface/archive/coins":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"multiply":0,"count":0}}`), nil
		case "/x/web-interface/coin/add":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"like":false}}`), nil
		case "/x/relation/followings":
			// 无关注列表（业务错误码），每日任务应回退到排行榜选视频。
			return jsonResponse(`{"code":-400,"message":"mock 无关注列表","ttl":1,"data":null}`), nil
		case "/xlive/revenue/v1/wallet/getStatus":
			return jsonResponse(`{"code":0,"message":"0","data":{"silver_2_coin_left":100}}`), nil
		case "/xlive/revenue/v1/wallet/silver2coin":
			return jsonResponse(`{"code":0,"message":"0","data":null}`), nil
		default:
			t.Errorf("unexpected request path: %s", path)
			return serverErrorResponse(), nil
		}
	})
}

// fmtFloat 输出 float64 的 JSON 数字表示。
func fmtFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// newDailyClient 构造注入 mock transport 的 BiliClient。
func newDailyClient(t *testing.T, rt http.RoundTripper) *bilibili.BiliClient {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := bilibili.New(&model.Config{Bilibili: model.BilibiliConfig{IntervalSeconds: 0, UserAgent: "test"}}, logger)
	bilibili.SetTransportForTest(client, rt)
	return client
}

// dailyTestCookie 测试 Cookie（Buvid3 非空，避免触发补 Cookie 步骤的首页请求）。
func dailyTestCookie() *model.Cookie {
	return &model.Cookie{DedeUserID: "100", SESSDATA: "s", BiliJCT: "jct", Buvid3: "b3"}
}

// runDaily 执行一次完整的每日任务（单账号），返回任务结果。
func runDaily(t *testing.T, cfg *model.Config, client *bilibili.BiliClient) *model.TaskResult {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	task := NewDailyTask(cfg, client, []*model.Cookie{dailyTestCookie()}, logger, "")
	res, err := task.Run(context.Background())
	if err != nil {
		t.Fatalf("DailyTask.Run 失败: %v", err)
	}
	if len(res.Accounts) != 1 {
		t.Fatalf("应产生 1 个账号结果, got %d", len(res.Accounts))
	}
	return res
}

// findStep 按名称查找步骤；不存在返回 nil。
func findStep(ar *model.AccountResult, name string) *model.StepResult {
	for i := range ar.Steps {
		if ar.Steps[i].Name == name {
			return &ar.Steps[i]
		}
	}
	return nil
}

// TestDailySkipWhenDonateCoinDisabled donate_coin=false：投币步骤跳过，AddCoin 从未发出。
func TestDailySkipWhenDonateCoinDisabled(t *testing.T) {
	cfg := dailyTestConfig(func(d *model.DailyConfig) {
		d.DonateCoin = boolPtr(false)
		d.DonateCoins = 5
	})
	counts := newReqCounts()
	client := newDailyClient(t, mockDailyTransport(t, counts, 100))

	ar := runDaily(t, cfg, client).Accounts[0]

	coin := findStep(&ar, "投币")
	if coin == nil {
		t.Fatalf("缺少投币步骤: %+v", ar.Steps)
	}
	if coin.Status != "skip" || coin.Message != "未开启投币" {
		t.Fatalf("投币步骤应 skip(未开启投币): %+v", coin)
	}
	if n := counts.get("/x/web-interface/coin/add"); n != 0 {
		t.Fatalf("投币关闭时 AddCoin 不应发出, 实际 %d 次", n)
	}
	if n := counts.get("/x/web-interface/coin/today/exp"); n != 0 {
		t.Fatalf("投币关闭时不应查询今日已投, 实际 %d 次", n)
	}
	if n := counts.get("/site/getCoin"); n != 0 {
		t.Fatalf("投币关闭时不应查询余额, 实际 %d 次", n)
	}
	// 其余步骤正常执行。
	for _, name := range []string{"登录验证", "观看视频", "分享视频"} {
		if s := findStep(&ar, name); s == nil || s.Status != "ok" {
			t.Fatalf("步骤 %s 应 ok: %+v", name, ar.Steps)
		}
	}
}

// TestDailySkipWhenWatchDisabled watch_video=false：观看步骤跳过、无 Heartbeat，
// 分享与投币正常执行。
func TestDailySkipWhenWatchDisabled(t *testing.T) {
	cfg := dailyTestConfig(func(d *model.DailyConfig) {
		d.WatchVideo = boolPtr(false)
	})
	counts := newReqCounts()
	client := newDailyClient(t, mockDailyTransport(t, counts, 100))

	ar := runDaily(t, cfg, client).Accounts[0]

	watch := findStep(&ar, "观看视频")
	if watch == nil {
		t.Fatalf("缺少观看步骤: %+v", ar.Steps)
	}
	if watch.Status != "skip" || watch.Message != "未开启观看视频" {
		t.Fatalf("观看步骤应 skip(未开启观看视频): %+v", watch)
	}
	if n := counts.get("/x/click-interface/web/heartbeat"); n != 0 {
		t.Fatalf("观看关闭时不应发送 Heartbeat, 实际 %d 次", n)
	}
	// 排行榜仅被分享与投币选视频触发（观看被跳过）：若观看开启应为 3 次。
	if n := counts.get("/x/web-interface/ranking/v2"); n != 2 {
		t.Fatalf("观看关闭时排行榜应仅 2 次（分享+投币选视频）, 实际 %d", n)
	}
	for _, name := range []string{"登录验证", "分享视频", "投币"} {
		if s := findStep(&ar, name); s == nil || s.Status != "ok" {
			t.Fatalf("步骤 %s 应 ok: %+v", name, ar.Steps)
		}
	}
}

// TestDailyAbortOnCookieInvalid nav 返回 code=-101：登录验证失败并中止该账号，
// 后续步骤不存在，AccountResult.Success=false。
func TestDailyAbortOnCookieInvalid(t *testing.T) {
	cfg := dailyTestConfig(nil)
	counts := newReqCounts()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := bilibili.New(&model.Config{Bilibili: model.BilibiliConfig{IntervalSeconds: 0, UserAgent: "test"}}, logger)
	bilibili.SetTransportForTest(client, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		counts.inc(r.URL.Path)
		if r.URL.Path != "/x/web-interface/nav" {
			t.Errorf("Cookie 失效时不应发起请求: %s", r.URL.Path)
			return serverErrorResponse(), nil
		}
		return jsonResponse(`{"code":-101,"message":"账号未登录","ttl":1,"data":null}`), nil
	}))

	res := runDaily(t, cfg, client)
	ar := res.Accounts[0]

	if ar.Success {
		t.Fatal("Cookie 失效时账号应标记失败")
	}
	if len(ar.Steps) != 1 {
		t.Fatalf("账号中止后应只有登录验证步骤, got %+v", ar.Steps)
	}
	login := ar.Steps[0]
	if login.Name != "登录验证" || login.Status != "fail" {
		t.Fatalf("登录验证应 fail: %+v", login)
	}
	if !strings.Contains(login.Message, "-101") || !strings.Contains(login.Message, "Cookie 已失效") {
		t.Fatalf("登录失败文案应提示 Cookie 失效与 -101: %q", login.Message)
	}
	for _, name := range []string{"观看视频", "分享视频", "投币"} {
		if s := findStep(&ar, name); s != nil {
			t.Fatalf("账号中止后不应存在步骤 %s: %+v", name, ar.Steps)
		}
	}
}

// TestDailySkipWhenNoCoins 余额为 0：投币步骤跳过（可投硬币不足），其余步骤正常。
func TestDailySkipWhenNoCoins(t *testing.T) {
	cfg := dailyTestConfig(func(d *model.DailyConfig) {
		d.DonateCoins = 1
	})
	counts := newReqCounts()
	client := newDailyClient(t, mockDailyTransport(t, counts, 0))

	ar := runDaily(t, cfg, client).Accounts[0]

	coin := findStep(&ar, "投币")
	if coin == nil {
		t.Fatalf("缺少投币步骤: %+v", ar.Steps)
	}
	if coin.Status != "skip" || !strings.Contains(coin.Message, "可投硬币不足") {
		t.Fatalf("余额为 0 时投币应 skip(可投硬币不足): %+v", coin)
	}
	if n := counts.get("/x/web-interface/coin/add"); n != 0 {
		t.Fatalf("余额不足时不应发 AddCoin, 实际 %d 次", n)
	}
	for _, name := range []string{"登录验证", "观看视频", "分享视频"} {
		if s := findStep(&ar, name); s == nil || s.Status != "ok" {
			t.Fatalf("步骤 %s 应 ok: %+v", name, ar.Steps)
		}
	}
}

// TestDailyFullFlow 全部开关开启 + 全部成功响应：观看/分享/投币均 ok，投币 1 枚。
func TestDailyFullFlow(t *testing.T) {
	cfg := dailyTestConfig(func(d *model.DailyConfig) {
		d.WatchVideo = boolPtr(true)
		d.ShareVideo = boolPtr(true)
		d.DonateCoin = boolPtr(true)
		d.DonateCoins = 1
	})
	counts := newReqCounts()
	client := newDailyClient(t, mockDailyTransport(t, counts, 100))

	ar := runDaily(t, cfg, client).Accounts[0]

	if !ar.Success {
		t.Fatalf("全流程应成功: %+v", ar.Steps)
	}
	for _, name := range []string{"登录验证", "观看视频", "分享视频", "投币"} {
		if s := findStep(&ar, name); s == nil || s.Status != "ok" {
			t.Fatalf("步骤 %s 应 ok: %+v", name, ar.Steps)
		}
	}
	if n := counts.get("/x/web-interface/coin/add"); n != 1 {
		t.Fatalf("应投币 1 次, 实际 %d 次", n)
	}
	if n := counts.get("/x/click-interface/web/heartbeat"); n != 2 {
		t.Fatalf("应发送 2 次 Heartbeat, 实际 %d 次", n)
	}
	if n := counts.get("/x/web-interface/share/add"); n != 1 {
		t.Fatalf("应分享 1 次, 实际 %d 次", n)
	}
	if n := counts.get("/x/web-interface/coin/today/exp"); n != 1 {
		t.Fatalf("应查询今日已投 1 次, 实际 %d 次", n)
	}
}

// TestDailySilver2CoinEnabled 验证 daily 配置中 silver2coin 开关开启时，
// 每日任务会执行银瓜子兑换子功能（exchange 请求恰好 1 次）。
func TestDailySilver2CoinEnabled(t *testing.T) {
	counts := newReqCounts()
	cfg := dailyTestConfig(func(d *model.DailyConfig) {
		d.Silver2Coin = true
	})
	client := bilibili.New(cfg, slog.Default())
	bilibili.SetTransportForTest(client, mockDailyTransport(t, counts, 100))

	ck := &model.Cookie{DedeUserID: "100", SESSDATA: "s", BiliJCT: "jct", Buvid3: "b3"}
	task := NewDailyTask(cfg, client, []*model.Cookie{ck}, slog.Default(), "")

	result, err := task.Run(context.Background())
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(result.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(result.Accounts))
	}
	steps := result.Accounts[0].Steps
	found := false
	for _, s := range steps {
		if s.Name == "银瓜子兑换硬币" {
			found = true
			if s.Status != "ok" {
				t.Fatalf("银瓜子兑换 status = %s, want ok (msg=%s)", s.Status, s.Message)
			}
		}
	}
	if !found {
		t.Fatalf("未找到银瓜子兑换步骤, steps=%+v", steps)
	}
	if n := counts.get("/xlive/revenue/v1/wallet/silver2coin"); n != 1 {
		t.Fatalf("exchange 请求次数 = %d, want 1", n)
	}
}

package task

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/raywangqvq/bilitoolgo/internal/api/bilibili"
	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// silver2coinTestConfig 构造银瓜子兑换任务测试配置。
func silver2coinTestConfig(enabled bool) *model.Config {
	return &model.Config{Tasks: model.TasksConfig{Silver2Coin: model.Silver2CoinConfig{Enabled: enabled}}}
}

// silver2coinTransport 按路径分发 mock 响应并计数；left 控制 silver_2_coin_left。
func silver2coinTransport(t *testing.T, counts *reqCounts, left int) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		counts.inc(path)
		switch path {
		case "/x/web-interface/nav":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"isLogin":true,"uname":"测试用户","money":100,"level_info":{"current_level":5}}}`), nil
		case "/xlive/revenue/v1/wallet/getStatus":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"silver":1000,"coin":0,"gold":0,"silver_2_coin_left":` + strconv.Itoa(left) + `,"coin_2_silver_left":0}}`), nil
		case "/xlive/revenue/v1/wallet/silver2coin":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"coin":2,"gold":0,"silver":0,"tid":"1"}}`), nil
		case "/site/getCoin":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"money":102}}`), nil
		default:
			t.Errorf("unexpected request path: %s", path)
			return serverErrorResponse(), nil
		}
	})
}

// runSilver2Coin 执行一次完整的银瓜子兑换任务（单账号）。
func runSilver2Coin(t *testing.T, cfg *model.Config, client *bilibili.BiliClient) *model.TaskResult {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tk := NewSilver2CoinTask(cfg, client, []*model.Cookie{dailyTestCookie()}, logger)
	res, err := tk.Run(context.Background())
	if err != nil {
		t.Fatalf("Silver2CoinTask.Run 失败: %v", err)
	}
	if len(res.Accounts) != 1 {
		t.Fatalf("应产生 1 个账号结果, got %d", len(res.Accounts))
	}
	return res
}

// TestSilver2CoinDisabled 未启用：记录 skip 且不发起任何请求。
func TestSilver2CoinDisabled(t *testing.T) {
	client := newDailyClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("任务禁用时不应发起请求: %s", r.URL.Path)
		return serverErrorResponse(), nil
	}))

	ar := runSilver2Coin(t, silver2coinTestConfig(false), client).Accounts[0]

	step := findStep(&ar, "银瓜子兑换硬币")
	if step == nil || step.Status != "skip" || step.Message != "银瓜子兑换硬币未启用" {
		t.Fatalf("应 skip(未启用): %+v", ar.Steps)
	}
}

// TestSilver2CoinFullFlow 正常链路：登录 → 查钱包 → 兑换 → 刷新余额，全部 ok。
func TestSilver2CoinFullFlow(t *testing.T) {
	counts := newReqCounts()
	client := newDailyClient(t, silver2coinTransport(t, counts, 1))

	ar := runSilver2Coin(t, silver2coinTestConfig(true), client).Accounts[0]

	if !ar.Success {
		t.Fatalf("全流程应成功: %+v", ar.Steps)
	}
	for _, name := range []string{"登录验证", "查询银瓜子余额", "银瓜子兑换硬币", "查询硬币余额"} {
		if s := findStep(&ar, name); s == nil || s.Status != "ok" {
			t.Fatalf("步骤 %s 应 ok: %+v", name, ar.Steps)
		}
	}
	if n := counts.get("/xlive/revenue/v1/wallet/getStatus"); n != 1 {
		t.Fatalf("应查询钱包 1 次, 实际 %d", n)
	}
	if n := counts.get("/xlive/revenue/v1/wallet/silver2coin"); n != 1 {
		t.Fatalf("应兑换 1 次, 实际 %d", n)
	}
	if n := counts.get("/site/getCoin"); n != 1 {
		t.Fatalf("应查询硬币余额 1 次, 实际 %d", n)
	}
}

// TestSilver2CoinSkipWhenLeftZero 剩余兑换次数 <=0：兑换步骤 skip（银瓜子不足），
// 不发起兑换请求，也不刷新余额。
func TestSilver2CoinSkipWhenLeftZero(t *testing.T) {
	counts := newReqCounts()
	client := newDailyClient(t, silver2coinTransport(t, counts, 0))

	ar := runSilver2Coin(t, silver2coinTestConfig(true), client).Accounts[0]

	step := findStep(&ar, "银瓜子兑换硬币")
	if step == nil || step.Status != "skip" || !strings.Contains(step.Message, "银瓜子不足") {
		t.Fatalf("兑换应 skip(银瓜子不足): %+v", ar.Steps)
	}
	if n := counts.get("/xlive/revenue/v1/wallet/silver2coin"); n != 0 {
		t.Fatalf("剩余次数为 0 时不应发起兑换, 实际 %d", n)
	}
	if n := counts.get("/site/getCoin"); n != 0 {
		t.Fatalf("未兑换时不应查询硬币余额, 实际 %d", n)
	}
}

// TestSilver2CoinAbortOnCookieInvalid nav 返回 code=-101：登录验证失败并中止该账号，
// 不发起任何钱包/兑换请求。
func TestSilver2CoinAbortOnCookieInvalid(t *testing.T) {
	counts := newReqCounts()
	client := newDailyClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		counts.inc(r.URL.Path)
		if r.URL.Path != "/x/web-interface/nav" {
			t.Errorf("Cookie 失效时不应发起请求: %s", r.URL.Path)
			return serverErrorResponse(), nil
		}
		return jsonResponse(`{"code":-101,"message":"账号未登录","ttl":1,"data":null}`), nil
	}))

	ar := runSilver2Coin(t, silver2coinTestConfig(true), client).Accounts[0]

	if ar.Success {
		t.Fatal("Cookie 失效时账号应标记失败")
	}
	if len(ar.Steps) != 1 {
		t.Fatalf("账号中止后应只有登录验证步骤: %+v", ar.Steps)
	}
	login := ar.Steps[0]
	if login.Name != "登录验证" || login.Status != "fail" || !strings.Contains(login.Message, "-101") {
		t.Fatalf("登录验证应 fail 且提示 -101: %+v", login)
	}
	if n := counts.get("/xlive/revenue/v1/wallet/getStatus"); n != 0 {
		t.Fatalf("登录失败后不应查询钱包, 实际 %d", n)
	}
}

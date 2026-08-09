package bilibili

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// visitIDRegex visit_id 格式：first(1-9) + 10 位小写字母数字 + last(0)。
var visitIDRegex = regexp.MustCompile(`^[1-9][0-9a-z]{10}0$`)

// biliTestCookie 解析出的测试 Cookie（raw 非空，可断言 Cookie 头）。
func biliTestCookie() *model.Cookie {
	ck, err := model.ParseCookie("DedeUserID=100; SESSDATA=test-session; bili_jct=test-jct")
	if err != nil {
		panic(err)
	}
	return ck
}

// biliTestClient 构造注入 mock transport 的 BiliClient。
func biliTestClient(t *testing.T, rt http.RoundTripper) *BiliClient {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := New(&model.Config{Bilibili: model.BilibiliConfig{IntervalSeconds: 0, UserAgent: "test"}}, logger)
	SetTransportForTest(c, rt)
	return c
}

// TestGetSilverStatus 验证钱包状态端点：路径/方法/Origin/响应解析。
func TestGetSilverStatus(t *testing.T) {
	var gotOrigin string
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/xlive/revenue/v1/wallet/getStatus" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		gotOrigin = r.Header.Get("Origin")
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"silver":100,"coin":2,"gold":3,"silver_2_coin_left":1,"coin_2_silver_left":10}}`), nil
	}))

	left, err := c.GetSilverStatus(context.Background(), biliTestCookie())
	if err != nil {
		t.Fatalf("GetSilverStatus error: %v", err)
	}
	if left != 1 {
		t.Fatalf("left = %d, want 1", left)
	}
	if gotOrigin != "https://link.bilibili.com" {
		t.Fatalf("Origin = %q, want https://link.bilibili.com", gotOrigin)
	}
}

// TestGetSilverStatusLeftZero 今日剩余兑换次数为 0。
func TestGetSilverStatusLeftZero(t *testing.T) {
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"silver":0,"silver_2_coin_left":0}}`), nil
	}))

	left, err := c.GetSilverStatus(context.Background(), biliTestCookie())
	if err != nil {
		t.Fatalf("GetSilverStatus error: %v", err)
	}
	if left != 0 {
		t.Fatalf("left = %d, want 0", left)
	}
}

// TestGetSilverStatusBizError 业务错误码返回 error。
func TestGetSilverStatusBizError(t *testing.T) {
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":-400,"message":"请求错误","ttl":1,"data":null}`), nil
	}))

	_, err := c.GetSilverStatus(context.Background(), biliTestCookie())
	if err == nil {
		t.Fatal("want error for code=-400, got nil")
	}
	if !strings.Contains(err.Error(), "-400") {
		t.Fatalf("error should contain code, got: %v", err)
	}
}

// TestExchangeSilver2Coin 验证兑换端点：路径/方法/表单字段/visit_id 格式/Referer/Origin/Cookie。
func TestExchangeSilver2Coin(t *testing.T) {
	var gotForm url.Values
	var gotOrigin, gotReferer, gotCookie, gotContentType string
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/xlive/revenue/v1/wallet/silver2coin" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		gotOrigin = r.Header.Get("Origin")
		gotReferer = r.Header.Get("Referer")
		gotCookie = r.Header.Get("Cookie")
		gotContentType = r.Header.Get("Content-Type")
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"coin":2,"gold":3,"silver":50,"tid":"1"}}`), nil
	}))

	err := c.ExchangeSilver2Coin(context.Background(), biliTestCookie())
	if err != nil {
		t.Fatalf("ExchangeSilver2Coin error: %v", err)
	}
	if gotForm.Get("csrf") != "test-jct" || gotForm.Get("csrf_token") != "test-jct" {
		t.Fatalf("csrf 字段错误: %v", gotForm)
	}
	if gotForm.Get("platform") != "pc" {
		t.Fatalf("platform 应为 pc: %v", gotForm)
	}
	if gotOrigin != "https://link.bilibili.com" || gotReferer != "https://link.bilibili.com" {
		t.Fatalf("Origin/Referer 应为 https://link.bilibili.com: origin=%q referer=%q", gotOrigin, gotReferer)
	}
	if !strings.HasPrefix(gotContentType, "application/x-www-form-urlencoded") {
		t.Fatalf("Content-Type 错误: %q", gotContentType)
	}
	if !strings.Contains(gotCookie, "bili_jct=test-jct") {
		t.Fatalf("Cookie 头应携带 bili_jct: %q", gotCookie)
	}
	if visitID := gotForm.Get("visit_id"); !visitIDRegex.MatchString(visitID) {
		t.Fatalf("visit_id 格式错误: %q", visitID)
	}
}

// TestExchangeSilver2CoinBizError 兑换业务失败返回 error。
func TestExchangeSilver2CoinBizError(t *testing.T) {
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":-400,"message":"兑换失败","ttl":1,"data":null}`), nil
	}))

	err := c.ExchangeSilver2Coin(context.Background(), biliTestCookie())
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "-400") {
		t.Fatalf("error should contain code, got: %v", err)
	}
}

// TestSilverVisitID 验证 visit_id 生成规则（§0.7）与进程内缓存。
func TestSilverVisitID(t *testing.T) {
	v1 := silverVisitID()
	v2 := silverVisitID()
	if v1 != v2 {
		t.Fatalf("visit_id 应进程内缓存一致: %q vs %q", v1, v2)
	}
	if !visitIDRegex.MatchString(v1) {
		t.Fatalf("visit_id 格式错误: %q", v1)
	}
}

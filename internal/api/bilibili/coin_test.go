package bilibili

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

// roundTripFunc 将函数适配为 http.RoundTripper，用于 mock 任意 URL 的响应。
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

// TestGetArchiveCoins 验证 /x/web-interface/archive/coins 的 data 为对象时的解析。
// 回归测试：此前误用 float64 导致 "cannot unmarshal object into ... float64"。
func TestGetArchiveCoins(t *testing.T) {
	c := New(&model.Config{}, slog.Default())
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/x/web-interface/archive/coins" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"multiply":2,"count":5}}`), nil
	})}

	got, err := c.GetArchiveCoins(context.Background(), &model.Cookie{}, 123)
	if err != nil {
		t.Fatalf("GetArchiveCoins error: %v", err)
	}
	if got != 2 {
		t.Fatalf("GetArchiveCoins = %d, want 2 (multiply=2 已投2枚，count=5 为总投币数不应使用)", got)
	}
}

// TestGetArchiveCoinsZero 验证未投币时 count=0。
func TestGetArchiveCoinsZero(t *testing.T) {
	c := New(&model.Config{}, slog.Default())
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"multiply":0,"count":0}}`), nil
	})}

	got, err := c.GetArchiveCoins(context.Background(), &model.Cookie{}, 456)
	if err != nil {
		t.Fatalf("GetArchiveCoins error: %v", err)
	}
	if got != 0 {
		t.Fatalf("GetArchiveCoins = %d, want 0", got)
	}
}

// TestGetArchiveCoinsBizError 验证业务错误码返回 error。
func TestGetArchiveCoinsBizError(t *testing.T) {
	c := New(&model.Config{}, slog.Default())
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":-400,"message":"请求错误","ttl":1,"data":null}`), nil
	})}

	_, err := c.GetArchiveCoins(context.Background(), &model.Cookie{}, 789)
	if err == nil {
		t.Fatal("want error for code=-400, got nil")
	}
	if !strings.Contains(err.Error(), "-400") {
		t.Fatalf("error should contain code, got: %v", err)
	}
}

// TestGetCoinBalance 验证 site/getCoin 的 data.money 数字解析。
func TestGetCoinBalance(t *testing.T) {
	c := New(&model.Config{}, slog.Default())
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"money":817.3,"money2":0}}`), nil
	})}

	got, err := c.GetCoinBalance(context.Background(), &model.Cookie{})
	if err != nil {
		t.Fatalf("GetCoinBalance error: %v", err)
	}
	if got != 817.3 {
		t.Fatalf("GetCoinBalance = %v, want 817.3", got)
	}
}

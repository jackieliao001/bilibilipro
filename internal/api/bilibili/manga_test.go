package bilibili

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// statusResponse 构造指定 HTTP 状态码的响应。
func statusResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// TestMangaClockIn 验证签到端点：方法/路径/platform 参数/Origin/Referer。
func TestMangaClockIn(t *testing.T) {
	var gotOrigin, gotReferer string
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/twirp/activity.v1.Activity/ClockIn" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if got := r.URL.Query().Get("platform"); got != "android" {
			t.Errorf("platform = %q, want android", got)
		}
		gotOrigin = r.Header.Get("Origin")
		gotReferer = r.Header.Get("Referer")
		return jsonResponse(`{"code":0,"message":"0","ttl":1}`), nil
	}))

	if err := c.MangaClockIn(context.Background(), biliTestCookie()); err != nil {
		t.Fatalf("MangaClockIn error: %v", err)
	}
	if gotOrigin != "https://manga.bilibili.com" || gotReferer != "https://manga.bilibili.com" {
		t.Fatalf("Origin/Referer 错误: %q %q", gotOrigin, gotReferer)
	}
}

// TestMangaClockInHTTP400 重复签到：HTTP 400 应返回 ErrMangaAlreadySignedIn。
func TestMangaClockInHTTP400(t *testing.T) {
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return statusResponse(http.StatusBadRequest, `{"code":-3,"message":"今日已签到过","ttl":1}`), nil
	}))

	err := c.MangaClockIn(context.Background(), biliTestCookie())
	if !errors.Is(err, ErrMangaAlreadySignedIn) {
		t.Fatalf("HTTP 400 应返回 ErrMangaAlreadySignedIn, got: %v", err)
	}
}

// TestMangaClockInBizError HTTP 200 但业务 code != 0 返回 error。
func TestMangaClockInBizError(t *testing.T) {
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":-400,"message":"签到失败","ttl":1}`), nil
	}))

	err := c.MangaClockIn(context.Background(), biliTestCookie())
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "-400") {
		t.Fatalf("error should contain code, got: %v", err)
	}
}

// TestMangaAddHistory 验证阅读打卡端点：路径/query（platform/comic_id/ep_id）。
func TestMangaAddHistory(t *testing.T) {
	var gotQuery string
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/twirp/bookshelf.v1.Bookshelf/AddHistory" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		gotQuery = r.URL.RawQuery
		return jsonResponse(`{"code":0,"message":"0","ttl":1}`), nil
	}))

	if err := c.MangaAddHistory(context.Background(), biliTestCookie(), 27355, 381662); err != nil {
		t.Fatalf("MangaAddHistory error: %v", err)
	}
	if gotQuery != "platform=android&comic_id=27355&ep_id=381662" {
		t.Fatalf("query = %q, want platform=android&comic_id=27355&ep_id=381662", gotQuery)
	}
}

// TestMangaAddHistoryHTTP400 阅读打卡 400 应为普通错误（不得误判为已签到）。
func TestMangaAddHistoryHTTP400(t *testing.T) {
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return statusResponse(http.StatusBadRequest, `{"code":-3,"message":"bad request","ttl":1}`), nil
	}))

	err := c.MangaAddHistory(context.Background(), biliTestCookie(), 27355, 381662)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if errors.Is(err, ErrMangaAlreadySignedIn) {
		t.Fatalf("AddHistory 的 400 不应视为已签到: %v", err)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("error should contain status 400, got: %v", err)
	}
}

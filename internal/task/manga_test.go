package task

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/raywangqvq/bilitoolgo/internal/api/bilibili"
	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// mangaTestConfig 构造漫画任务测试配置。
func mangaTestConfig(enabled bool, comicID int64) *model.Config {
	return &model.Config{Tasks: model.TasksConfig{Manga: model.MangaConfig{Enabled: enabled, CustomComicID: comicID}}}
}

// mangaBadRequestResponse 构造 HTTP 400 响应（漫画重复签到场景）。
func mangaBadRequestResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Status:     "400 Bad Request",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"code":-3,"message":"今日已签到过","ttl":1}`)),
	}
}

// mangaTransport 按路径分发 mock 响应并计数；clockIn400 时签到返回 HTTP 400；
// addHistoryQuery 捕获阅读请求的 query 以便断言。
func mangaTransport(t *testing.T, counts *reqCounts, clockIn400 bool, addHistoryQuery *string) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		path := r.URL.Path
		counts.inc(path)
		switch path {
		case "/x/web-interface/nav":
			return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"isLogin":true,"uname":"测试用户","money":100,"level_info":{"current_level":5}}}`), nil
		case "/twirp/activity.v1.Activity/ClockIn":
			if clockIn400 {
				return mangaBadRequestResponse(), nil
			}
			return jsonResponse(`{"code":0,"message":"0","ttl":1}`), nil
		case "/twirp/bookshelf.v1.Bookshelf/AddHistory":
			if addHistoryQuery != nil {
				*addHistoryQuery = r.URL.RawQuery
			}
			return jsonResponse(`{"code":0,"message":"0","ttl":1}`), nil
		default:
			t.Errorf("unexpected request path: %s", path)
			return serverErrorResponse(), nil
		}
	})
}

// runManga 执行一次完整的漫画任务（单账号）。
func runManga(t *testing.T, cfg *model.Config, client *bilibili.BiliClient) *model.TaskResult {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tk := NewMangaTask(cfg, client, []*model.Cookie{dailyTestCookie()}, logger)
	res, err := tk.Run(context.Background())
	if err != nil {
		t.Fatalf("MangaTask.Run 失败: %v", err)
	}
	if len(res.Accounts) != 1 {
		t.Fatalf("应产生 1 个账号结果, got %d", len(res.Accounts))
	}
	return res
}

// TestMangaDisabled 未启用：记录 skip 且不发起任何请求。
func TestMangaDisabled(t *testing.T) {
	client := newDailyClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("任务禁用时不应发起请求: %s", r.URL.Path)
		return serverErrorResponse(), nil
	}))

	ar := runManga(t, mangaTestConfig(false, 27355), client).Accounts[0]

	step := findStep(&ar, "漫画任务")
	if step == nil || step.Status != "skip" || step.Message != "漫画任务未启用" {
		t.Fatalf("应 skip(未启用): %+v", ar.Steps)
	}
}

// TestMangaFullFlow 正常链路：登录 → 签到 → 阅读（comic_id=27355, ep_id=381662, platform=android）。
func TestMangaFullFlow(t *testing.T) {
	counts := newReqCounts()
	var addHistoryQuery string
	client := newDailyClient(t, mangaTransport(t, counts, false, &addHistoryQuery))

	ar := runManga(t, mangaTestConfig(true, 27355), client).Accounts[0]

	if !ar.Success {
		t.Fatalf("全流程应成功: %+v", ar.Steps)
	}
	for _, name := range []string{"登录验证", "漫画签到", "漫画阅读"} {
		if s := findStep(&ar, name); s == nil || s.Status != "ok" {
			t.Fatalf("步骤 %s 应 ok: %+v", name, ar.Steps)
		}
	}
	if n := counts.get("/twirp/activity.v1.Activity/ClockIn"); n != 1 {
		t.Fatalf("应签到 1 次, 实际 %d", n)
	}
	if n := counts.get("/twirp/bookshelf.v1.Bookshelf/AddHistory"); n != 1 {
		t.Fatalf("应阅读 1 次, 实际 %d", n)
	}
	if addHistoryQuery != "platform=android&comic_id=27355&ep_id=381662" {
		t.Fatalf("阅读请求 query = %q, want platform=android&comic_id=27355&ep_id=381662", addHistoryQuery)
	}
}

// TestMangaClockIn400AsSignedIn 签到 HTTP 400：视为已签到（skip），阅读仍执行，账号成功。
func TestMangaClockIn400AsSignedIn(t *testing.T) {
	counts := newReqCounts()
	client := newDailyClient(t, mangaTransport(t, counts, true, nil))

	ar := runManga(t, mangaTestConfig(true, 27355), client).Accounts[0]

	if !ar.Success {
		t.Fatalf("重复签到视为成功, 账号不应失败: %+v", ar.Steps)
	}
	sign := findStep(&ar, "漫画签到")
	if sign == nil || sign.Status != "skip" || !strings.Contains(sign.Message, "已签到") {
		t.Fatalf("签到应 skip(已签到): %+v", ar.Steps)
	}
	if n := counts.get("/twirp/bookshelf.v1.Bookshelf/AddHistory"); n != 1 {
		t.Fatalf("重复签到后阅读仍应执行, 实际 %d", n)
	}
}

// TestMangaSkipReadWhenNoComic custom_comic_id<=0：阅读步骤跳过，不发 AddHistory。
func TestMangaSkipReadWhenNoComic(t *testing.T) {
	counts := newReqCounts()
	client := newDailyClient(t, mangaTransport(t, counts, false, nil))

	ar := runManga(t, mangaTestConfig(true, 0), client).Accounts[0]

	if !ar.Success {
		t.Fatalf("账号不应失败: %+v", ar.Steps)
	}
	read := findStep(&ar, "漫画阅读")
	if read == nil || read.Status != "skip" || !strings.Contains(read.Message, "custom_comic_id") {
		t.Fatalf("阅读应 skip(未配置): %+v", ar.Steps)
	}
	if n := counts.get("/twirp/bookshelf.v1.Bookshelf/AddHistory"); n != 0 {
		t.Fatalf("未配置漫画时不应发 AddHistory, 实际 %d", n)
	}
}

// TestMangaAbortOnCookieInvalid nav 返回 code=-101：登录验证失败并中止该账号。
func TestMangaAbortOnCookieInvalid(t *testing.T) {
	counts := newReqCounts()
	client := newDailyClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		counts.inc(r.URL.Path)
		if r.URL.Path != "/x/web-interface/nav" {
			t.Errorf("Cookie 失效时不应发起请求: %s", r.URL.Path)
			return serverErrorResponse(), nil
		}
		return jsonResponse(`{"code":-101,"message":"账号未登录","ttl":1,"data":null}`), nil
	}))

	ar := runManga(t, mangaTestConfig(true, 27355), client).Accounts[0]

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
	if n := counts.get("/twirp/activity.v1.Activity/ClockIn"); n != 0 {
		t.Fatalf("登录失败后不应签到, 实际 %d", n)
	}
}

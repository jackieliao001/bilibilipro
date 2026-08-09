package bilibili

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"testing"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// TestHeartbeatSkipCidZero 回归测试：arc/search 来源视频 cid=0 时，
// 请求体不应包含 cid 字段（发 cid=0 会使播放上报无效）。
func TestHeartbeatSkipCidZero(t *testing.T) {
	var gotBody string
	c := New(&model.Config{}, slog.Default())
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":null}`), nil
	})}

	ck := &model.Cookie{DedeUserID: "1", BiliJCT: "jct"}
	err := c.Heartbeat(context.Background(), ck, &model.VideoInfo{Aid: 100, Bvid: "BV1", Cid: 0}, 10)
	if err != nil {
		t.Fatalf("Heartbeat error: %v", err)
	}
	vals, _ := url.ParseQuery(gotBody)
	if _, ok := vals["cid"]; ok {
		t.Fatalf("cid should be omitted when 0, body=%s", gotBody)
	}
	if vals.Get("aid") != "100" {
		t.Fatalf("aid missing, body=%s", gotBody)
	}
}

// TestHeartbeatWithCid 验证 cid 非 0 时正常发送。
func TestHeartbeatWithCid(t *testing.T) {
	var gotBody string
	c := New(&model.Config{}, slog.Default())
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":null}`), nil
	})}

	ck := &model.Cookie{DedeUserID: "1", BiliJCT: "jct"}
	err := c.Heartbeat(context.Background(), ck, &model.VideoInfo{Aid: 100, Bvid: "BV1", Cid: 999}, 10)
	if err != nil {
		t.Fatalf("Heartbeat error: %v", err)
	}
	vals, _ := url.ParseQuery(gotBody)
	if vals.Get("cid") != "999" {
		t.Fatalf("cid = %q, want 999, body=%s", vals.Get("cid"), gotBody)
	}
}

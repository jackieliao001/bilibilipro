package bilibili

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

// TestGetFollowingsByTagBareArray 回归测试：/x/relation/tag 的 data 是裸数组。
// 此前误用 relationTagData{List} 导致 "cannot unmarshal array into struct"。
func TestGetFollowingsByTagBareArray(t *testing.T) {
	c := New(&model.Config{}, slog.Default())
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":[{"mid":1001,"uname":"up1"},{"mid":1002,"uname":"up2"}]}`), nil
	})}

	got, err := c.GetFollowingsByTag(context.Background(), &model.Cookie{DedeUserID: "1"}, 10, 1)
	if err != nil {
		t.Fatalf("GetFollowingsByTag error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Mid != 1001 || got[0].Uname != "up1" {
		t.Fatalf("got[0] = %+v, want mid=1001 uname=up1", got[0])
	}
}

// TestGetRelationTags 验证分组列表（data 为数组）。
func TestGetRelationTags(t *testing.T) {
	c := New(&model.Config{}, slog.Default())
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":[{"tagid":101,"name":"天选时刻","count":5}]}`), nil
	})}

	got, err := c.GetRelationTags(context.Background(), &model.Cookie{})
	if err != nil {
		t.Fatalf("GetRelationTags error: %v", err)
	}
	if len(got) != 1 || got[0].TagID != 101 || got[0].Name != "天选时刻" {
		t.Fatalf("unexpected: %+v", got)
	}
}

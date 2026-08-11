package bilibili

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

// TestRealArchiveCoins 真实网络回归测试（临时，验证后删除）。
// 用本机 build/cookies.json 的真实 cookie 只读查询 archive/coins，
// 确认修复后能正确解析 B 站真实响应（data 为对象 {multiply,count}）。
// 运行：REAL_TEST=1 go test -run TestRealArchiveCoins -v ./internal/api/bilibili/
func TestRealArchiveCoins(t *testing.T) {
	if os.Getenv("REAL_TEST") != "1" {
		t.Skip("set REAL_TEST=1 to run real network test")
	}
	cookies, err := model.ParseCookiesFromFile("D:/WorkSpace/GitSpace/BiliBiliToolPro/bilibilipro/build/cookies.json")
	if err != nil || len(cookies) == 0 {
		t.Fatalf("load cookies failed: %v", err)
	}
	cfg := &model.Config{Bilibili: model.BilibiliConfig{IntervalSeconds: 0, UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"}}
	c := New(cfg, slog.Default())

	// 用户日志中报错的两个 aid
	for _, aid := range []int64{117036801858088, 117031852709914} {
		got, err := c.GetArchiveCoins(context.Background(), cookies[0], aid)
		if err != nil {
			t.Fatalf("aid=%d error: %v", aid, err)
		}
		t.Logf("aid=%d coins=%d (ok)", aid, got)
	}
}

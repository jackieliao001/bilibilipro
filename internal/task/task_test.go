package task

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/raywangqvq/bilitoolgo/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testCookie(uid string) *model.Cookie {
	return &model.Cookie{DedeUserID: uid, SESSDATA: "s-" + uid, BiliJCT: "j-" + uid}
}

// TestMultiAccountTaskIsolation 验证单账号 panic/error 不中断其他账号。
func TestMultiAccountTaskIsolation(t *testing.T) {
	m := &MultiAccountTask{
		Name:    "Test",
		Cookies: []*model.Cookie{testCookie("1"), testCookie("2"), testCookie("3"), testCookie("4")},
		Logger:  testLogger(),
		DoFunc: func(ctx context.Context, ck *model.Cookie, idx int) (*model.AccountResult, error) {
			switch idx {
			case 0:
				panic("boom")
			case 1:
				return nil, errors.New("do error")
			case 2:
				return &model.AccountResult{UserID: ck.DedeUserID, Success: true}, nil
			default:
				return nil, nil // nil result + nil error 也应按失败记录，但不 panic
			}
		},
	}

	res, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("任务级不应返回错误: %v", err)
	}
	if len(res.Accounts) != 4 {
		t.Fatalf("4 个账号应全部产生结果, got %d", len(res.Accounts))
	}

	// 账号 0：panic 被隔离为 fail。
	a0 := res.Accounts[0]
	if a0.Success {
		t.Fatal("账号 0 应标记失败")
	}
	if len(a0.Steps) != 1 || a0.Steps[0].Status != "fail" || !strings.Contains(a0.Steps[0].Message, "panic: boom") {
		t.Fatalf("账号 0 失败步骤错误: %+v", a0.Steps)
	}

	// 账号 1：error 被记录为 fail 步骤。
	a1 := res.Accounts[1]
	if a1.Success || len(a1.Steps) != 1 || a1.Steps[0].Status != "fail" || a1.Steps[0].Message != "do error" {
		t.Fatalf("账号 1 失败步骤错误: %+v", a1.Steps)
	}

	// 账号 2：正常成功。
	if !res.Accounts[2].Success || len(res.Accounts[2].Steps) != 0 {
		t.Fatalf("账号 2 应成功: %+v", res.Accounts[2])
	}

	// 账号 3：nil,nil 返回 → UserID 回填、Success=false。
	a3 := res.Accounts[3]
	if a3.Success || a3.UserID != "4" {
		t.Fatalf("账号 3 处理错误: %+v", a3)
	}
}

// TestMultiAccountTaskCancellation 验证 ctx 取消后剩余账号不再执行。
func TestMultiAccountTaskCancellation(t *testing.T) {
	executed := 0
	ctx, cancel := context.WithCancel(context.Background())
	m := &MultiAccountTask{
		Name:    "Test",
		Cookies: []*model.Cookie{testCookie("1"), testCookie("2"), testCookie("3")},
		Logger:  testLogger(),
		DoFunc: func(ctx context.Context, ck *model.Cookie, idx int) (*model.AccountResult, error) {
			executed++
			cancel() // 第一个账号执行期间取消
			return &model.AccountResult{UserID: ck.DedeUserID, Success: true}, nil
		},
	}
	res, err := m.Run(ctx)
	if err == nil {
		t.Fatal("ctx 取消后 Run 应返回 ctx.Err()")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(res.Accounts) != 1 {
		t.Fatalf("取消后应只剩 1 个账号结果, got %d", len(res.Accounts))
	}
	if executed != 1 {
		t.Fatalf("DoFunc 应只执行 1 次, got %d", executed)
	}
}

// TestMultiAccountTaskNilCookie 验证 nil cookie 被跳过且不 panic。
func TestMultiAccountTaskNilCookie(t *testing.T) {
	m := &MultiAccountTask{
		Name:    "Test",
		Cookies: []*model.Cookie{nil, testCookie("1")},
		Logger:  testLogger(),
		DoFunc: func(ctx context.Context, ck *model.Cookie, idx int) (*model.AccountResult, error) {
			return &model.AccountResult{UserID: ck.DedeUserID, Success: true}, nil
		},
	}
	res, err := m.Run(context.Background())
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if len(res.Accounts) != 1 || res.Accounts[0].UserID != "1" {
		t.Fatalf("nil cookie 未正确跳过: %+v", res.Accounts)
	}
}

// TestRunStepStatusMapping 验证 runStep 的 ok/skip/fail 状态映射。
func TestRunStepStatusMapping(t *testing.T) {
	m := &MultiAccountTask{Name: "Test", Logger: testLogger()}
	ar := &model.AccountResult{}

	// ok
	s1 := m.runStep(ar, "步骤A", func() error { return nil })
	if s1.Status != "ok" || s1.Message != "" {
		t.Fatalf("成功步骤状态错误: %+v", s1)
	}

	// skip：errSkip 哨兵
	s2 := m.runStep(ar, "步骤B", func() error { return fmt.Errorf("%w: 今日已投满", errSkip) })
	if s2.Status != "skip" {
		t.Fatalf("errSkip 应映射为 skip: %+v", s2)
	}
	if s2.Message != "今日已投满" {
		t.Fatalf("skip 消息应剥离哨兵前缀: %q", s2.Message)
	}

	// fail：普通错误
	s3 := m.runStep(ar, "步骤C", func() error { return errors.New("网络错误") })
	if s3.Status != "fail" || s3.Message != "网络错误" {
		t.Fatalf("普通错误应映射为 fail: %+v", s3)
	}

	if len(ar.Steps) != 3 {
		t.Fatalf("应记录 3 个步骤, got %d", len(ar.Steps))
	}
	if ar.Steps[0].Status != "ok" || ar.Steps[1].Status != "skip" || ar.Steps[2].Status != "fail" {
		t.Fatalf("步骤顺序/状态错误: %+v", ar.Steps)
	}
}

// TestSkipStep 验证 skipStep 直接记录 skip 步骤。
func TestSkipStep(t *testing.T) {
	m := &MultiAccountTask{Name: "Test", Logger: testLogger()}
	ar := &model.AccountResult{}
	m.skipStep(ar, "观看视频", "未开启观看视频")
	if len(ar.Steps) != 1 {
		t.Fatalf("应记录 1 个步骤, got %d", len(ar.Steps))
	}
	s := ar.Steps[0]
	if s.Name != "观看视频" || s.Status != "skip" || s.Message != "未开启观看视频" {
		t.Fatalf("skipStep 记录错误: %+v", s)
	}
}

// TestCookieFilePath 验证环境变量覆盖与默认值。
func TestCookieFilePath(t *testing.T) {
	t.Setenv("RAY_COOKIE_FILE", "custom/cookies.json")
	if got := cookieFilePath(); got != "custom/cookies.json" {
		t.Fatalf("cookieFilePath() = %q, want custom/cookies.json", got)
	}
	t.Setenv("RAY_COOKIE_FILE", "")
	if got := cookieFilePath(); got != "cookies.json" {
		t.Fatalf("cookieFilePath() = %q, want cookies.json", got)
	}
}

// TestParseUIDSet 验证白名单解析。
func TestParseUIDSet(t *testing.T) {
	set := parseUIDSet("1, 2,abc,, -5, 1024")
	if len(set) != 3 || !set[1] || !set[2] || !set[1024] {
		t.Fatalf("parseUIDSet 结果错误: %+v", set)
	}
	if set[0] || set[5] {
		t.Fatalf("非法项不应进入集合: %+v", set)
	}
	if len(parseUIDSet("")) != 0 {
		t.Fatal("空串应返回空集合")
	}
}

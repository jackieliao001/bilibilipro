package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

// cookieFilePath 返回 Cookie 文件保存路径：优先环境变量 RAY_COOKIE_FILE，
// 否则默认 cookies.json。LoginTask 的 cookieFile 字段（来自 main 的 -cookies flag）优先级最高。
func cookieFilePath() string {
	if v := os.Getenv("RAY_COOKIE_FILE"); v != "" {
		return v
	}
	return "cookies.json"
}

// errSkip 步骤内部"跳过"的哨兵错误，runStep 将其映射为 skip 状态。
var errSkip = errors.New("skip")

// Task 任务接口。
type Task interface {
	Name() string
	Run(ctx context.Context) (*model.TaskResult, error)
}

// 编译期接口断言。
var (
	_ Task = (*LoginTask)(nil)
	_ Task = (*DailyTask)(nil)
	_ Task = (*UnfollowTask)(nil)
	_ Task = (*TestTask)(nil)
)

// MultiAccountTask 多账号任务基类：遍历账号执行 DoFunc，单账号 panic/error 异常隔离，失败不中断其他账号。
type MultiAccountTask struct {
	Name    string
	Cookies []*model.Cookie
	DoFunc  func(ctx context.Context, ck *model.Cookie, idx int) (*model.AccountResult, error)
	Logger  *slog.Logger
}

// Run 按账号依次执行 DoFunc，单账号异常隔离，ctx 取消时中断剩余账号。
func (t *MultiAccountTask) Run(ctx context.Context) (*model.TaskResult, error) {
	result := &model.TaskResult{TaskName: t.Name}
	for i, ck := range t.Cookies {
		if ck == nil {
			t.Logger.Warn("跳过空 Cookie 账号", "task", t.Name, "index", i)
			continue
		}
		select {
		case <-ctx.Done():
			t.Logger.Warn("任务被取消，剩余账号不再执行", "task", t.Name, "done", i)
			return result, ctx.Err()
		default:
		}
		ar, err := t.safeDo(ctx, ck, i)
		if ar == nil {
			ar = &model.AccountResult{UserID: ck.DedeUserID}
		}
		if ar.UserID == "" {
			ar.UserID = ck.DedeUserID
		}
		if err != nil {
			ar.Success = false
			ar.Steps = append(ar.Steps, model.StepResult{Name: "run", Status: "fail", Message: err.Error()})
		}
		result.Accounts = append(result.Accounts, *ar)
	}
	return result, nil
}

// safeDo 隔离单个账号执行中的 panic。
func (t *MultiAccountTask) safeDo(ctx context.Context, ck *model.Cookie, idx int) (ar *model.AccountResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			t.Logger.Warn("账号执行 panic，已隔离", "task", t.Name, "index", idx, "uid", ck.DedeUserID, "panic", r)
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return t.DoFunc(ctx, ck, idx)
}

// runStep 执行一步并记录 StepResult：打印"---开始{name}---"，失败仅记录日志，不中断后续步骤。
func (t *MultiAccountTask) runStep(ar *model.AccountResult, name string, fn func() error) model.StepResult {
	t.Logger.Info("---开始" + name + "---")
	step := model.StepResult{Name: name, Status: "ok"}
	if err := fn(); err != nil {
		if errors.Is(err, errSkip) {
			step.Status = "skip"
			step.Message = strings.TrimPrefix(err.Error(), "skip: ")
			t.Logger.Info("---跳过"+name+"---", "reason", step.Message)
		} else {
			step.Status = "fail"
			step.Message = err.Error()
			t.Logger.Warn("---"+name+"失败---", "err", err)
		}
	} else {
		t.Logger.Info("---完成" + name + "---")
	}
	ar.Steps = append(ar.Steps, step)
	return step
}

// runStepNoThrow 与 runStep 语义一致（失败不中断后续步骤），供调用处区分可读性。
func (t *MultiAccountTask) runStepNoThrow(ar *model.AccountResult, name string, fn func() error) model.StepResult {
	return t.runStep(ar, name, fn)
}

// skipStep 直接记录一个跳过步骤。
func (t *MultiAccountTask) skipStep(ar *model.AccountResult, name, reason string) {
	t.Logger.Info("---跳过"+name+"---", "reason", reason)
	ar.Steps = append(ar.Steps, model.StepResult{Name: name, Status: "skip", Message: reason})
}

// maskName 用户名脱敏：保留前 2 个字符，其余用 *** 代替（按 rune 处理中文）。
func maskName(name string) string {
	r := []rune(name)
	if len(r) <= 2 {
		return name + "***"
	}
	return string(r[:2]) + "***"
}

// shuffle 洗牌（math/rand/v2，自动随机种子）。
func shuffle[T any](s []T) {
	for i := len(s) - 1; i > 0; i-- {
		j := rand.IntN(i + 1)
		s[i], s[j] = s[j], s[i]
	}
}

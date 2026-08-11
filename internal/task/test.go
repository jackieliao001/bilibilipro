package task

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackieliao001/bilibilipro/internal/api/bilibili"
	"github.com/jackieliao001/bilibilipro/internal/model"
)

// TestTask 测试任务：校验每个账号的 Cookie 有效性并输出账号信息。
type TestTask struct {
	client *bilibili.BiliClient
	*MultiAccountTask
}

// NewTestTask 创建测试任务。
func NewTestTask(client *bilibili.BiliClient, cookies []*model.Cookie, logger *slog.Logger) Task {
	t := &TestTask{client: client}
	t.MultiAccountTask = &MultiAccountTask{
		Name:    "Test",
		Cookies: cookies,
		Logger:  logger,
		DoFunc:  t.doForAccount,
	}
	return t
}

// Name 任务名。
func (t *TestTask) Name() string { return t.MultiAccountTask.Name }

// doForAccount 校验单个账号：GetUserInfo → code==0 且 IsLogin 则输出账号信息。
func (t *TestTask) doForAccount(ctx context.Context, ck *model.Cookie, idx int) (*model.AccountResult, error) {
	ar := &model.AccountResult{UserID: ck.DedeUserID}
	info, err := t.client.GetUserInfo(ctx, ck)
	// 注意：bilibili.Get 在 code != 0 时同时返回 (resp, err)，先检查 resp.Code 区分业务错误。
	if info != nil && info.Code != 0 {
		msg := fmt.Sprintf("Cookie 无效或未登录: code=%d %s", info.Code, info.Message)
		ar.Success = false
		ar.Steps = append(ar.Steps, model.StepResult{Name: "登录校验", Status: "fail", Message: msg})
		t.Logger.Warn(msg, "uid", ck.DedeUserID)
		return ar, nil
	}
	if err != nil || info == nil {
		msg := "获取用户信息失败"
		if err != nil {
			msg = fmt.Sprintf("获取用户信息失败: %v", err)
		}
		ar.Success = false
		ar.Steps = append(ar.Steps, model.StepResult{Name: "登录校验", Status: "fail", Message: msg})
		t.Logger.Warn(msg, "uid", ck.DedeUserID)
		return ar, nil
	}
	if info.Data.IsLogin {
		level := 0
		if info.Data.LevelInfo != nil {
			level = info.Data.LevelInfo.CurrentLevel
		}
		vip := "无"
		if info.Data.VipStatus == 1 {
			vip = "大会员"
		}
		ar.UName = info.Data.Uname
		ar.Success = true
		msg := fmt.Sprintf("用户名 %s，等级 Lv%d，硬币 %.2f，会员 %s", ar.UName, level, info.Data.Money, vip)
		ar.Steps = append(ar.Steps, model.StepResult{Name: "登录校验", Status: "ok", Message: msg})
		t.Logger.Info("测试通过", "uid", ck.DedeUserID, "msg", msg)
	} else {
		msg := "Cookie 未登录，请重新运行 login 扫码登录"
		ar.Success = false
		ar.Steps = append(ar.Steps, model.StepResult{Name: "登录校验", Status: "fail", Message: msg})
		t.Logger.Warn(msg, "uid", ck.DedeUserID)
	}
	return ar, nil
}

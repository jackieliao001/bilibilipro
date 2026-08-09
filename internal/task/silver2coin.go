package task

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/raywangqvq/bilitoolgo/internal/api/bilibili"
	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// Silver2CoinTask 银瓜子兑换硬币任务。
type Silver2CoinTask struct {
	cfg    *model.Config
	client *bilibili.BiliClient
	*MultiAccountTask
}

// NewSilver2CoinTask 创建银瓜子兑换硬币任务。
func NewSilver2CoinTask(cfg *model.Config, client *bilibili.BiliClient, cookies []*model.Cookie, logger *slog.Logger) Task {
	t := &Silver2CoinTask{cfg: cfg, client: client}
	t.MultiAccountTask = &MultiAccountTask{
		Name:    "Silver2Coin",
		Cookies: cookies,
		Logger:  logger,
		DoFunc:  t.doForAccount,
	}
	return t
}

// Name 任务名。
func (t *Silver2CoinTask) Name() string { return t.MultiAccountTask.Name }

// doForAccount 单个账号的兑换流程：启用检查 → 登录验证 → 查钱包 → 兑换 → 刷新余额。
// 登录失败直接中止该账号，其余步骤失败仅记录日志，不影响账号继续。
func (t *Silver2CoinTask) doForAccount(ctx context.Context, ck *model.Cookie, idx int) (*model.AccountResult, error) {
	ar := &model.AccountResult{UserID: ck.DedeUserID, Success: true}

	// 1. 启用检查。
	if !t.cfg.Tasks.Silver2Coin.Enabled {
		ar.Steps = append(ar.Steps, model.StepResult{Name: "银瓜子兑换硬币", Status: "skip", Message: "银瓜子兑换硬币未启用"})
		return ar, nil
	}

	// 2. 登录验证：失败直接中止该账号。
	info, err := t.client.GetUserInfo(ctx, ck)
	if err != nil || info == nil || info.Code != 0 || !info.Data.IsLogin {
		// 注意：bilibili.Get 在 code != 0 时同时返回 (resp, err)，先检查 resp.Code 以输出更友好的消息。
		msg := "登录校验失败"
		switch {
		case info != nil && info.Code != 0:
			if info.Code == -101 {
				msg = "登录校验失败: Cookie 已失效或未登录（code=-101），请运行 bilipro login 重新扫码登录"
			} else {
				msg = fmt.Sprintf("登录校验失败: code=%d %s", info.Code, info.Message)
			}
		case err != nil:
			msg = fmt.Sprintf("登录校验失败: %v", err)
		case info == nil || !info.Data.IsLogin:
			msg = "Cookie 未登录，请重新运行 login 扫码登录"
		}
		t.Logger.Warn(msg, "uid", ck.DedeUserID)
		ar.Success = false
		ar.Steps = append(ar.Steps, model.StepResult{Name: "登录验证", Status: "fail", Message: msg})
		return ar, nil
	}
	level := 0
	if info.Data.LevelInfo != nil {
		level = info.Data.LevelInfo.CurrentLevel
	}
	ar.UName = info.Data.Uname
	ar.Steps = append(ar.Steps, model.StepResult{
		Name:    "登录验证",
		Status:  "ok",
		Message: fmt.Sprintf("用户 %s，硬币余额 %.2f，等级 Lv%d", ar.UName, info.Data.Money, level),
	})
	t.Logger.Info("登录成功", "uid", ck.DedeUserID, "uname", ar.UName)

	// 3. 查询钱包状态；查询失败或剩余兑换次数不足时不发起兑换。
	var left int
	step := t.runStepNoThrow(ar, "查询银瓜子余额", func() error {
		var err error
		left, err = t.client.GetSilverStatus(ctx, ck)
		return err
	})
	if step.Status != "ok" {
		return ar, nil
	}
	if left <= 0 {
		t.skipStep(ar, "银瓜子兑换硬币", "银瓜子不足，今日可兑换次数已用完")
		return ar, nil
	}

	// 4. 兑换（失败仅记录日志，不再查余额）。
	step = t.runStepNoThrow(ar, "银瓜子兑换硬币", func() error {
		return t.client.ExchangeSilver2Coin(ctx, ck)
	})
	if step.Status != "ok" {
		return ar, nil
	}

	// 5. 兑换成功后刷新硬币余额输出。
	t.runStepNoThrow(ar, "查询硬币余额", func() error {
		money, err := t.client.GetCoinBalance(ctx, ck)
		if err != nil {
			return err
		}
		t.Logger.Info("兑换完成，硬币余额", "uid", ck.DedeUserID, "balance", money)
		return nil
	})
	return ar, nil
}

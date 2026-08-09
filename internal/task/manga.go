package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/raywangqvq/bilitoolgo/internal/api/bilibili"
	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// defaultMangaEpID 漫画阅读默认章节 ep_id（原版 MangaTaskConfig.CustomEpId 默认值；
// 当前配置未提供 custom_ep_id 字段，阅读统一使用该默认章节）。
const defaultMangaEpID = 381662

// MangaTask 漫画签到+阅读任务。
type MangaTask struct {
	cfg    *model.Config
	client *bilibili.BiliClient
	*MultiAccountTask
}

// NewMangaTask 创建漫画任务。
func NewMangaTask(cfg *model.Config, client *bilibili.BiliClient, cookies []*model.Cookie, logger *slog.Logger) Task {
	t := &MangaTask{cfg: cfg, client: client}
	t.MultiAccountTask = &MultiAccountTask{
		Name:    "Manga",
		Cookies: cookies,
		Logger:  logger,
		DoFunc:  t.doForAccount,
	}
	return t
}

// Name 任务名。
func (t *MangaTask) Name() string { return t.MultiAccountTask.Name }

// doForAccount 单个账号的漫画流程：启用检查 → 登录验证 → 签到 → 阅读。
// 登录失败直接中止该账号，签到/阅读失败仅记录日志，不影响账号继续。
func (t *MangaTask) doForAccount(ctx context.Context, ck *model.Cookie, idx int) (*model.AccountResult, error) {
	ar := &model.AccountResult{UserID: ck.DedeUserID, Success: true}
	manga := t.cfg.Tasks.Manga

	// 1. 启用检查。
	if !manga.Enabled {
		ar.Steps = append(ar.Steps, model.StepResult{Name: "漫画任务", Status: "skip", Message: "漫画任务未启用"})
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

	// 3. 签到：HTTP 400（重复签到）视为已签到，按跳过处理。
	t.runStepNoThrow(ar, "漫画签到", func() error {
		err := t.client.MangaClockIn(ctx, ck)
		if errors.Is(err, bilibili.ErrMangaAlreadySignedIn) {
			return fmt.Errorf("%w: 今日已签到过", errSkip)
		}
		return err
	})

	// 4. 阅读：custom_comic_id <= 0 时跳过。
	if manga.CustomComicID <= 0 {
		t.skipStep(ar, "漫画阅读", "未配置自定义漫画（custom_comic_id<=0）")
		return ar, nil
	}
	t.runStepNoThrow(ar, "漫画阅读", func() error {
		return t.client.MangaAddHistory(ctx, ck, manga.CustomComicID, defaultMangaEpID)
	})
	return ar, nil
}

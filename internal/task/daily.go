package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/raywangqvq/bilitoolgo/internal/api/bilibili"
	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// DailyTask 每日任务：观看视频、分享视频、投币。
type DailyTask struct {
	cfg        *model.Config
	client     *bilibili.BiliClient
	cookieFile string
	*MultiAccountTask
}

// NewDailyTask 创建每日任务。
func NewDailyTask(cfg *model.Config, client *bilibili.BiliClient, cookies []*model.Cookie, logger *slog.Logger, cookieFile string) Task {
	t := &DailyTask{cfg: cfg, client: client, cookieFile: cookieFile}
	t.MultiAccountTask = &MultiAccountTask{
		Name:    "Daily",
		Cookies: cookies,
		Logger:  logger,
		DoFunc:  t.doForAccount,
	}
	return t
}

// Name 任务名。
func (t *DailyTask) Name() string { return t.MultiAccountTask.Name }

// doForAccount 单个账号的每日任务流程；登录失败直接中止该账号，其余步骤失败不中断。
func (t *DailyTask) doForAccount(ctx context.Context, ck *model.Cookie, idx int) (*model.AccountResult, error) {
	ar := &model.AccountResult{UserID: ck.DedeUserID, Success: true}
	daily := t.cfg.Tasks.Daily

	// 1. 启用检查。
	if !daily.Enabled {
		ar.Steps = append(ar.Steps, model.StepResult{Name: "每日任务", Status: "skip", Message: "每日任务未启用"})
		return ar, nil
	}

	// 2. 补全 Cookie：buvid3 为空时访问首页补全并保存（失败仅告警）。
	if ck.Buvid3 == "" {
		t.runStepNoThrow(ar, "补全Cookie", func() error {
			setCookies, err := t.client.GetHomePage(ctx, ck)
			if err != nil {
				return err
			}
			ck.MergeSetCookie(setCookies)
			if err := model.SaveCookiesToFile(t.Cookies, t.cookiePath()); err != nil {
				t.Logger.Warn("保存 Cookie 失败", "err", err)
			}
			return nil
		})
	}

	// 3. 登录验证：失败直接中止该账号。
	info, err := t.client.GetUserInfo(ctx, ck)
	if err != nil || info == nil || info.Code != 0 || !info.Data.IsLogin {
		// 注意：bilibili.Get 在 code != 0 时同时返回 (resp, err)，先检查 resp.Code 以输出更友好的消息。
		msg := "登录校验失败"
		switch {
		case info != nil && info.Code != 0:
			if info.Code == -101 {
				msg = fmt.Sprintf("登录校验失败: Cookie 已失效或未登录（code=-101），请运行 bilipro login 重新扫码登录")
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
	t.Logger.Info("登录成功", "uid", ck.DedeUserID, "uname", ar.UName, "level", level, "coins", info.Data.Money)

	// 4. 查询每日任务状态（失败重试一次；失败不中断后续步骤，按未完成处理）。
	var dailyInfo *model.DailyTaskInfo
	t.runStepNoThrow(ar, "查询每日任务状态", func() error {
		var err error
		dailyInfo, err = t.fetchDailyInfo(ctx, ck)
		return err
	})
	if dailyInfo == nil {
		t.Logger.Warn("每日任务状态未知，按未完成处理")
		dailyInfo = &model.DailyTaskInfo{}
	}

	// 5. 观看 + 分享。
	if daily.WatchVideoEnabled() {
		if dailyInfo.Watch {
			t.skipStep(ar, "观看视频", "今日已完成观看")
		} else {
			t.runStepNoThrow(ar, "观看视频", func() error {
				v, err := t.pickRandomVideo(ctx, ck, nil)
				if err != nil {
					return fmt.Errorf("挑选视频失败: %w", err)
				}
				t.Logger.Info("开始观看", "aid", v.Aid, "title", v.Title)
				if err := t.client.Heartbeat(ctx, ck, v, 0); err != nil {
					return fmt.Errorf("心跳(播放0秒)失败: %w", err)
				}
				if err := t.client.Heartbeat(ctx, ck, v, rand.IntN(15)+1); err != nil {
					return fmt.Errorf("心跳(播放完成)失败: %w", err)
				}
				t.Logger.Info("观看完成", "aid", v.Aid, "title", v.Title)
				return nil
			})
		}
	} else {
		t.skipStep(ar, "观看视频", "未开启观看视频")
	}

	if daily.ShareVideoEnabled() {
		if dailyInfo.Share {
			t.skipStep(ar, "分享视频", "今日已完成分享")
		} else {
			t.runStepNoThrow(ar, "分享视频", func() error {
				v, err := t.pickRandomVideo(ctx, ck, nil)
				if err != nil {
					return fmt.Errorf("挑选视频失败: %w", err)
				}
				if err := t.client.ShareVideo(ctx, ck, v.Aid); err != nil {
					return fmt.Errorf("分享失败: %w", err)
				}
				t.Logger.Info("分享完成", "aid", v.Aid, "title", v.Title)
				return nil
			})
		}
	} else {
		t.skipStep(ar, "分享视频", "未开启分享视频")
	}

	// 6. 投币。
	t.coinStep(ctx, ck, ar, level)

	// 7. 可选子功能（开关在 daily 配置段，默认关闭）。
	t.runOptionalSubTask(ctx, ck, ar, "银瓜子兑换", daily.Silver2Coin, func(subCfg *model.Config, client *bilibili.BiliClient, cookies []*model.Cookie, logger *slog.Logger) (task Task) {
		return NewSilver2CoinTask(subCfg, client, cookies, logger)
	}, func(cfg *model.Config) {
		cfg.Tasks.Silver2Coin.Enabled = true
	})
	t.runOptionalSubTask(ctx, ck, ar, "漫画签到", daily.Manga, func(subCfg *model.Config, client *bilibili.BiliClient, cookies []*model.Cookie, logger *slog.Logger) (task Task) {
		return NewMangaTask(subCfg, client, cookies, logger)
	}, func(cfg *model.Config) {
		cfg.Tasks.Manga.Enabled = true
	})
	t.runOptionalSubTask(ctx, ck, ar, "天选抽奖", daily.LiveLottery, func(subCfg *model.Config, client *bilibili.BiliClient, cookies []*model.Cookie, logger *slog.Logger) (task Task) {
		return NewLiveLotteryTask(subCfg, client, cookies, logger)
	}, func(cfg *model.Config) {
		cfg.Tasks.LiveLottery.Enabled = true
	})
	t.runOptionalSubTask(ctx, ck, ar, "直播挂机", daily.LiveFansMedal, func(subCfg *model.Config, client *bilibili.BiliClient, cookies []*model.Cookie, logger *slog.Logger) (task Task) {
		return NewLiveFansMedalTask(subCfg, client, cookies, logger)
	}, func(cfg *model.Config) {
		cfg.Tasks.LiveFansMedal.Enabled = true
	})

	return ar, nil
}

// runOptionalSubTask 执行每日任务中可选子功能：开关开启时构造子任务并执行，
// 将子任务的步骤合并进当前账号结果；关闭时记录 skip 步骤。
// enable 回调用于在 cfg 浅拷贝上强制打开子功能开关（每日任务开关独立于独立命令开关）。
func (t *DailyTask) runOptionalSubTask(ctx context.Context, ck *model.Cookie, ar *model.AccountResult, name string, enabled bool, newTask func(subCfg *model.Config, client *bilibili.BiliClient, cookies []*model.Cookie, logger *slog.Logger) Task, enable func(cfg *model.Config)) {
	if !enabled {
		t.skipStep(ar, name, "未在每日任务中开启")
		return
	}
	subCfg := *t.cfg
	enable(&subCfg)
	sub := newTask(&subCfg, t.client, []*model.Cookie{ck}, t.Logger) // 仅当前账号，避免子任务重复遍历全部账号
	subAr, err := sub.Run(ctx)
	if err != nil {
		t.Logger.Warn(name+"执行异常", "err", err)
		ar.Steps = append(ar.Steps, model.StepResult{Name: name, Status: "fail", Message: err.Error()})
		return
	}
	if subAr == nil || len(subAr.Accounts) == 0 {
		t.skipStep(ar, name, "无执行结果")
		return
	}
	// 合并子任务该账号的步骤（子任务内部已做登录验证与异常隔离）
	steps := subAr.Accounts[0].Steps
	if len(steps) == 0 {
		t.skipStep(ar, name, "无步骤记录")
		return
	}
	ar.Steps = append(ar.Steps, steps...)
}

// coinStep 投币步骤：总开关 → Lv6 保留 → 今日已投检查 → 余额检查 → 逐个投币。
func (t *DailyTask) coinStep(ctx context.Context, ck *model.Cookie, ar *model.AccountResult, level int) {
	daily := t.cfg.Tasks.Daily
	if !daily.DonateCoinEnabled() {
		t.skipStep(ar, "投币", "未开启投币")
		return
	}
	if daily.DonateCoins <= 0 {
		t.skipStep(ar, "投币", "未开启投币")
		return
	}
	// B 站每日最多投 5 枚硬币，配置超限按 5 处理。
	targetCoins := daily.DonateCoins
	if targetCoins > 5 {
		t.Logger.Info("donate_coins 超过每日上限 5，按 5 执行", "configured", daily.DonateCoins)
		targetCoins = 5
	}
	if daily.DonateForArticle {
		t.Logger.Info("专栏投币暂未启用，本次仅进行视频投币")
	}
	if level >= 6 && daily.SaveCoinsWhenLv6 {
		t.skipStep(ar, "投币", "已满级(Lv6)且开启保留硬币")
		return
	}
	t.runStepNoThrow(ar, "投币", func() error {
		already, err := t.client.GetDonatedCoinsToday(ctx, ck)
		if err != nil {
			return fmt.Errorf("获取今日已投硬币数失败: %w", err)
		}
		need := targetCoins - already
		if need <= 0 {
			return fmt.Errorf("%w: 今日硬币已投满（%d）", errSkip, already)
		}
		money, err := t.client.GetCoinBalance(ctx, ck)
		if err != nil {
			return fmt.Errorf("获取硬币余额失败: %w", err)
		}
		available := int(money) - daily.ProtectCoins
		if available <= 0 {
			return fmt.Errorf("%w: 可投硬币不足（余额 %.2f，保留 %d）", errSkip, money, daily.ProtectCoins)
		}
		if need > available {
			t.Logger.Info("硬币余额不足以投满，按可投数量投币", "need", need, "available", available)
			need = available
		}
		// 排除自己的 mid；单次投币失败换下一个视频重试，最多额外尝试 10 次；
		// 连续 3 次失败（如 -401 风控）提前终止，避免长时间空转。
		exclude := map[int64]bool{ck.UserID(): true}
		donated := 0
		attempts := 0
		consecutiveFails := 0
		for donated < need && attempts < need+10 {
			attempts++
			v, err := t.pickVideoForCoin(ctx, ck, exclude)
			if err != nil {
				return fmt.Errorf("挑选可投视频失败: %w", err)
			}
			// select_like 由配置 daily.select_like 控制（缺省 false，即投币不点赞）。
			if err := t.client.AddCoin(ctx, ck, v.Aid, 1, t.cfg.Tasks.Daily.SelectLike, false); err != nil {
				consecutiveFails++
				t.Logger.Warn("投币失败，换下一个视频重试", "aid", v.Aid, "连续失败次数", consecutiveFails, "err", err)
				exclude[v.Aid] = true
				if consecutiveFails >= 3 {
					t.Logger.Warn("连续多次投币失败（可能触发风控），提前结束投币", "已成功", donated, "目标", need)
					break
				}
				continue
			}
			consecutiveFails = 0
			exclude[v.Aid] = true
			donated++
			t.Logger.Info("投币成功", "aid", v.Aid, "title", v.Title)
		}
		if donated < need {
			return fmt.Errorf("投币未完成（成功 %d/%d）", donated, need)
		}
		t.Logger.Info("投币完成", "count", donated)
		return nil
	})
}

// fetchDailyInfo 获取每日任务状态，失败重试一次。
func (t *DailyTask) fetchDailyInfo(ctx context.Context, ck *model.Cookie) (*model.DailyTaskInfo, error) {
	resp, err := t.client.GetDailyTaskRewardInfo(ctx, ck)
	if err == nil && resp != nil && resp.Code == 0 {
		return &resp.Data, nil
	}
	if err == nil {
		if resp == nil {
			err = errors.New("空响应")
		} else {
			err = fmt.Errorf("code=%d %s", resp.Code, resp.Message)
		}
	}
	t.Logger.Warn("获取每日任务状态失败，重试一次", "err", err)
	resp, err = t.client.GetDailyTaskRewardInfo(ctx, ck)
	if err == nil && resp != nil && resp.Code == 0 {
		return &resp.Data, nil
	}
	if err == nil {
		if resp == nil {
			err = errors.New("空响应")
		} else {
			err = fmt.Errorf("code=%d %s", resp.Code, resp.Message)
		}
	}
	return nil, err
}

// pickRandomVideo 按优先级挑选随机视频：支持UP → 关注列表（逐个尝试）→ 排行榜。
func (t *DailyTask) pickRandomVideo(ctx context.Context, ck *model.Cookie, exclude map[int64]bool) (*model.VideoInfo, error) {
	if ids := t.cfg.Tasks.Daily.SupportUpIDs; ids != "" {
		for _, s := range strings.Split(ids, ",") {
			mid, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil || mid <= 0 {
				continue
			}
			videos, err := t.client.GetUpVideos(ctx, mid, ck)
			if err != nil || len(videos) == 0 {
				t.Logger.Debug("获取UP主视频失败", "mid", mid, "err", err)
				continue
			}
			if v := pickVideos(videos, exclude); v != nil {
				return v, nil
			}
		}
	}
	// 关注列表：GetFollowings 取前 20 个 mid 逐个尝试，成功即用。
	if ups, err := t.client.GetFollowings(ctx, ck); err == nil {
		for _, up := range ups {
			videos, err := t.client.GetUpVideos(ctx, up.Mid, ck)
			if err != nil || len(videos) == 0 {
				continue
			}
			if v := pickVideos(videos, exclude); v != nil {
				return v, nil
			}
		}
	} else {
		t.Logger.Warn("获取关注列表失败", "err", err)
	}
	// 排行榜兜底。
	videos, err := t.client.GetRankingVideos(ctx)
	if err != nil {
		return nil, err
	}
	if v := pickVideos(videos, exclude); v != nil {
		return v, nil
	}
	return nil, errors.New("未找到可用视频（排行榜为空或全部被排除）")
}

// pickVideoForCoin 挑选可投币的视频：排除自己的 mid 与已投满（>=2 币）的视频，最多尝试 10 次。
func (t *DailyTask) pickVideoForCoin(ctx context.Context, ck *model.Cookie, exclude map[int64]bool) (*model.VideoInfo, error) {
	exclude[ck.UserID()] = true
	for attempt := 0; attempt < 10; attempt++ {
		v, err := t.pickRandomVideo(ctx, ck, exclude)
		if err != nil {
			return nil, err
		}
		coins, err := t.client.GetArchiveCoins(ctx, ck, v.Aid)
		if err != nil {
			t.Logger.Warn("查询视频已投币数失败，视为可投", "aid", v.Aid, "err", err)
			return v, nil
		}
		if coins >= 2 {
			exclude[v.Aid] = true
			continue
		}
		return v, nil
	}
	return nil, errors.New("尝试 10 次仍未找到可投币的视频")
}

// pickVideos 从视频列表中随机选一个未被排除的视频。
// exclude 同时支持 aid 与 mid 两种键（投币场景排除自己 mid 与已投满的视频）。
func pickVideos(videos []model.VideoInfo, exclude map[int64]bool) *model.VideoInfo {
	candidates := make([]model.VideoInfo, 0, len(videos))
	for _, v := range videos {
		if exclude != nil && (exclude[v.Aid] || exclude[v.Mid]) {
			continue
		}
		candidates = append(candidates, v)
	}
	if len(candidates) == 0 {
		return nil
	}
	shuffle(candidates)
	return &candidates[0]
}

// cookiePath 返回 Cookie 文件保存路径：优先 main 传入的 -cookies 路径，
// 其次环境变量 RAY_COOKIE_FILE，最后默认 cookies.json。
func (t *DailyTask) cookiePath() string {
	if t.cookieFile != "" {
		return t.cookieFile
	}
	return cookieFilePath()
}

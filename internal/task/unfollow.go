package task

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackieliao001/bilibilipro/internal/api/bilibili"
	"github.com/jackieliao001/bilibilipro/internal/model"
)

// UnfollowTask 取关任务：按分组从最老关注开始批量取关。
type UnfollowTask struct {
	cfg    *model.Config
	client *bilibili.BiliClient
	*MultiAccountTask
}

// NewUnfollowTask 创建取关任务。
func NewUnfollowTask(cfg *model.Config, client *bilibili.BiliClient, cookies []*model.Cookie, logger *slog.Logger) Task {
	t := &UnfollowTask{cfg: cfg, client: client}
	t.MultiAccountTask = &MultiAccountTask{
		Name:    "Unfollow",
		Cookies: cookies,
		Logger:  logger,
		DoFunc:  t.doForAccount,
	}
	return t
}

// Name 任务名。
func (t *UnfollowTask) Name() string { return t.MultiAccountTask.Name }

// doForAccount 单个账号的取关流程。
func (t *UnfollowTask) doForAccount(ctx context.Context, ck *model.Cookie, idx int) (*model.AccountResult, error) {
	ar := &model.AccountResult{UserID: ck.DedeUserID, Success: true}
	uc := t.cfg.Tasks.Unfollow

	// 1. 启用检查。
	if !uc.Enabled {
		ar.Steps = append(ar.Steps, model.StepResult{Name: "取关", Status: "skip", Message: "取关任务未启用"})
		return ar, nil
	}

	// 2. 按 GroupName 匹配分组，找不到则报错返回。
	var tag model.TagInfo
	step := t.runStepNoThrow(ar, "查询分组", func() error {
		tags, err := t.client.GetRelationTags(ctx, ck)
		if err != nil {
			return err
		}
		for _, tg := range tags {
			if tg.Name == uc.GroupName {
				tag = tg
				return nil
			}
		}
		return fmt.Errorf("未找到分组 %q，请先在 B 站创建该分组并添加关注", uc.GroupName)
	})
	if step.Status == "fail" {
		ar.Success = false
		return ar, nil
	}

	// 3. 数量：Count<=0 表示全部，超过总数则取总数。
	count := uc.Count
	if count <= 0 || count > tag.Count {
		count = tag.Count
	}
	if count == 0 {
		t.skipStep(ar, "取关", "分组内暂无关注用户")
		return ar, nil
	}

	// 4. 分页拉取（从最后一页往前翻），最老关注的先取关，截断到 count；白名单过滤。
	retain := parseUIDSet(uc.RetainUIDs)
	var targets []model.UpInfo
	step = t.runStepNoThrow(ar, "获取关注列表", func() error {
		totalPage := (tag.Count + 19) / 20
		for pn := totalPage; pn >= 1; pn-- {
			page, err := t.client.GetFollowingsByTag(ctx, ck, tag.TagID, pn)
			if err != nil {
				return fmt.Errorf("第 %d 页获取失败: %w", pn, err)
			}
			// 页内倒序：列表为最新关注在前，倒序后最老关注在前。
			for i := len(page) - 1; i >= 0; i-- {
				if !retain[page[i].Mid] {
					targets = append(targets, page[i])
				}
			}
		}
		if len(targets) > count {
			targets = targets[:count]
		}
		return nil
	})
	if step.Status == "fail" {
		ar.Success = false
		return ar, nil
	}
	if len(targets) == 0 {
		t.skipStep(ar, "取关", "白名单过滤后没有需要取关的用户")
		return ar, nil
	}

	// 5. 逐个取关（act=2），统计成功/失败；请求间隔由 HTTP 层自动控制。
	success, failed := 0, 0
	step = t.runStepNoThrow(ar, "取关", func() error {
		for _, up := range targets {
			if err := t.client.ModifyRelation(ctx, ck, up.Mid, 2); err != nil {
				failed++
				t.Logger.Warn("取关失败", "mid", up.Mid, "uname", up.Uname, "err", err)
			} else {
				success++
				t.Logger.Info("取关成功", "mid", up.Mid, "uname", up.Uname)
			}
		}
		if success == 0 {
			return fmt.Errorf("取关全部失败（共 %d 个）", failed)
		}
		return nil
	})
	if step.Status == "fail" {
		ar.Success = false
	} else if len(ar.Steps) > 0 {
		last := &ar.Steps[len(ar.Steps)-1]
		if last.Status == "ok" {
			last.Message = fmt.Sprintf("成功 %d 个，失败 %d 个", success, failed)
		}
	}

	// 6. 重新获取分组，打印剩余人数。
	if tags, err := t.client.GetRelationTags(ctx, ck); err == nil {
		for _, tg := range tags {
			if tg.Name == uc.GroupName {
				t.Logger.Info("取关完成，分组剩余关注", "group", tg.Name, "count", tg.Count)
			}
		}
	} else {
		t.Logger.Warn("重新获取分组人数失败", "err", err)
	}

	return ar, nil
}

// parseUIDSet 解析逗号分隔的 UID 白名单为 set。
func parseUIDSet(s string) map[int64]bool {
	set := make(map[int64]bool)
	for _, part := range strings.Split(s, ",") {
		uid, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err == nil && uid > 0 {
			set[uid] = true
		}
	}
	return set
}

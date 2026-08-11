package task

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackieliao001/bilibilipro/internal/api/bilibili"
	"github.com/jackieliao001/bilibilipro/internal/model"
)

// 编译期接口断言。
var _ Task = (*LiveLotteryTask)(nil)

// LiveLotteryTask 天选时刻抽奖任务：扫描全部分区直播列表，
// 过滤出天选时刻直播间（Pendant_info["2"].Pendent_id==504）并批量参与抽奖。
type LiveLotteryTask struct {
	cfg    *model.Config
	client *bilibili.BiliClient
	*MultiAccountTask
}

// NewLiveLotteryTask 创建天选时刻抽奖任务。
func NewLiveLotteryTask(cfg *model.Config, client *bilibili.BiliClient, cookies []*model.Cookie, logger *slog.Logger) Task {
	t := &LiveLotteryTask{cfg: cfg, client: client}
	t.MultiAccountTask = &MultiAccountTask{
		Name:    "LiveLottery",
		Cookies: cookies,
		Logger:  logger,
		DoFunc:  t.doForAccount,
	}
	return t
}

// Name 任务名。
func (t *LiveLotteryTask) Name() string { return t.MultiAccountTask.Name }

// 天选抽奖状态 / 条件类型（与 DTO 枚举一致）。
const (
	tianXuanStatusEnable = 1 // 可参与
	tianXuanStatusEnd    = 2 // 已结束

	requireTypeNone   = 0 // 无要求
	requireTypeFollow = 1 // 关注主播
)

// doForAccount 单个账号的天选抽奖流程。
func (t *LiveLotteryTask) doForAccount(ctx context.Context, ck *model.Cookie, idx int) (*model.AccountResult, error) {
	ar := &model.AccountResult{UserID: ck.DedeUserID, Success: true}
	lc := t.cfg.Tasks.LiveLottery

	// 1. 启用检查。
	if !lc.Enabled {
		ar.Steps = append(ar.Steps, model.StepResult{Name: "天选时刻抽奖", Status: "skip", Message: "天选时刻抽奖未启用"})
		return ar, nil
	}

	// 2. 登录验证：失败直接中止该账号。
	if ok, msg := verifyLogin(ctx, t.client, ck, t.Logger); !ok {
		ar.Success = false
		ar.Steps = append(ar.Steps, model.StepResult{Name: "登录验证", Status: "fail", Message: msg})
		return ar, nil
	}
	ar.Steps = append(ar.Steps, model.StepResult{Name: "登录验证", Status: "ok", Message: "登录成功"})

	// 3. 记录抽奖前最后一次关注（分组时用于识别本次新增关注）；失败不影响抽奖主流程。
	lastFollowUpID := int64(0)
	if lc.FollowGroupName != "" {
		if ups, err := t.client.GetRecentFollowings(ctx, ck); err == nil && len(ups) > 0 {
			lastFollowUpID = ups[0].Mid
		} else if err != nil {
			t.Logger.Warn("获取最近关注失败，本次不做自动分组", "err", err)
		}
	}

	// 4. 扫描分区并参与抽奖（按 NumberOfDraw 控制次数）。
	draws := 0
	var followed []bilibili.TianXuanRoom
	t.runStepNoThrow(ar, "天选时刻抽奖", func() error {
		count, err := t.scanAndDraw(ctx, ck, lc, &draws, &followed)
		if err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("%w: 未搜索到天选直播间", errSkip)
		}
		return nil
	})

	// 5. 自动分组：把抽奖新增的关注移入 FollowGroupName 分组。
	if lc.FollowGroupName != "" && lastFollowUpID > 0 {
		t.runStepNoThrow(ar, "自动分组关注的主播", func() error {
			return t.groupFollowing(ctx, ck, lc.FollowGroupName, lastFollowUpID, followed)
		})
	}

	return ar, nil
}

// scanAndDraw 遍历全部分区（每区最多 5 页，has_more!=1 提前结束），
// 过滤天选时刻直播间并逐个尝试参与；达到 NumberOfDraw 后停止。
// 返回命中的直播间数量与成功参与数（draws）、抽奖关注的主播（followed）。
func (t *LiveLotteryTask) scanAndDraw(ctx context.Context, ck *model.Cookie, lc model.LiveLotteryConfig, draws *int, followed *[]bilibili.TianXuanRoom) (int, error) {
	areas, err := t.client.GetLiveAreas(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取直播分区失败: %w", err)
	}
	areas = filterAreas(areas, lc.AreaHostID)
	if len(areas) == 0 {
		return 0, fmt.Errorf("%w: 配置的分区过滤后为空", errSkip)
	}

	count := 0
	for _, area := range areas {
		select {
		case <-ctx.Done():
			return count, ctx.Err()
		default:
		}
		t.Logger.Info("扫描分区", "area", area.Name, "id", area.ID)

		sortType := ""
		for page := 1; page <= 5; page++ {
			data, err := t.client.GetTianXuanList(ctx, ck, area.ID, page, sortType)
			if err != nil {
				t.Logger.Warn("获取直播列表失败", "area", area.Name, "page", page, "err", err)
				break
			}
			for i := range data.List {
				room := &data.List[i]
				if !room.IsTianXuan() {
					continue
				}
				count++
				t.tryJoin(ctx, ck, lc, room, draws, followed)
				if lc.NumberOfDraw > 0 && *draws >= lc.NumberOfDraw {
					return count, nil
				}
			}
			if data.HasMore != 1 {
				break
			}
			if len(data.NewTags) > 0 {
				sortType = data.NewTags[0].SortType
			} else {
				sortType = ""
			}
		}
	}
	return count, nil
}

// tryJoin 尝试参与单个直播间的天选抽奖。全程异常隔离：任一环节失败仅记日志，
// 不中断扫描（对应原版 TryJoinTianXuan 整体 try/catch ignore）。
func (t *LiveLotteryTask) tryJoin(ctx context.Context, ck *model.Cookie, lc model.LiveLotteryConfig, room *bilibili.TianXuanRoom, draws *int, followed *[]bilibili.TianXuanRoom) {
	// 黑名单：retain_uids 中的主播 UID 不参与。
	if parseUIDSet(lc.RetainUids)[room.Uid] {
		t.Logger.Debug("主播在黑名单中，跳过", "uid", room.Uid, "uname", room.Uname)
		return
	}

	ok, check, err := t.client.CheckTianXuan(ctx, ck, room.RoomID)
	if err != nil {
		t.Logger.Warn("检查天选抽奖失败", "room", room.RoomID, "err", err)
		return
	}
	if !ok {
		t.Logger.Debug("无天选抽奖，跳过", "room", room.RoomID)
		return
	}
	if check.Status != tianXuanStatusEnable {
		t.Logger.Debug("抽奖已开奖，跳过", "room", room.RoomID, "status", check.Status)
		return
	}
	if !awardNameSatisfied(check.AwardName, lc.IncludeRewardName, lc.ExcludeRewardName) {
		t.Logger.Debug("奖品名不满足筛选条件，跳过", "room", room.RoomID, "award", check.AwardName)
		return
	}
	// 需赠送礼物（礼物价格超过配置下限，默认 0=任何赠礼都跳过）。
	if check.GiftPrice > lc.GiftPrice {
		t.Logger.Debug("需赠送礼物，跳过", "room", room.RoomID, "gift", check.GiftName, "price", check.GiftPrice)
		return
	}
	// 要求粉丝勋章（require_type 为 2 粉丝牌等级 / 3 提督舰长，或缺失）跳过。
	if check.RequireType == nil || (*check.RequireType != requireTypeNone && *check.RequireType != requireTypeFollow) {
		requireText := check.RequireText
		if check.RequireType != nil {
			requireText = fmt.Sprintf("%s(类型%d)", check.RequireText, *check.RequireType)
		}
		t.Logger.Debug("要求粉丝勋章，跳过", "room", room.RoomID, "require", requireText)
		return
	}

	t.Logger.Info("参与天选抽奖", "room", room.RoomID, "uname", room.Uname, "uid", room.Uid,
		"award", check.AwardName, "num", check.AwardNum, "require", check.RequireText)
	t.paceJoin()
	if err := t.client.JoinTianXuan(ctx, ck, check); err != nil {
		t.Logger.Warn("参与抽奖失败", "room", room.RoomID, "err", err)
		return
	}
	*draws++
	t.Logger.Info("参与抽奖成功", "room", room.RoomID, "uname", room.Uname, "已参与", *draws)

	// 关注型抽奖：记录主播用于后续自动分组（去重）。
	if check.RequireType != nil && *check.RequireType == requireTypeFollow {
		for _, f := range *followed {
			if f.Uid == room.Uid {
				return
			}
		}
		*followed = append(*followed, *room)
	}
}

// groupFollowing 把本次抽奖新增的关注主播批量移入指定分组：
// 关注列表（时间倒序）中位于 lastFollowUpID 之前的关注，与抽奖关注取交集。
func (t *LiveLotteryTask) groupFollowing(ctx context.Context, ck *model.Cookie, groupName string, lastFollowUpID int64, followed []bilibili.TianXuanRoom) error {
	if len(followed) == 0 {
		t.Logger.Info("本次抽奖未新增关注，跳过自动分组")
		return nil
	}

	ups, err := t.client.GetRecentFollowings(ctx, ck)
	if err != nil {
		return fmt.Errorf("获取关注列表失败: %w", err)
	}
	added := make(map[int64]bool)
	for _, up := range ups {
		if up.Mid == lastFollowUpID {
			break
		}
		added[up.Mid] = true
	}
	var targets []bilibili.TianXuanRoom
	for _, f := range followed {
		if added[f.Uid] {
			targets = append(targets, f)
		}
	}
	if len(targets) == 0 {
		t.Logger.Info("抽奖关注的主播不在新增关注列表中，跳过分组")
		return nil
	}

	tagID, err := t.findOrCreateGroup(ctx, ck, groupName)
	if err != nil {
		return err
	}
	fids := make([]int64, 0, len(targets))
	for _, f := range targets {
		fids = append(fids, f.Uid)
	}
	if err := t.client.CopyUpsToGroup(ctx, ck, fids, tagID); err != nil {
		return fmt.Errorf("批量分组失败: %w", err)
	}
	t.Logger.Info("分组完成", "group", groupName, "count", len(fids))
	return nil
}

// findOrCreateGroup 按名称查找关注分组，不存在则创建，返回 tagid。
func (t *LiveLotteryTask) findOrCreateGroup(ctx context.Context, ck *model.Cookie, groupName string) (int64, error) {
	tags, err := t.client.GetRelationTags(ctx, ck)
	if err != nil {
		return 0, fmt.Errorf("获取关注分组失败: %w", err)
	}
	for _, tg := range tags {
		if tg.Name == groupName {
			t.Logger.Info("关注分组已存在", "group", groupName, "tagid", tg.TagID)
			return tg.TagID, nil
		}
	}
	t.Logger.Info("关注分组不存在，尝试创建", "group", groupName)
	tagID, err := t.client.CreateRelationTag(ctx, ck, groupName)
	if err != nil {
		return 0, fmt.Errorf("创建关注分组失败: %w", err)
	}
	t.Logger.Info("关注分组创建成功", "group", groupName, "tagid", tagID)
	return tagID, nil
}

// filterAreas 按 AreaHostID 配置（逗号分隔的分区 id）过滤分区；空配置返回全部。
func filterAreas(areas []bilibili.LiveArea, areaHostID string) []bilibili.LiveArea {
	if strings.TrimSpace(areaHostID) == "" {
		return areas
	}
	want := parseUIDSet(areaHostID)
	out := make([]bilibili.LiveArea, 0, len(areas))
	for _, a := range areas {
		if want[a.ID] {
			out = append(out, a)
		}
	}
	return out
}

// awardNameSatisfied 奖品名过滤：命中任一排除关键字 → 不满足；
// 包含关键字非空时须命中至少一个（为空视为满足）。对应原版 AwardNameIsSatisfied。
func awardNameSatisfied(awardName, include, exclude string) bool {
	for _, key := range splitByPipe(exclude) {
		if strings.Contains(awardName, key) {
			return false
		}
	}
	if strings.TrimSpace(include) == "" {
		return true
	}
	for _, key := range splitByPipe(include) {
		if strings.Contains(awardName, key) {
			return true
		}
	}
	return false
}

// splitByPipe 按 | 分割关键字，去除空白与空项。
func splitByPipe(s string) []string {
	var out []string
	for _, part := range strings.Split(s, "|") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// paceJoin 参与抽奖前的固定 3s 限流（原版 IntervalDelegatingHandler 特殊路径；
// 仅当配置了请求间隔时生效，与 C# 行为一致）。
func (t *LiveLotteryTask) paceJoin() {
	if t.cfg.Bilibili.IntervalSeconds > 0 {
		time.Sleep(3 * time.Second)
	}
}

// verifyLogin 登录验证（nav 接口），返回是否成功与失败原因。
// code=-101 视为 Cookie 失效；失败由调用方中止该账号。
func verifyLogin(ctx context.Context, client *bilibili.BiliClient, ck *model.Cookie, logger *slog.Logger) (bool, string) {
	info, err := client.GetUserInfo(ctx, ck)
	if err != nil || info == nil || info.Code != 0 || !info.Data.IsLogin {
		msg := "登录校验失败"
		switch {
		case info != nil && info.Code != 0:
			if info.Code == -101 {
				msg = "登录校验失败: Cookie 已失效或未登录（code=-101），请运行 bilibilipro login 重新扫码登录"
			} else {
				msg = fmt.Sprintf("登录校验失败: code=%d %s", info.Code, info.Message)
			}
		case err != nil:
			msg = fmt.Sprintf("登录校验失败: %v", err)
		case info == nil || !info.Data.IsLogin:
			msg = "Cookie 未登录，请重新运行 login 扫码登录"
		}
		logger.Warn(msg, "uid", ck.DedeUserID)
		return false, msg
	}
	logger.Info("登录成功", "uid", ck.DedeUserID, "uname", info.Data.Uname)
	return true, ""
}

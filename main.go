// BiliBiliToolPro Go 重写版入口。
//
// 用法: bilibilipro <login|daily|unfollow|test> [-config path] [-cookies path] [-accounts 1,2] [-debug] [-cron expr] [-random-sleep]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackieliao001/bilibilipro/internal/api/bilibili"
	"github.com/jackieliao001/bilibilipro/internal/log"
	"github.com/jackieliao001/bilibilipro/internal/model"
	"github.com/jackieliao001/bilibilipro/internal/push"
	"github.com/jackieliao001/bilibilipro/internal/scheduler"
	"github.com/jackieliao001/bilibilipro/internal/task"
)

const (
	cmdLogin         = "login"
	cmdDaily         = "daily"
	cmdUnfollow      = "unfollow"
	cmdTest          = "test"
	cmdSilver2Coin   = "silver2coin"
	cmdManga         = "manga"
	cmdLiveLottery   = "livelottery"
	cmdLiveFansMedal = "livefansmedal"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 1 {
		usage()
		return 2
	}
	cmd := args[0]
	switch cmd {
	case cmdLogin, cmdDaily, cmdUnfollow, cmdTest, cmdSilver2Coin, cmdManga, cmdLiveLottery, cmdLiveFansMedal:
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n", cmd)
		usage()
		return 1
	}

	// 子命令后的 flag 解析
	fs := flag.NewFlagSet("bilibilipro "+cmd, flag.ContinueOnError)
	fs.Usage = usage

	defaultConfigPath := os.Getenv("CONFIG_PATH")
	if defaultConfigPath == "" {
		defaultConfigPath = "config.yaml"
	}
	configPath := fs.String("config", defaultConfigPath, "配置文件路径")

	defaultCookieFile := os.Getenv("COOKIE_PATH")
	if defaultCookieFile == "" {
		defaultCookieFile = os.Getenv("RAY_COOKIE_FILE")
	}
	if defaultCookieFile == "" {
		defaultCookieFile = "cookies.json"
	}
	cookieFile := fs.String("cookies", defaultCookieFile, "cookies JSON 文件路径（默认 cookies.json，可用环境变量 RAY_COOKIE_FILE 覆盖）")
	accountsFlag := fs.String("accounts", "", "账号索引列表（1-based，逗号分隔，默认全部）")
	debug := fs.Bool("debug", false, "开启 debug 日志级别")
	cronFlag := fs.String("cron", "", "cron 表达式（6 段，含秒），如 \"0 30 8 * * *\"；非空时进入常驻调度模式")
	randomSleepFlag := fs.Bool("random-sleep", false, "启用随机沉默（时长程序随机，最晚不超过当日 23:00:00 开始执行；未指定时取配置 security.random_sleep_enabled）")

	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, "参数解析失败:", err)
		usage()
		return 2
	}

	// 1. 加载配置
	cfg, err := model.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "加载配置失败:", err)
		fmt.Fprintln(os.Stderr, "提示：可复制 config.example.yaml 为 config.yaml 后修改。")
		return 1
	}
	if *debug {
		cfg.Log.Level = "debug"
	}

	// 2. 初始化日志（配置中 log.file 为空时，回退用 LOG_FILE 环境变量）
	if cfg.Log.File == "" {
		if v := os.Getenv("LOG_FILE"); v != "" {
			cfg.Log.File = v
		}
	}
	logger, err := log.Init(cfg.Log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化日志失败:", err)
		return 1
	}
	slog.SetDefault(logger)

	// 3. 组装 B 站客户端
	client := bilibili.New(cfg, logger)

	// 4. 加载 cookies（login 不需要）
	var cookies []*model.Cookie
	if cmd != cmdLogin {
		cookies, err = loadCookies(*cookieFile, cfg, logger)
		if err != nil {
			logger.Error("加载 cookies 失败", "error", err)
			return 1
		}
		logger.Info("加载 cookies 完成", "count", len(cookies))
	}

	// 5. 推送管理器（钉钉 / Server酱 / PushPlus / 企业微信 / Telegram / 自定义 API）
	pushMgr := push.NewManager()
	if dt := cfg.Push.DingTalk; dt.Enabled && dt.WebhookURL != "" {
		pushMgr.Add(push.NewDingTalkChannel(dt))
		logger.Info("已启用钉钉推送")
	}
	if sc := cfg.Push.ServerChan; sc.Enabled && sc.SendKey != "" {
		pushMgr.Add(push.NewServerChanChannel(sc))
		logger.Info("已启用Server酱推送")
	}
	if pp := cfg.Push.PushPlus; pp.Enabled && pp.Token != "" {
		pushMgr.Add(push.NewPushPlusChannel(pp))
		logger.Info("已启用PushPlus推送")
	}
	if ww := cfg.Push.WorkWeixin; ww.Enabled && ww.WebhookURL != "" {
		pushMgr.Add(push.NewWorkWeixinChannel(ww))
		logger.Info("已启用企业微信推送")
	}
	if tg := cfg.Push.Telegram; tg.Enabled && tg.BotToken != "" && tg.ChatID != "" {
		pushMgr.Add(push.NewTelegramChannel(tg))
		logger.Info("已启用Telegram推送")
	}
	if ca := cfg.Push.CustomAPI; ca.Enabled && ca.URL != "" {
		pushMgr.Add(push.NewCustomAPIChannel(ca))
		logger.Info("已启用自定义API推送")
	}

	// 6. -accounts 过滤（1-based）
	if cookies != nil {
		indices, perr := parseAccounts(*accountsFlag)
		if perr != nil {
			logger.Error("解析 -accounts 失败", "error", perr)
			return 2
		}
		if len(indices) > 0 {
			filtered := make([]*model.Cookie, 0, len(indices))
			for _, i := range indices {
				if i < 1 || i > len(cookies) {
					logger.Warn("账号索引越界，已跳过", "index", i, "total", len(cookies))
					continue
				}
				filtered = append(filtered, cookies[i-1])
			}
			cookies = filtered
		}
	}

	// 7. 调度与随机沉默参数：flag 优先，其次读取配置文件（config.scheduler / config.security）
	cronExpr := *cronFlag
	if cronExpr == "" && cfg.Scheduler.Enabled && cfg.Scheduler.Cron != "" {
		cronExpr = cfg.Scheduler.Cron
	}
	// -random-sleep 是 bool flag，无法表达"未指定"：用 fs.Visit 检测是否被显式设置，
	// 未显式设置时回退到 config.security.random_sleep_enabled。
	randomSleepSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "random-sleep" {
			randomSleepSet = true
		}
	})
	randomSleepEnabled := cfg.Security.RandomSleepEnabled
	if randomSleepSet {
		randomSleepEnabled = *randomSleepFlag
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 8. 常驻调度模式：cron 到点自动执行任务，Ctrl+C 优雅退出
	if cronExpr != "" {
		next, err := scheduler.NextRun(cronExpr)
		if err != nil {
			logger.Error("cron 表达式解析失败", "cron", cronExpr, "error", err)
			return 2
		}
		sleepDesc := "未启用"
		if randomSleepEnabled {
			sleepDesc = "已启用（时长程序随机，最晚不超过当日 23:00:00 开始执行）"
		}
		logger.Info(fmt.Sprintf("调度已启动: 表达式 %s, 下次执行 %s, 随机沉默 %s",
			cronExpr, next.Format("2006-01-02 15:04:05"), sleepDesc))

		job := func(ctx context.Context) error {
			_, _, runErr := runTaskOnce(ctx, cmd, cfg, client, cookies, *cookieFile, pushMgr, logger)
			if runErr != nil {
				// 单次失败只记日志，不影响调度器继续等待下次触发
				logger.Warn("单次任务执行未成功，等待下次调度", "error", runErr)
			}
			return nil
		}
		if err := scheduler.Run(ctx, cronExpr, randomSleepEnabled, job, logger); err != nil {
			logger.Error("调度器异常退出", "error", err)
			return 1
		}
		logger.Info("调度器已退出")
		return 0
	}

	// 9. 一次性模式：启用随机沉默时先沉默再执行（防定时集中触发）
	if randomSleepEnabled {
		if err := scheduler.SleepRandom(ctx, true, logger); err != nil {
			logger.Warn("随机沉默被取消，任务未执行", "error", err)
			return 0
		}
	} else {
		logger.Info("随机沉默未启用（security.random_sleep_enabled=false），如需启用请在 config.yaml 配置")
	}
	_, _, runErr := runTaskOnce(ctx, cmd, cfg, client, cookies, *cookieFile, pushMgr, logger)
	if runErr != nil {
		return 1
	}
	return 0
}

// runTaskOnce 创建任务并执行一次：按子命令创建 task、t.Run、控制台报告、
// 钉钉推送与成败统计。返回成功账号数 succ、总账号数 total 与错误 runErr，
// 退出码由调用方决定（一次性模式失败返回 1，调度模式仅记日志继续等下次）。
func runTaskOnce(ctx context.Context, cmd string, cfg *model.Config, client *bilibili.BiliClient, cookies []*model.Cookie, cookieFile string, pushMgr *push.Manager, logger *slog.Logger) (succ, total int, runErr error) {
	var t task.Task
	switch cmd {
	case cmdLogin:
		t = task.NewLoginTask(cfg, client, logger, cookieFile)
	case cmdDaily:
		t = task.NewDailyTask(cfg, client, cookies, logger, cookieFile)
	case cmdUnfollow:
		t = task.NewUnfollowTask(cfg, client, cookies, logger)
	case cmdTest:
		t = task.NewTestTask(client, cookies, logger)
	case cmdSilver2Coin:
		t = task.NewSilver2CoinTask(cfg, client, cookies, logger)
	case cmdManga:
		t = task.NewMangaTask(cfg, client, cookies, logger)
	case cmdLiveLottery:
		t = task.NewLiveLotteryTask(cfg, client, cookies, logger)
	case cmdLiveFansMedal:
		t = task.NewLiveFansMedalTask(cfg, client, cookies, logger)
	}

	logger.Info("开始执行任务", "task", t.Name())
	startAt := time.Now()
	result, err := t.Run(ctx)
	if result != nil {
		result.Duration = time.Since(startAt)
	}
	if err != nil {
		logger.Error("任务执行失败", "task", t.Name(), "error", err)
		return 0, 0, err
	}
	if result == nil {
		logger.Error("任务返回空结果", "task", t.Name())
		return 0, 0, errors.New("任务返回空结果")
	}

	// 控制台报告 + 钉钉推送摘要
	summary := result.Summary()
	fmt.Println(summary) // 控制台报告（与 QR 码展示同类，属用户可见输出而非业务日志）
	pushMgr.Send(ctx, "BiliTool 任务执行报告", summary)

	// 成败统计：任务整体失败（没有账号成功）返回错误，退出码交给调用方
	succ = 0
	for _, a := range result.Accounts {
		if a.Success {
			succ++
		}
	}
	total = len(result.Accounts)
	if succ == 0 {
		logger.Error("任务整体失败（没有账号执行成功）", "task", t.Name(), "total", total)
		return succ, total, fmt.Errorf("任务整体失败（没有账号执行成功）: %s", t.Name())
	}
	logger.Info("任务执行完成", "task", t.Name(), "success_accounts", succ, "total", total)
	return succ, total, nil
}

// loadCookies 从 cookies 文件加载；失败时回退到配置文件中内嵌的 cookies，
// 两者均无有效 cookie 时返回错误。
func loadCookies(path string, cfg *model.Config, logger *slog.Logger) ([]*model.Cookie, error) {
	cookies, err := model.ParseCookiesFromFile(path)
	if err == nil {
		return cookies, nil
	}
	logger.Warn("从文件加载 cookies 失败，回退到配置文件中的 cookies", "path", path, "error", err)

	var fallback []*model.Cookie
	for _, cc := range cfg.Cookies {
		ck, perr := model.ParseCookie(cc.CookieStr)
		if perr != nil {
			logger.Warn("配置文件中的 cookie 解析失败，已跳过", "name", cc.Name, "error", perr)
			continue
		}
		if verr := ck.Validate(); verr != nil {
			logger.Warn("配置文件中的 cookie 无效，已跳过", "name", cc.Name, "error", verr)
			continue
		}
		fallback = append(fallback, ck)
	}
	if len(fallback) == 0 {
		return nil, fmt.Errorf("没有可用的有效 cookie（文件 %s 加载失败，配置文件 cookies 也为空）", path)
	}
	return fallback, nil
}

// parseAccounts 解析 "1,2,3" 形式的账号索引（1-based）；空串返回 nil 表示全部。
func parseAccounts(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("无效的账号索引 %q", part)
		}
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: bilibilipro <login|daily|unfollow|test> [-config path] [-cookies path] [-accounts 1,2] [-debug] [-cron expr] [-random-sleep]")
	fmt.Fprintln(os.Stderr, "  login    扫码登录并保存新 cookie")
	fmt.Fprintln(os.Stderr, "  daily    每日任务（看视频/分享/投币等）")
	fmt.Fprintln(os.Stderr, "  unfollow 取关指定分组用户")
	fmt.Fprintln(os.Stderr, "  test     测试账号有效性")
	fmt.Fprintln(os.Stderr, "  silver2coin 银瓜子兑换硬币")
	fmt.Fprintln(os.Stderr, "  manga    漫画签到+阅读")
	fmt.Fprintln(os.Stderr, "  livelottery 天选时刻抽奖")
	fmt.Fprintln(os.Stderr, "  livefansmedal 直播间挂机")
	fmt.Fprintln(os.Stderr, "  -cron <expr>          常驻调度模式：cron 表达式（6 段，含秒），如 \"0 30 8 * * *\"=每天 08:30:00；留空则读取 config.scheduler")
	fmt.Fprintln(os.Stderr, "  -random-sleep          启用随机沉默：任务触发后随机沉默一段时间再执行（时长程序随机，最晚不超过当日 23:00:00 开始执行；默认取 config.security.random_sleep_enabled）")
	fmt.Fprintln(os.Stderr, "环境变量: RAY_COOKIE_FILE（cookies 文件路径）、RAY_LOG_LEVEL、RAY_DINGTALK_WEBHOOK")
}

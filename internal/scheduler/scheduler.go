// Package scheduler 提供 cron 常驻调度与随机沉默能力。
//
// 调度表达式同时支持两种格式（解析器为 SecondOptional）：
//   - 标准 5 段： "0 8 * * *"            —— 每天 08:00（与契约示例一致）
//   - 秒级 6 段： "*/5 * * * * *"        —— 每 5 秒（测试与需要秒级精度时使用）
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/robfig/cron/v3"
)

// parser 同时接受 5 段标准表达式（分钟级）与 6 段秒级表达式，
// 兼顾契约的 "0 8 * * *" 示例与测试所需的秒级快速触发。
var parser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// Run 常驻调度：按 cron 表达式周期执行 job。
// 每次触发时若 randomSleepEnabled 则先随机沉默（时长程序随机，最晚不超过当日 23:00:00 开始执行）再执行 job；
// 若上一次触发（含沉默）仍在进行，本次触发跳过（防重叠）。
// job panic 时 recover 并记录日志，不影响下次触发；
// ctx 取消时优雅停止调度器（等待正在运行的 job 完成）。返回 nil 表示调度器正常退出。
func Run(ctx context.Context, cronExpr string, randomSleepEnabled bool, job func(ctx context.Context) error, logger *slog.Logger) error {
	c := cron.New(
		cron.WithParser(parser),
		cron.WithChain(cron.SkipIfStillRunning(cronSlogLogger{logger: logger})),
	)
	if _, err := c.AddFunc(cronExpr, func() {
		logger.Info("调度触发")
		if err := SleepRandom(ctx, randomSleepEnabled, logger); err != nil {
			logger.Warn("随机沉默被取消，本次触发跳过", "error", err)
			return
		}
		defer func() {
			if r := recover(); r != nil {
				logger.Error("任务执行 panic，已恢复，等待下次触发", "panic", r)
			}
		}()
		if err := job(ctx); err != nil {
			if ctx.Err() != nil {
				logger.Warn("任务被取消", "error", err)
			} else {
				logger.Error("任务执行失败（不影响下次调度）", "error", err)
			}
		}
	}); err != nil {
		return fmt.Errorf("无效的 cron 表达式 %q: %w", cronExpr, err)
	}

	c.Start()
	logger.Info("调度已启动", "cron", cronExpr)

	<-ctx.Done()
	// Stop 停止后续触发，并返回一个在正在运行的 job 全部完成时取消的 context。
	<-c.Stop().Done()
	logger.Info("调度已停止")
	return nil
}

// SleepRandom 随机沉默（可被 ctx 取消）。
// enabled=false 直接返回 nil；enabled=true 时随机沉默 [0, 到当日 23:00:00 的剩余秒数] 秒，
// 即最晚不超过当日 23:00:00 开始执行（不会跨天）。ctx 取消时返回 ctx.Err()，调用方据此得知被中断。
func SleepRandom(ctx context.Context, enabled bool, logger *slog.Logger) error {
	return sleepRandomAt(ctx, enabled, time.Now(), logger)
}

// sleepRandomAt 是 SleepRandom 的实现，now 可注入以便测试固定时间点
// （避免测试依赖真实时钟：23:00 之后剩余窗口为 0，永远随机不到等待时长）。
func sleepRandomAt(ctx context.Context, enabled bool, now time.Time, logger *slog.Logger) error {
	secs := randomSleepSeconds(enabled, now)
	if secs <= 0 {
		// 未启用 / 随机到 0 秒 / 已过当日 23:00 截止时刻（理论边界）：直接返回不沉默
		return nil
	}
	execTime := time.Now().Add(time.Duration(secs) * time.Second).Format("2006-01-02 15:04:05")
	if secs >= 60 {
		logger.Info("随机沉默后执行任务",
			"沉默时长", fmt.Sprintf("%d分钟%d秒", secs/60, secs%60),
			"预计执行时间", execTime,
		)
	} else {
		logger.Info("随机沉默后执行任务",
			"沉默时长", fmt.Sprintf("%d秒", secs),
			"预计执行时间", execTime,
		)
	}
	select {
	case <-ctx.Done():
		logger.Warn("随机沉默被取消")
		return ctx.Err()
	case <-time.After(time.Duration(secs) * time.Second):
		return nil
	}
}

// sleepCutoffHour 随机沉默的最晚开始执行时间（24 小时制）。
// 沉默后最晚不超过当日 23:00:00 开始执行，为任务留足运行时间（避免接近午夜执行导致跨天）。
const sleepCutoffHour = 23

// remainingSecondsBeforeCutoff 返回 t 到当日 sleepCutoffHour:00:00 的剩余秒数。
// t 已过截止时刻时返回 0（不再沉默，立即执行）。
// t 的当日秒数 = t.Hour()*3600 + t.Minute()*60 + t.Second()（亚秒部分忽略，按秒截断）。
func remainingSecondsBeforeCutoff(t time.Time) int {
	secs := t.Hour()*3600 + t.Minute()*60 + t.Second()
	cutoff := sleepCutoffHour * 3600
	if secs >= cutoff {
		return 0
	}
	return cutoff - secs
}

// randomSleepSeconds 计算随机沉默秒数：enabled=false 返回 0；
// 否则在 [0, 到当日 23:00:00 的剩余秒数] 闭区间随机（已过截止时刻时返回 0）。
// 抽成独立函数便于单元测试。
func randomSleepSeconds(enabled bool, now time.Time) int {
	if !enabled {
		return 0
	}
	remaining := remainingSecondsBeforeCutoff(now)
	if remaining <= 0 {
		return 0
	}
	return rand.IntN(remaining + 1)
}

// NextRun 返回下一次 cron 触发时间（用于启动时打印提示）；解析失败返回 error。
func NextRun(cronExpr string) (time.Time, error) {
	sched, err := parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("无效的 cron 表达式 %q: %w", cronExpr, err)
	}
	return sched.Next(time.Now()), nil
}

// cronSlogLogger 把 robfig/cron 内部日志桥接到项目统一的 slog 日志，
// 使 SkipIfStillRunning 的跳过提示等以一致格式输出。
type cronSlogLogger struct {
	logger *slog.Logger
}

func (l cronSlogLogger) Info(msg string, keysAndValues ...any) {
	l.logger.Info(msg, kvToAttrs(keysAndValues)...)
}

func (l cronSlogLogger) Error(err error, msg string, keysAndValues ...any) {
	l.logger.Error(msg, append(kvToAttrs(keysAndValues), slog.Any("error", err))...)
}

// kvToAttrs 把 cron 的 "key, value, ..." 平铺切片转换为 slog 的 attr 参数。
func kvToAttrs(kv []any) []any {
	attrs := make([]any, 0, len(kv))
	for i := 0; i+1 < len(kv); i += 2 {
		attrs = append(attrs, slog.Any(fmt.Sprintf("%v", kv[i]), kv[i+1]))
	}
	return attrs
}

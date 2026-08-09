package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSleepRandomDisabled 验证 enabled=false 时立即返回，不做任何沉默。
func TestSleepRandomDisabled(t *testing.T) {
	start := time.Now()
	if err := SleepRandom(context.Background(), false, testLogger()); err != nil {
		t.Fatalf("enabled=false 不应返回错误: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
		t.Fatalf("enabled=false 应立即返回, 实际耗时 %v", elapsed)
	}
}

// TestRemainingSecondsBeforeCutoff 验证沉默截止（23:00:00）剩余秒数计算（固定时间断言）。
func TestRemainingSecondsBeforeCutoff(t *testing.T) {
	cases := []struct {
		name string
		t    time.Time
		want int
	}{
		{"00:00:00（当天刚开始，剩余到23:00为 23小时）", time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC), 23 * 3600},
		{"12:00:00（剩 11 小时）", time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC), 11 * 3600},
		{"22:59:59（最后一秒）", time.Date(2026, 8, 8, 22, 59, 59, 0, time.UTC), 1},
		{"23:00:00（已到截止，不再沉默）", time.Date(2026, 8, 8, 23, 0, 0, 0, time.UTC), 0},
		{"23:59:59（已过截止）", time.Date(2026, 8, 8, 23, 59, 59, 0, time.UTC), 0},
		{"12:00:00.999（亚秒按秒截断）", time.Date(2026, 8, 8, 12, 0, 0, 999_000_000, time.UTC), 11 * 3600},
	}
	for _, c := range cases {
		if got := remainingSecondsBeforeCutoff(c.t); got != c.want {
			t.Fatalf("%s: remainingSecondsBeforeCutoff 应为 %d, got %d", c.name, c.want, got)
		}
	}
}

// TestRandomSleepSeconds 验证随机秒数计算：enabled=false → 0；
// enabled=true 且 now 固定 → 返回值在 [0, 当日剩余秒数] 闭区间内。
func TestRandomSleepSeconds(t *testing.T) {
	noon := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	if v := randomSleepSeconds(false, noon); v != 0 {
		t.Fatalf("randomSleepSeconds(false, now) 应为 0, got %d", v)
	}

	// 中午：剩余 39600 秒（到 23:00），返回值应在 [0, 39600] 内。
	remainingNoon := remainingSecondsBeforeCutoff(noon)
	if remainingNoon != 39600 {
		t.Fatalf("前置校验失败: 中午剩余秒数应为 39600, got %d", remainingNoon)
	}
	for i := 0; i < 500; i++ {
		v := randomSleepSeconds(true, noon)
		if v < 0 || v > remainingNoon {
			t.Fatalf("randomSleepSeconds(true, noon) 应在 [0, %d] 内, got %d", remainingNoon, v)
		}
	}

	// 午夜 00:00:00：剩余 82800 秒（23 小时，上限最大场景）。
	midnight := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	remainingMid := remainingSecondsBeforeCutoff(midnight)
	if remainingMid != 82800 {
		t.Fatalf("前置校验失败: 午夜剩余秒数应为 82800, got %d", remainingMid)
	}
	for i := 0; i < 500; i++ {
		v := randomSleepSeconds(true, midnight)
		if v < 0 || v > remainingMid {
			t.Fatalf("randomSleepSeconds(true, midnight) 应在 [0, %d] 内, got %d", remainingMid, v)
		}
	}

	// 23:59:59：已过 23:00 截止，剩余 0 秒，只能返回 0（保证 23:00 前开始执行）。
	late := time.Date(2026, 8, 8, 23, 59, 59, 0, time.UTC)
	for i := 0; i < 200; i++ {
		v := randomSleepSeconds(true, late)
		if v != 0 {
			t.Fatalf("randomSleepSeconds(true, late) 应在 [0, 1] 内, got %d", v)
		}
	}
}

// TestSleepRandomCancel 验证 ctx 取消时 SleepRandom 提前返回 ctx.Err()。
// 注意：enabled=true 时随机秒数可能恰为 0（立即返回 nil，不进入等待），
// 因此用重试循环确保至少一次真正进入等待后再取消，避免偶发假失败。
func TestSleepRandomCancel(t *testing.T) {
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- SleepRandom(ctx, true, testLogger())
		}()

		time.Sleep(50 * time.Millisecond)
		cancel()

		select {
		case err := <-done:
			if errors.Is(err, context.Canceled) {
				return // 成功验证：等待被取消并提前返回
			}
			if err != nil {
				t.Fatalf("取消后应返回 context.Canceled, got %v", err)
			}
			// err == nil：本次随机到 0 秒，未进入实际等待，重试
		case <-time.After(2 * time.Second):
			t.Fatal("ctx 取消后 SleepRandom 未在 2 秒内返回")
		}
	}
	t.Fatal("连续 20 次随机均为 0 秒，未进入实际等待，无法验证取消路径")
}

// TestNextRunValid 验证合法表达式（5 段标准 + 6 段秒级）返回未来时间。
func TestNextRunValid(t *testing.T) {
	now := time.Now()
	for _, expr := range []string{"0 8 * * *", "0 0 8 * * *", "*/5 * * * * *", "@daily"} {
		next, err := NextRun(expr)
		if err != nil {
			t.Fatalf("NextRun(%q) 不应报错: %v", expr, err)
		}
		if !next.After(now) {
			t.Fatalf("NextRun(%q) 应返回未来时间, got %v (now=%v)", expr, next, now)
		}
	}
	// 秒级表达式应在 6 秒内再次触发。
	next, err := NextRun("*/5 * * * * *")
	if err != nil {
		t.Fatalf("NextRun 不应报错: %v", err)
	}
	if d := next.Sub(time.Now()); d > 6*time.Second {
		t.Fatalf("每 5 秒的表达式下次触发应不超过 6 秒, got %v", d)
	}
}

// TestNextRunInvalid 验证非法表达式返回 error 与零值时间。
func TestNextRunInvalid(t *testing.T) {
	for _, expr := range []string{"", "not a cron", "0 8", "61 * * * * *", "0 0 25 * * *"} {
		next, err := NextRun(expr)
		if err == nil {
			t.Fatalf("NextRun(%q) 应返回错误", expr)
		}
		if !next.IsZero() {
			t.Fatalf("NextRun(%q) 失败时应返回零值时间, got %v", expr, next)
		}
	}
}

// TestRunTriggersJob 验证每秒触发（6 段秒级表达式）时 job 至少执行一次。
func TestRunTriggersJob(t *testing.T) {
	var count atomic.Int32
	job := func(ctx context.Context) error {
		count.Add(1)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, "*/1 * * * * *", false, job, testLogger()) }()

	time.Sleep(2500 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run 应返回 nil, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ctx 取消后 Run 未退出")
	}
	if got := count.Load(); got < 1 {
		t.Fatalf("job 应至少执行一次, got %d", got)
	}
}

// TestRunSkipOverlap 验证防重叠：job 阻塞 3 秒时，期间每秒触发均被跳过，
// 最终只执行一次。
func TestRunSkipOverlap(t *testing.T) {
	var count atomic.Int32
	job := func(ctx context.Context) error {
		count.Add(1)
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, "*/1 * * * * *", false, job, testLogger()) }()

	time.Sleep(3500 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run 应返回 nil, got %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("ctx 取消后 Run 未退出（应等正在运行的 job 完成）")
	}
	if got := count.Load(); got != 1 {
		t.Fatalf("重叠触发应被跳过（只执行 1 次）, got %d", got)
	}
}

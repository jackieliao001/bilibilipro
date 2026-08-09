package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingRT 统计请求次数的 RoundTripper。
type countingRT struct{ n atomic.Int32 }

func (c *countingRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
}

func (c *countingRT) count() int { return int(c.n.Load()) }

// TestIntervalRoundTripperZeroFastPath IntervalSeconds<=0 直接透传（快速路径，不 sleep）。
func TestIntervalRoundTripperZeroFastPath(t *testing.T) {
	rt := &countingRT{}
	if got := NewIntervalRoundTripper(rt, 0); got != rt {
		t.Fatal("IntervalSeconds=0 应直接返回原 RoundTripper（快速路径）")
	}
	if got := NewIntervalRoundTripper(rt, -1); got != rt {
		t.Fatal("IntervalSeconds<0 应直接返回原 RoundTripper")
	}
}

// TestIntervalRoundTripperFastPathNoDelay 快速路径下请求应立即发出（无 sleep 开销）。
func TestIntervalRoundTripperFastPathNoDelay(t *testing.T) {
	rt := NewIntervalRoundTripper(&countingRT{}, 0)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("快速路径不应有间隔开销, 实际耗时 %v", d)
	}
}

// TestIntervalRoundTripperConcurrent 并发安全：多 goroutine 同时请求不 panic，
// 且请求被间隔串行化（每个请求的目标间隔 [interval/2, interval] 内随机）。
// 注：interval 参数以秒为单位（NewIntervalRoundTripper 按秒构造），故用 1s。
func TestIntervalRoundTripperConcurrent(t *testing.T) {
	const n = 8
	base := &countingRT{}
	rt := NewIntervalRoundTripper(base, 1) // interval=1s，目标间隔 [0.5s, 1s]

	start := time.Now()
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com/", nil)
			if err != nil {
				errCh <- err
				return
			}
			resp, err := rt.RoundTrip(req)
			if err != nil {
				errCh <- err
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发请求失败（含 panic/竞争）: %v", err)
	}
	if base.count() != n {
		t.Fatalf("请求数应为 %d, 实际 %d", n, base.count())
	}
	elapsed := time.Since(start)
	// 8 个请求串行化：最小总间隔 7×0.5s=3.5s。
	if elapsed < 3*time.Second {
		t.Fatalf("请求应被间隔串行化（8×1s 间隔）, 实际耗时 %v", elapsed)
	}
}

// TestIntervalRoundTripperCancelDuringWait 等待期间 ctx 取消：快速返回 ctx.Err()，不占用连接。
func TestIntervalRoundTripperCancelDuringWait(t *testing.T) {
	rt := NewIntervalRoundTripper(&countingRT{}, 5) // 目标间隔 [2.5s, 5s]
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 占位请求：让内部 last 非零，后续请求才需要等待。
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	time.Sleep(200 * time.Millisecond)
	done := make(chan error, 1)
	go func() {
		req2, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/", nil)
		if err != nil {
			done <- err
			return
		}
		_, err = rt.RoundTrip(req2)
		done <- err
	}()
	time.Sleep(200 * time.Millisecond) // 让第 2 个请求进入等待

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消后应返回 context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ctx 取消后 RoundTrip 未快速返回（等待不可中断？）")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("取消后返回过慢: %v", d)
	}
}

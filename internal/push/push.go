// Package push 负责多渠道消息推送（当前支持钉钉）。
package push

import (
	"context"
	"log/slog"
	"sync"
)

// Channel 是一个推送渠道。
type Channel interface {
	// Name 返回渠道名称。
	Name() string
	// Send 发送一条消息；失败返回 error。
	Send(ctx context.Context, title, message string) error
}

// Manager 管理多个推送渠道，支持并发广播。
type Manager struct {
	mu       sync.Mutex
	channels []Channel
}

// NewManager 创建一个空的推送管理器。
func NewManager() *Manager {
	return &Manager{channels: make([]Channel, 0)}
}

// Add 注册一个推送渠道（nil 安全）。
func (m *Manager) Add(ch Channel) {
	if m == nil || ch == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels = append(m.channels, ch)
}

// Send 向所有渠道并发广播消息，等待全部渠道完成后返回。
// 单渠道失败仅记录 warn 日志，不阻塞其他渠道；无渠道时静默返回。
func (m *Manager) Send(ctx context.Context, title, message string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	chs := make([]Channel, len(m.channels))
	copy(chs, m.channels)
	m.mu.Unlock()

	if len(chs) == 0 {
		return
	}

	var wg sync.WaitGroup
	for _, ch := range chs {
		wg.Add(1)
		go func(ch Channel) {
			defer wg.Done()
			if err := ch.Send(ctx, title, message); err != nil {
				slog.Warn("推送渠道发送失败", "channel", ch.Name(), "error", err)
			}
		}(ch)
	}
	wg.Wait()
}

// Package log 初始化并管理应用日志（slog）。
package log

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

// Init 按配置初始化 logger：控制台 text 输出 + （可选）文件 JSON 输出。
// cfg.Level 为空时默认 info；无法识别的级别返回错误。
// 注意：文件句柄由 logger 持有直至进程退出，无需调用方关闭。
func Init(cfg model.LogConfig) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	handlers := make([]slog.Handler, 0, 2)
	handlers = append(handlers, slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	if cfg.File != "" {
		w, err := newRotatingWriter(cfg.File, cfg.MaxSizeMB, cfg.MaxBackups)
		if err != nil {
			return nil, err
		}
		handlers = append(handlers, slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
	}

	var h slog.Handler
	if len(handlers) == 1 {
		h = handlers[0]
	} else {
		h = &fanoutHandler{handlers: handlers}
	}
	return slog.New(h), nil
}

// Default 返回仅控制台的默认 logger（info 级别）。
func Default() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// parseLevel 解析 debug/info/warn/error；空串默认 info。
func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (expect debug|info|warn|error)", s)
	}
}

// fanoutHandler 将日志记录转发给多个子 handler。
// 标准库不提供多输出 handler，这里实现 slog.Handler 接口补齐。
type fanoutHandler struct {
	handlers []slog.Handler
}

func (f *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range f.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		// Clone 避免多个 handler 共享 Record 内部的可复用切片
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (f *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	hs := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		hs[i] = h.WithAttrs(attrs)
	}
	return &fanoutHandler{handlers: hs}
}

func (f *fanoutHandler) WithGroup(name string) slog.Handler {
	hs := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		hs[i] = h.WithGroup(name)
	}
	return &fanoutHandler{handlers: hs}
}

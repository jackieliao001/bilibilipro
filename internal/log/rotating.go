package log

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// rotatingWriter 按大小轮转的日志文件 writer：
// 文件超过阈值时重命名归档（path -> path.1，旧备份后移），
// 最多保留 maxBackups 份历史备份（超限删除最老），确保服务器长期运行日志不无限增长。
type rotatingWriter struct {
	mu         sync.Mutex
	path       string
	maxSize    int64
	maxBackups int
	file       *os.File
	size       int64
}

// newRotatingWriter 创建轮转 writer。maxSizeMB<=0 时默认 10MB，maxBackups<=0 时默认 5 份。
func newRotatingWriter(path string, maxSizeMB, maxBackups int) (*rotatingWriter, error) {
	if maxSizeMB <= 0 {
		maxSizeMB = 10
	}
	if maxBackups <= 0 {
		maxBackups = 5
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	w := &rotatingWriter{
		path:       path,
		maxSize:    int64(maxSizeMB) * 1024 * 1024,
		maxBackups: maxBackups,
	}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

// Write 实现 io.Writer：超限自动轮转。
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil || w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Close 关闭当前文件。
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// open 打开（或创建）日志文件并初始化大小。
func (w *rotatingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", w.path, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("stat log file %s: %w", w.path, err)
	}
	w.file = f
	w.size = info.Size()
	return nil
}

// rotate 执行轮转：path -> path.1，旧备份依次后移，删除超过 maxBackups 的备份。
func (w *rotatingWriter) rotate() error {
	if w.file != nil {
		w.file.Close()
		w.file = nil
	}
	// 从最老的开始后移（保留 maxBackups 份，编号 1..maxBackups）
	for i := w.maxBackups - 1; i >= 1; i-- {
		oldName := fmt.Sprintf("%s.%d", w.path, i)
		newName := fmt.Sprintf("%s.%d", w.path, i+1)
		if _, err := os.Stat(oldName); err == nil {
			// 删除超过上限的最老备份
			if i+1 > w.maxBackups {
				os.Remove(oldName)
				continue
			}
			if err := os.Rename(oldName, newName); err != nil {
				return fmt.Errorf("rotate log backup: %w", err)
			}
		}
	}
	// 当前文件 -> .1
	if _, err := os.Stat(w.path); err == nil {
		if err := os.Rename(w.path, w.path+".1"); err != nil {
			return fmt.Errorf("rotate log file: %w", err)
		}
	}
	return w.open()
}

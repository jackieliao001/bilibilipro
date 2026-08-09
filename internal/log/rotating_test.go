package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRotatingWriterRotates 验证超过阈值后触发轮转（生成 .1 备份并新建主文件）。
func TestRotatingWriterRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := newRotatingWriter(path, 1, 2) // 1MB 阈值、2 份备份
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	defer w.Close()

	// 写 1MB+ 数据（分多次写，保证触发轮转）
	chunk := strings.Repeat("a", 64*1024)
	for i := 0; i < 20; i++ {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// 应存在主文件与 .1 备份
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("主文件不存在: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf(".1 备份不存在（未触发轮转）: %v", err)
	}
}

// TestRotatingWriterBackupLimit 验证备份数不超过 maxBackups。
func TestRotatingWriterBackupLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test2.log")

	w, err := newRotatingWriter(path, 1, 3) // 1MB 阈值、最多 3 份备份
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	defer w.Close()

	chunk := strings.Repeat("b", 128*1024)
	for i := 0; i < 30; i++ { // 写 3.75MB，应触发 3 次以上轮转
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// 备份编号最大不超过 maxBackups（.3 存在但 .4 不应存在）
	if _, err := os.Stat(path + ".3"); err != nil {
		t.Fatalf(".3 备份不存在: %v", err)
	}
	if _, err := os.Stat(path + ".4"); err == nil {
		t.Fatalf(".4 不应存在（超过备份上限）")
	}
}

// TestRotatingWriterAppend 验证轮转后主文件可继续追加写入。
func TestRotatingWriterAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test3.log")

	w, err := newRotatingWriter(path, 1, 2)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	defer w.Close()

	chunk := strings.Repeat("c", 64*1024)
	for i := 0; i < 20; i++ {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// 轮转后再写，主文件应可写
	if _, err := w.Write([]byte("after-rotate")); err != nil {
		t.Fatalf("轮转后写入失败: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读主文件: %v", err)
	}
	if !strings.Contains(string(data), "after-rotate") {
		t.Fatalf("轮转后写入内容丢失")
	}
}

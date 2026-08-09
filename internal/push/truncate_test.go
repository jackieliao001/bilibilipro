package push

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateMessageShortUnchanged(t *testing.T) {
	msg := "short message"
	if got := truncateMessage(msg, 100); got != msg {
		t.Fatalf("短消息不应被截断: %q", got)
	}
}

func TestTruncateMessageAscii(t *testing.T) {
	msg := strings.Repeat("x", 5000)
	got := truncateMessage(msg, 4096)
	if len(got) > 4096 {
		t.Fatalf("长度 = %d, 超过上限", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("应以 ... 结尾: %q", got[len(got)-10:])
	}
}

// TestTruncateMessageUTF8 验证多字节字符按 UTF-8 边界截断（结果合法 UTF-8 且不超限）。
func TestTruncateMessageUTF8(t *testing.T) {
	msg := strings.Repeat("你", 3000) // 3 字节/字符，共 9000 字节
	got := truncateMessage(msg, 100)
	if len(got) > 100 {
		t.Fatalf("长度 = %d, 超过上限 100", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("截断结果不是合法 UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("应以 ... 结尾")
	}
	// 混合 ASCII 与多字节
	mixed := "a" + strings.Repeat("界", 100)
	got2 := truncateMessage(mixed, 10)
	if !utf8.ValidString(got2) || len(got2) > 10 {
		t.Fatalf("混合截断非法: %q (len=%d)", got2, len(got2))
	}
}

func TestTruncateMessageSmallBudget(t *testing.T) {
	// 上限小于省略号长度 → 返回空串
	if got := truncateMessage("hello", 2); got != "" {
		t.Fatalf("上限过小时应返回空串: %q", got)
	}
	if got := truncateMessage("hello", 0); got != "hello" {
		t.Fatalf("maxBytes<=0 时不应截断: %q", got)
	}
}

func TestTruncateChars(t *testing.T) {
	msg := strings.Repeat("你", 5000) // 5000 字符
	got := truncateChars(msg, 4096)
	if got == msg {
		t.Fatal("超长消息应被截断")
	}
	runes := []rune(got)
	if len(runes) != 4096 {
		t.Fatalf("字符数 = %d, want 4096（含省略号）", len(runes))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatal("应以 ... 结尾")
	}
	// 未超限不截断
	if got := truncateChars("abc", 4096); got != "abc" {
		t.Fatalf("短消息不应被截断: %q", got)
	}
}

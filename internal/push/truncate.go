package push

// 各渠道单条消息长度上限（参考各平台官方限制）。
const (
	// serverChanMaxBytes Server酱 desp 字段上限（32KB）。
	serverChanMaxBytes = 32 * 1024
	// workWeixinMaxBytes 企业微信机器人 markdown content 上限（4096 字节）。
	workWeixinMaxBytes = 4096
	// telegramMaxChars Telegram text 上限（4096 字符）。
	telegramMaxChars = 4096
)

// truncateMessage 将消息截断到不超过 maxBytes 字节。
// 在 UTF-8 字符边界截断，避免产生非法字节序列；超限时在末尾追加 "..."（计入上限）。
func truncateMessage(msg string, maxBytes int) string {
	if maxBytes <= 0 || len(msg) <= maxBytes {
		return msg
	}
	budget := maxBytes - len("...")
	if budget <= 0 {
		return ""
	}
	b := []byte(msg)
	end := budget
	for end > 0 && end < len(b) && b[end]&0xC0 == 0x80 {
		end--
	}
	return string(b[:end]) + "..."
}

// truncateChars 将消息截断到不超过 maxChars 个字符（按 rune 计数，
// 适用于 Telegram 的 4096 字符限制）；超限时在末尾追加 "..."（计入上限）。
func truncateChars(msg string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	runes := []rune(msg)
	if len(runes) <= maxChars {
		return msg
	}
	budget := maxChars - len("...")
	if budget <= 0 {
		return ""
	}
	return string(runes[:budget]) + "..."
}

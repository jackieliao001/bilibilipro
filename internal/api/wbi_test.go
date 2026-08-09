package api

import (
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// biliTestKeys 使用 B 站官方文档示例中的 img_key/sub_key 作为已知输入，
// 通过自洽校验（mixinKey 推导 + 参数过滤排序 + md5）验证算法链条，
// 不依赖记忆的 w_rid 测试向量。
const (
	testImgKey = "7cd084941338484aae65ad9425bdf888"
	testSubKey = "4932caff0ff746eab6f01bf08b70ac45"
)

func TestGetMixinKeyLength(t *testing.T) {
	mk := GetMixinKey(testImgKey + testSubKey)
	if len(mk) != 32 {
		t.Fatalf("GetMixinKey len = %d, want 32", len(mk))
	}
}

func TestGetMixinKeyDeterministic(t *testing.T) {
	a := GetMixinKey(testImgKey + testSubKey)
	b := GetMixinKey(testImgKey + testSubKey)
	if a != b {
		t.Fatalf("相同输入产生不同结果: %q vs %q", a, b)
	}
}

func TestGetMixinKeyShortInput(t *testing.T) {
	if mk := GetMixinKey("short"); mk != "" {
		t.Fatalf("短输入应返回空串, got %q", mk)
	}
	if mk := GetMixinKey(""); mk != "" {
		t.Fatalf("空输入应返回空串, got %q", mk)
	}
	// 恰好 64 字符
	exact := strings.Repeat("a", 64)
	if mk := GetMixinKey(exact); len(mk) != 32 {
		t.Fatalf("64 字符输入应返回 32 位, got %d: %q", len(mk), mk)
	}
}

// TestEncWbiFullChain 校验 EncWbi 的完整算法链：
// 参数过滤（空值/特殊字符）→ 追加 wts → 按键排序 → url.QueryEscape 拼接 → md5(query+mixinKey)。
func TestEncWbiFullChain(t *testing.T) {
	params := map[string]string{
		"foo":     "114",
		"bar":     "514",
		"baz":     "1919810",
		"empty":   "",
		"special": "a'b(c)", // 含 ' 与 (，应被过滤
	}
	wrid, wts := EncWbi(params, testImgKey, testSubKey)

	// 1. wts 已写回参数，且为 10 位 Unix 秒时间戳。
	if params["wts"] != wts {
		t.Fatalf("wts 未写回 params: params=%q, wts=%q", params["wts"], wts)
	}
	if len(wts) != 10 {
		t.Fatalf("wts 长度 = %d, want 10", len(wts))
	}
	if _, err := strconv.ParseInt(wts, 10, 64); err != nil {
		t.Fatalf("wts 非数字: %q", wts)
	}

	// 2. w_rid 为 32 位小写 hex。
	if len(wrid) != 32 {
		t.Fatalf("w_rid 长度 = %d, want 32", len(wrid))
	}
	if _, err := hex.DecodeString(wrid); err != nil {
		t.Fatalf("w_rid 不是合法 hex: %q (%v)", wrid, err)
	}
	if wrid != strings.ToLower(wrid) {
		t.Fatalf("w_rid 应全小写: %q", wrid)
	}

	// 3. 重新计算期望值：过滤 + 排序后为 bar, baz, foo, wts。
	mixinKey := GetMixinKey(testImgKey + testSubKey)
	keys := []string{"bar", "baz", "foo", "wts"}
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(url.QueryEscape(k))
		sb.WriteByte('=')
		sb.WriteString(url.QueryEscape(params[k]))
	}
	sum := md5.Sum([]byte(sb.String() + mixinKey))
	want := hex.EncodeToString(sum[:])
	if wrid != want {
		t.Fatalf("w_rid 与自算期望不符:\n got  %q\n want %q\n query=%q mixinKey=%q", wrid, want, sb.String(), mixinKey)
	}
}

// TestEncWbiDeterministic 相同输入（同一秒内）产生相同输出。
// 跨秒边界时 wts 会变化，故重试直到两次调用落在同一秒。
func TestEncWbiDeterministic(t *testing.T) {
	params := func() map[string]string {
		return map[string]string{"foo": "114", "bar": "514", "baz": "1919810"}
	}
	for i := 0; i < 10; i++ {
		p1 := params()
		wrid1, wts1 := EncWbi(p1, testImgKey, testSubKey)
		p2 := params()
		wrid2, wts2 := EncWbi(p2, testImgKey, testSubKey)
		if wts1 == wts2 {
			if wrid1 != wrid2 {
				t.Fatalf("同一 wts 下结果不同: %q vs %q", wrid1, wrid2)
			}
			return
		}
	}
	t.Fatal("10 次尝试均跨越秒边界，无法验证确定性")
}

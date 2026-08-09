package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validRaw 构造一个完整可用的 cookie 串（含 buvid3 与扩展字段，用于 String 稳定性检查）。
func validRaw(uid string) string {
	return "DedeUserID=" + uid + "; SESSDATA=sess-" + uid + "; bili_jct=jct-" + uid + "; buvid3=bv3-" + uid + "; CURRENT_LEVEL=5"
}

func mustParse(t *testing.T, raw string) *Cookie {
	t.Helper()
	c, err := ParseCookie(raw)
	if err != nil {
		t.Fatalf("ParseCookie(%q) 意外失败: %v", raw, err)
	}
	return c
}

func TestParseCookieNormal(t *testing.T) {
	c := mustParse(t, "DedeUserID=123; SESSDATA=abcdef; bili_jct=jct123; buvid3=xyz; OTHER=1")
	if c.DedeUserID != "123" || c.SESSDATA != "abcdef" || c.BiliJCT != "jct123" || c.Buvid3 != "xyz" {
		t.Fatalf("字段解析错误: %+v", c)
	}
	// raw 应包含所有键值（含非命名字段）。
	if c.raw["OTHER"] != "1" {
		t.Fatalf("raw 缺少扩展字段 OTHER: %+v", c.raw)
	}
}

func TestParseCookieCaseInsensitive(t *testing.T) {
	c := mustParse(t, "dedeuserid=42; sessdata=SS; bili_jct=JJ")
	if c.DedeUserID != "42" || c.SESSDATA != "SS" || c.BiliJCT != "JJ" {
		t.Fatalf("大小写不敏感解析失败: %+v", c)
	}
}

func TestParseCookiePartial(t *testing.T) {
	// 只含三项中任一项也允许解析成功（缺失字段保持为空）。
	c := mustParse(t, "SESSDATA=only-sess")
	if c.SESSDATA != "only-sess" || c.DedeUserID != "" {
		t.Fatalf("部分解析失败: %+v", c)
	}
}

func TestParseCookieEmpty(t *testing.T) {
	for _, raw := range []string{"", "   ", "; ; "} {
		if _, err := ParseCookie(raw); err == nil {
			t.Fatalf("ParseCookie(%q) 应返回错误", raw)
		}
	}
}

func TestParseCookieMalformed(t *testing.T) {
	// 不含任何关键字段 → 错误。
	if _, err := ParseCookie("foo=bar; baz=qux"); err == nil {
		t.Fatal("不含关键字段的 cookie 应返回错误")
	}
	// 无 "=" 的片段应被静默跳过，不 panic。
	c := mustParse(t, "DedeUserID=1; garbage; SESSDATA=s; bili_jct=j")
	if c.DedeUserID != "1" || c.SESSDATA != "s" {
		t.Fatalf("畸形片段处理错误: %+v", c)
	}
}

func TestCookieValidate(t *testing.T) {
	good := mustParse(t, validRaw("100"))
	if err := good.Validate(); err != nil {
		t.Fatalf("有效 cookie 校验失败: %v", err)
	}

	var nilCk *Cookie
	if err := nilCk.Validate(); err == nil {
		t.Fatal("nil cookie 应校验失败")
	}

	cases := []struct {
		name string
		raw  string
	}{
		{"缺 DedeUserID", "SESSDATA=s; bili_jct=j"},
		{"DedeUserID 非数字", "DedeUserID=abc; SESSDATA=s; bili_jct=j"},
		{"缺 SESSDATA", "DedeUserID=1; bili_jct=j"},
		{"缺 bili_jct", "DedeUserID=1; SESSDATA=s"},
	}
	for _, tc := range cases {
		c, err := ParseCookie(tc.raw)
		if err != nil {
			t.Fatalf("%s: ParseCookie 意外失败: %v", tc.name, err)
		}
		if verr := c.Validate(); verr == nil {
			t.Fatalf("%s: 应校验失败", tc.name)
		}
	}
}

func TestCookieUserID(t *testing.T) {
	if id := mustParse(t, "DedeUserID=987654").UserID(); id != 987654 {
		t.Fatalf("UserID() = %d, want 987654", id)
	}
	var nilCk *Cookie
	if id := nilCk.UserID(); id != 0 {
		t.Fatalf("nil UserID() = %d, want 0", id)
	}
	if id := mustParse(t, "SESSDATA=s").UserID(); id != 0 {
		t.Fatalf("无 DedeUserID 时 UserID() = %d, want 0", id)
	}
}

func TestCookieString(t *testing.T) {
	c := mustParse(t, validRaw("7"))
	got := c.String()
	// 键按字典序输出：bili_jct, buvid3, CURRENT_LEVEL, DedeUserID, SESSDATA
	want := "CURRENT_LEVEL=5; DedeUserID=7; SESSDATA=sess-7; bili_jct=jct-7; buvid3=bv3-7"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	// nil / 空 raw 返回空串
	var nilCk *Cookie
	if s := nilCk.String(); s != "" {
		t.Fatalf("nil String() = %q, want empty", s)
	}
	empty := &Cookie{}
	if s := empty.String(); s != "" {
		t.Fatalf("empty String() = %q, want empty", s)
	}
}

func TestCookieMergeSetCookie(t *testing.T) {
	c := mustParse(t, "DedeUserID=1; SESSDATA=old; bili_jct=j")
	c.MergeSetCookie([]string{
		"SESSDATA=new-sess; Path=/; HttpOnly",
		"buvid3=xyz123; Domain=.bilibili.com; Expires=Wed, 21 Oct 2026 07:28:00 GMT",
		"",               // 空 header 忽略
		"no-equals-sign", // 无 "=" 忽略
	})
	if c.SESSDATA != "new-sess" {
		t.Fatalf("SESSDATA 未合并: %q", c.SESSDATA)
	}
	if c.Buvid3 != "xyz123" {
		t.Fatalf("Buvid3 未合并: %q", c.Buvid3)
	}
	if c.raw["SESSDATA"] != "new-sess" || c.raw["buvid3"] != "xyz123" {
		t.Fatalf("raw 未同步: %+v", c.raw)
	}
	// 属性段（Path= 等）不应进入 raw。
	if _, ok := c.raw["Path"]; ok {
		t.Fatalf("cookie 属性被误入 raw: %+v", c.raw)
	}
	// nil 接收者安全
	var nilCk *Cookie
	nilCk.MergeSetCookie([]string{"buvid3=x"})
}

func writeTempFile(t *testing.T, name, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}
	return path
}

func TestParseCookiesFromFileFormatCookies(t *testing.T) {
	path := writeTempFile(t, "cookies.json", `{
  "cookies": [
    {"name": "a", "cookie_str": "DedeUserID=1; SESSDATA=s1; bili_jct=j1"},
    {"name": "b", "cookie_str": "DedeUserID=2; SESSDATA=s2; bili_jct=j2; buvid3=b2"}
  ]
}`)
	cookies, err := ParseCookiesFromFile(path)
	if err != nil {
		t.Fatalf("ParseCookiesFromFile 失败: %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("got %d cookies, want 2", len(cookies))
	}
	if cookies[0].DedeUserID != "1" || cookies[1].DedeUserID != "2" {
		t.Fatalf("cookie 顺序/内容错误: %+v", cookies)
	}
	if cookies[1].Buvid3 != "b2" {
		t.Fatalf("buvid3 丢失: %+v", cookies[1])
	}
}

func TestParseCookiesFromFileFormatBiliBili(t *testing.T) {
	path := writeTempFile(t, "legacy.json", `{
  "BiliBiliCookies": [
    "DedeUserID=10; SESSDATA=s10; bili_jct=j10",
    "DedeUserID=11; SESSDATA=s11; bili_jct=j11"
  ]
}`)
	cookies, err := ParseCookiesFromFile(path)
	if err != nil {
		t.Fatalf("ParseCookiesFromFile 失败: %v", err)
	}
	if len(cookies) != 2 || cookies[0].DedeUserID != "10" || cookies[1].DedeUserID != "11" {
		t.Fatalf("BiliBiliCookies 格式解析错误: %+v", cookies)
	}
}

func TestParseCookiesFromFileSkipsInvalidAndDuplicate(t *testing.T) {
	path := writeTempFile(t, "mixed.json", `{
  "cookies": [
    {"name": "valid", "cookie_str": "DedeUserID=1; SESSDATA=s1; bili_jct=j1"},
    {"name": "dup", "cookie_str": "DedeUserID=1; SESSDATA=s1-dup; bili_jct=j1"},
    {"name": "bad-parse", "cookie_str": "foo=bar"},
    {"name": "bad-validate", "cookie_str": "DedeUserID=abc; SESSDATA=s; bili_jct=j"}
  ]
}`)
	cookies, err := ParseCookiesFromFile(path)
	if err != nil {
		t.Fatalf("ParseCookiesFromFile 失败: %v", err)
	}
	if len(cookies) != 1 || cookies[0].DedeUserID != "1" {
		t.Fatalf("应只保留 1 个有效 cookie: %+v", cookies)
	}
}

func TestParseCookiesFromFileAllInvalid(t *testing.T) {
	path := writeTempFile(t, "all-invalid.json", `{"cookies": [{"name": "x", "cookie_str": "foo=bar"}]}`)
	cookies, err := ParseCookiesFromFile(path)
	if err != nil {
		t.Fatalf("全部无效条目不应报错: %v", err)
	}
	if len(cookies) != 0 {
		t.Fatalf("got %d cookies, want 0", len(cookies))
	}
}

func TestParseCookiesFromFileMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.json")
	if _, err := ParseCookiesFromFile(path); err == nil {
		t.Fatal("文件不存在应返回错误")
	}
}

func TestParseCookiesFromFileBadJSON(t *testing.T) {
	path := writeTempFile(t, "bad.json", `{not json`)
	if _, err := ParseCookiesFromFile(path); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
}

func TestSaveCookiesToFileRoundTrip(t *testing.T) {
	c1 := mustParse(t, validRaw("1001"))
	c2 := mustParse(t, validRaw("1002"))
	dup := mustParse(t, validRaw("1001")) // 与 c1 同 uid，保存时应去重
	var nilCk *Cookie                     // nil 应被跳过

	path := filepath.Join(t.TempDir(), "out.json")
	if err := SaveCookiesToFile([]*Cookie{c1, c2, dup, nilCk}, path); err != nil {
		t.Fatalf("SaveCookiesToFile 失败: %v", err)
	}

	// 文件结构为 {"cookies":[...]}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取保存文件失败: %v", err)
	}
	if !strings.Contains(string(data), `"cookies"`) {
		t.Fatalf("输出缺少 cookies 顶层字段: %s", data)
	}

	back, err := ParseCookiesFromFile(path)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if len(back) != 2 {
		t.Fatalf("去重后应为 2 个 cookie, got %d", len(back))
	}
	if back[0].DedeUserID != "1001" || back[1].DedeUserID != "1002" {
		t.Fatalf("回读内容错误: %+v", back)
	}
	if back[0].SESSDATA != "sess-1001" || back[0].BiliJCT != "jct-1001" || back[0].Buvid3 != "bv3-1001" {
		t.Fatalf("回读字段丢失: %+v", back[0])
	}
}

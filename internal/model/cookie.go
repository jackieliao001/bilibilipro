package model

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Cookie 表示一个 B 站账号的完整 Cookie 集合。
type Cookie struct {
	DedeUserID string `json:"DedeUserID"`
	SESSDATA   string `json:"SESSDATA"`
	BiliJCT    string `json:"bili_jct"`
	Buvid3     string `json:"buvid3,omitempty"`

	raw map[string]string // 原始 k=v 键值对（未导出）
}

// ParseCookie 解析形如 "k1=v1; k2=v2" 的 cookie 字符串。
// 要求至少能解析出 DedeUserID / SESSDATA / bili_jct 中的一项，否则返回错误；
// 缺失的字段不报错，仅保持为空。
func ParseCookie(raw string) (*Cookie, error) {
	c := &Cookie{raw: make(map[string]string)}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty cookie string")
	}

	hasDedeUserID, hasSESSDATA, hasBiliJCT := false, false, false
	for _, seg := range strings.Split(raw, ";") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		kv := strings.SplitN(seg, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		if k == "" {
			continue
		}
		c.raw[k] = v
		switch {
		case strings.EqualFold(k, "DedeUserID"):
			c.DedeUserID = v
			hasDedeUserID = true
		case strings.EqualFold(k, "SESSDATA"):
			c.SESSDATA = v
			hasSESSDATA = true
		case strings.EqualFold(k, "bili_jct"):
			c.BiliJCT = v
			hasBiliJCT = true
		case strings.EqualFold(k, "buvid3"):
			c.Buvid3 = v
		}
	}
	if !hasDedeUserID && !hasSESSDATA && !hasBiliJCT {
		return nil, fmt.Errorf("cookie string contains none of DedeUserID/SESSDATA/bili_jct")
	}
	return c, nil
}

// Validate 检查 Cookie 是否为可用的账号凭证。
// DedeUserID 必须非空且可转换为 int64，SESSDATA、bili_jct 必须非空。
func (c *Cookie) Validate() error {
	if c == nil {
		return fmt.Errorf("cookie is nil")
	}
	if c.DedeUserID == "" {
		return fmt.Errorf("cookie missing DedeUserID")
	}
	if _, err := strconv.ParseInt(c.DedeUserID, 10, 64); err != nil {
		return fmt.Errorf("invalid DedeUserID %q: %w", c.DedeUserID, err)
	}
	if c.SESSDATA == "" {
		return fmt.Errorf("cookie missing SESSDATA")
	}
	if c.BiliJCT == "" {
		return fmt.Errorf("cookie missing bili_jct")
	}
	return nil
}

// String 从 raw 序列化完整的 cookie 串，用于 HTTP Cookie 头。
// 键按字典序输出以保证结果稳定；B 站 cookie 值一般不含逗号，直接原样输出。
func (c *Cookie) String() string {
	if c == nil || len(c.raw) == 0 {
		return ""
	}
	keys := make([]string, 0, len(c.raw))
	for k := range c.raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+c.raw[k])
	}
	return strings.Join(parts, "; ")
}

// UserID 返回 DedeUserID 的 int64 形式，解析失败返回 0。
func (c *Cookie) UserID() int64 {
	if c == nil {
		return 0
	}
	id, _ := strconv.ParseInt(c.DedeUserID, 10, 64)
	return id
}

// MergeSetCookie 合并 Set-Cookie 响应头（如登录轮询返回的 Set-Cookie）。
// 每个 header 只取第一段 "k=v"（忽略 Path=/、HttpOnly 等属性），
// 并入 raw 并同步刷新命名字段（buvid3 等）。
func (c *Cookie) MergeSetCookie(headers []string) {
	if c == nil || len(headers) == 0 {
		return
	}
	if c.raw == nil {
		c.raw = make(map[string]string)
	}
	for _, h := range headers {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		seg := h
		if idx := strings.Index(h, ";"); idx >= 0 {
			seg = h[:idx]
		}
		seg = strings.TrimSpace(seg)
		kv := strings.SplitN(seg, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		if k == "" {
			continue
		}
		c.raw[k] = v
		c.refreshNamed(k, v)
	}
}

// refreshNamed 将 raw 中的键值同步到命名字段。
func (c *Cookie) refreshNamed(k, v string) {
	switch {
	case strings.EqualFold(k, "DedeUserID"):
		c.DedeUserID = v
	case strings.EqualFold(k, "SESSDATA"):
		c.SESSDATA = v
	case strings.EqualFold(k, "bili_jct"):
		c.BiliJCT = v
	case strings.EqualFold(k, "buvid3"):
		c.Buvid3 = v
	}
}

// cookiesFile 兼容两种 JSON 结构：
//   - {"cookies":[{"name":"","cookie_str":""}]}（优先）
//   - {"BiliBiliCookies":["cookie1","cookie2"]}
type cookiesFile struct {
	Cookies []struct {
		Name      string `json:"name"`
		CookieStr string `json:"cookie_str"`
	} `json:"cookies"`
	BiliBiliCookies []string `json:"BiliBiliCookies"`
}

// ParseCookiesFromFile 从 JSON 文件加载 cookie 列表。
// 无效（解析失败或 Validate 失败）及重复（按 DedeUserID）的条目跳过并记录日志。
func ParseCookiesFromFile(path string) ([]*Cookie, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cookies file: %w", err)
	}
	var f cookiesFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse cookies file %s: %w", path, err)
	}

	var cookies []*Cookie
	seen := make(map[int64]bool)
	add := func(name, raw string) {
		ck, perr := ParseCookie(raw)
		if perr != nil {
			slog.Warn("跳过无效 cookie", "name", name, "error", perr)
			return
		}
		if verr := ck.Validate(); verr != nil {
			slog.Warn("跳过无效 cookie", "name", name, "error", verr)
			return
		}
		id := ck.UserID()
		if seen[id] {
			slog.Warn("跳过重复 cookie", "name", name, "uid", id)
			return
		}
		seen[id] = true
		cookies = append(cookies, ck)
	}

	if len(f.Cookies) > 0 {
		for _, e := range f.Cookies {
			add(e.Name, e.CookieStr)
		}
	} else {
		for i, raw := range f.BiliBiliCookies {
			add(fmt.Sprintf("BiliBiliCookies[%d]", i), raw)
		}
	}
	return cookies, nil
}

// SaveCookiesToFile 将 cookie 列表写入 JSON 文件，格式为
// {"cookies":[{"name":"账号{DedeUserID}","cookie_str":"..."}]}，按 DedeUserID 去重。
func SaveCookiesToFile(cookies []*Cookie, path string) error {
	type entry struct {
		Name      string `json:"name"`
		CookieStr string `json:"cookie_str"`
	}
	file := struct {
		Cookies []entry `json:"cookies"`
	}{Cookies: make([]entry, 0)}

	seen := make(map[int64]bool)
	for _, c := range cookies {
		if c == nil {
			continue
		}
		id := c.UserID()
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		file.Cookies = append(file.Cookies, entry{
			Name:      fmt.Sprintf("账号%d", id),
			CookieStr: c.String(),
		})
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cookies: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write cookies file: %w", err)
	}
	return nil
}

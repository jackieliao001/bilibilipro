package model

import "strings"

// BiliApiResponse 是 B 站 API 的泛型响应包装。
type BiliApiResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	TTL     int    `json:"ttl,omitempty"`
	Data    T      `json:"data,omitempty"`
}

// UserInfo 用户信息（/x/web-interface/nav）。
type UserInfo struct {
	Mid       int64      `json:"mid"`
	Uname     string     `json:"uname"`
	IsLogin   bool       `json:"isLogin"`
	Money     float64    `json:"money"`
	LevelInfo *LevelInfo `json:"level_info"`
	VipStatus int        `json:"vipStatus"`
	VipType   int        `json:"vipType"`
	WbiImg    WbiImg     `json:"wbi_img"`
}

// LevelInfo 用户等级信息。
type LevelInfo struct {
	CurrentLevel int   `json:"current_level"`
	CurrentExp   int64 `json:"current_exp"`
}

// WbiImg WBI 签名图片（用于生成 mixin key）。
type WbiImg struct {
	ImgURL string `json:"img_url"`
	SubURL string `json:"sub_url"`
}

// ImgKey 从 img_url 提取 img key（最后一段路径，不含扩展名）。
// 例如 https://i0.hdslb.com/bfs/wbi/xxx.png → xxx
func (w WbiImg) ImgKey() string {
	return w.key(w.ImgURL)
}

// SubKey 从 sub_url 提取 sub key（最后一段路径，不含扩展名）。
// 例如 https://i0.hdslb.com/bfs/wbi/xxx.png → xxx
func (w WbiImg) SubKey() string {
	return w.key(w.SubURL)
}

// key 提取 URL 最后一段路径并去掉文件扩展名。
func (w WbiImg) key(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	// 去掉 query / fragment
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	// 取最后一段路径
	u = strings.TrimRight(u, "/")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[i+1:]
	}
	// 去掉扩展名（如 .png）
	if i := strings.LastIndex(u, "."); i > 0 {
		u = u[:i]
	}
	return u
}

// DailyTaskInfo 每日任务奖励信息（/x/member/web/exp/reward）。
type DailyTaskInfo struct {
	Login bool `json:"login"`
	Watch bool `json:"watch"`
	Coins int  `json:"coins"`
	Share bool `json:"share"`
}

// VideoInfo 视频信息。
type VideoInfo struct {
	Aid   int64  `json:"aid"`
	Bvid  string `json:"bvid"`
	Cid   int64  `json:"cid"`
	Title string `json:"title"`
	Mid   int64  `json:"mid"`
}

// UpInfo UP 主信息。
type UpInfo struct {
	Mid   int64  `json:"mid"`
	Uname string `json:"uname"`
}

// TagInfo 关注分组信息。
type TagInfo struct {
	TagID int64  `json:"tagid"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

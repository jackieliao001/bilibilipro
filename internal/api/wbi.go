package api

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

// wbiMixinKeyTab B 站 WBI 签名混淆表（固定 64 位映射）。
var wbiMixinKeyTab = [64]int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35,
	27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13,
	37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4,
	22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52,
}

// wbiCacheTTL WBI keys 缓存时长（B 站 key 约每两天轮换一次，缓存一天足够）。
const wbiCacheTTL = 24 * time.Hour

// GetMixinKey 由 img_key+sub_key 拼接串按混淆表映射后取前 32 位得到 mixin key。
func GetMixinKey(orig string) string {
	if len(orig) < 64 {
		return ""
	}
	var sb strings.Builder
	sb.Grow(64)
	for _, idx := range wbiMixinKeyTab {
		sb.WriteByte(orig[idx])
	}
	return sb.String()[:32]
}

// EncWbi 对参数做 WBI 签名：追加 wts → 过滤空值与含特殊字符的值 → 编码并按键排序
// → 拼接 query → md5(query+mixinKey)。返回 w_rid 与 wts（调用方需把它们写回参数）。
func EncWbi(params map[string]string, imgKey, subKey string) (wrid, wts string) {
	mixinKey := GetMixinKey(imgKey + subKey)
	wts = strconv.FormatInt(time.Now().Unix(), 10)
	params["wts"] = wts

	keys := make([]string, 0, len(params))
	for k, v := range params {
		if v == "" || strings.ContainsAny(v, "!'()*") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

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
	return hex.EncodeToString(sum[:]), wts
}

// wbiCacheEntry WBI keys 缓存项。
type wbiCacheEntry struct {
	imgKey    string
	subKey    string
	fetchedAt time.Time
}

// WbiService 提供 WBI 签名所需的 img_key/sub_key，按账号 DedeUserID 缓存；
// 通过注入的 fetchFn 获取 keys（避免与 bilibili 包循环依赖）。
type WbiService struct {
	fetchFn func(ctx context.Context, ck *model.Cookie) (*model.WbiImg, error)
	mu      sync.Mutex
	cache   map[string]*wbiCacheEntry
}

// NewWbiService 创建 WBI 服务。
// fetchFn 负责从 nav 接口获取 WbiImg，由 bilibili.New 组装时注入。
func NewWbiService(fetchFn func(ctx context.Context, ck *model.Cookie) (*model.WbiImg, error)) *WbiService {
	return &WbiService{
		fetchFn: fetchFn,
		cache:   make(map[string]*wbiCacheEntry),
	}
}

// GetKeys 返回指定账号的 img_key/sub_key（img_url/sub_url 的最后一段）：
// 优先使用缓存，未命中时调用 fetchFn 获取后按 DedeUserID 缓存。
func (s *WbiService) GetKeys(ctx context.Context, ck *model.Cookie) (imgKey, subKey string, err error) {
	if ck == nil {
		return "", "", fmt.Errorf("wbi: 需要登录 cookie")
	}
	id := ck.DedeUserID
	if id != "" {
		s.mu.Lock()
		if entry, ok := s.cache[id]; ok && time.Since(entry.fetchedAt) < wbiCacheTTL {
			s.mu.Unlock()
			return entry.imgKey, entry.subKey, nil
		}
		s.mu.Unlock()
	}

	if s.fetchFn == nil {
		return "", "", fmt.Errorf("wbi: 未注入 key 获取函数")
	}
	img, err := s.fetchFn(ctx, ck)
	if err != nil {
		return "", "", fmt.Errorf("获取 wbi keys 失败: %w", err)
	}
	imgKey = path.Base(img.ImgURL)
	subKey = path.Base(img.SubURL)
	if imgKey == "" || subKey == "" || imgKey == "." || subKey == "." {
		return "", "", fmt.Errorf("wbi: 无效的 img_url/sub_url")
	}

	if id != "" {
		s.mu.Lock()
		s.cache[id] = &wbiCacheEntry{imgKey: imgKey, subKey: subKey, fetchedAt: time.Now()}
		s.mu.Unlock()
	}
	return imgKey, subKey, nil
}

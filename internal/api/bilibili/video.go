package bilibili

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"strconv"
	"time"

	"github.com/jackieliao001/bilibilipro/internal/model"
)

// rankingData 排行榜接口响应 data 结构。
type rankingData struct {
	List []model.VideoInfo `json:"list"`
}

// upVideosData UP 主投稿接口响应 data 结构（data.list.vlist）。
type upVideosData struct {
	List struct {
		Vlist []model.VideoInfo `json:"vlist"`
	} `json:"list"`
}

// GetRankingVideos 获取全站排行榜视频（无需 cookie）。
func (c *BiliClient) GetRankingVideos(ctx context.Context) ([]model.VideoInfo, error) {
	resp, err := Get[rankingData](c, ctx, "https://api.bilibili.com/x/web-interface/ranking/v2?rid=0&type=all", nil, false)
	if err != nil {
		return nil, err
	}
	return resp.Data.List, nil
}

// GetVideoDetail 获取视频详情（aid → VideoInfo）。
func (c *BiliClient) GetVideoDetail(ctx context.Context, aid int64) (*model.VideoInfo, error) {
	resp, err := Get[model.VideoInfo](c, ctx, fmt.Sprintf("https://api.bilibili.com/x/web-interface/view?aid=%d", aid), nil, false)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// GetUpVideos 获取 UP 主最近投稿（WBI 签名，取前 30 条按发布时间排序）。
func (c *BiliClient) GetUpVideos(ctx context.Context, mid int64, ck *model.Cookie) ([]model.VideoInfo, error) {
	reqURL := fmt.Sprintf("https://api.bilibili.com/x/space/wbi/arc/search?mid=%d&ps=30&pn=1&order=pubdate", mid)
	resp, err := Get[upVideosData](c, ctx, reqURL, ck, true)
	if err != nil {
		return nil, err
	}
	return resp.Data.List.Vlist, nil
}

// Heartbeat 上报视频播放心跳。
func (c *BiliClient) Heartbeat(ctx context.Context, ck *model.Cookie, v *model.VideoInfo, playedTime int) error {
	form := url.Values{}
	form.Set("aid", strconv.FormatInt(v.Aid, 10))
	form.Set("bvid", v.Bvid)
	// arc/search 鏉ユ簮鐨勮棰戞棤 cid锛堜负 0锛夛紝鎸?.NET 鐢熶骇瀹炵幇鐪佺暐璇ュ瓧娈碉紝閬垮厤鏃犳晥涓婃姤
	if v.Cid != 0 {
		form.Set("cid", strconv.FormatInt(v.Cid, 10))
	}
	form.Set("mid", strconv.FormatInt(ck.UserID(), 10))
	form.Set("csrf", ck.BiliJCT)
	form.Set("played_time", strconv.Itoa(playedTime))
	form.Set("realtime", strconv.Itoa(playedTime))
	form.Set("start_ts", strconv.FormatInt(time.Now().Unix(), 10))
	form.Set("type", "3")
	form.Set("dt", "2")
	form.Set("play_type", "1")
	_, err := PostForm[any](c, ctx, "https://api.bilibili.com/x/click-interface/web/heartbeat", form, ck, false)
	return err
}

// ShareVideo 分享视频。
func (c *BiliClient) ShareVideo(ctx context.Context, ck *model.Cookie, aid int64) error {
	form := url.Values{}
	form.Set("aid", strconv.FormatInt(aid, 10))
	form.Set("csrf", ck.BiliJCT)
	form.Set("eab_x", "1")
	form.Set("ramval", strconv.Itoa(rand.Intn(18)+3)) // 随机 3-20
	form.Set("source", "web_normal")
	form.Set("ga", "1")
	_, err := PostForm[any](c, ctx, "https://api.bilibili.com/x/web-interface/share/add", form, ck, false)
	return err
}

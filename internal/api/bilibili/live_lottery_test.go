package bilibili

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// TestGetLiveAreas 分区列表端点：路径/source_id/不带 Cookie/嵌套 data 解析。
func TestGetLiveAreas(t *testing.T) {
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/xlive/web-interface/v1/index/getWebAreaList" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if got := r.URL.Query().Get("source_id"); got != "2" {
			t.Errorf("source_id = %q, want 2", got)
		}
		if got := r.Header.Get("Cookie"); got != "" {
			t.Errorf("GetLiveAreas 不应携带 Cookie: %q", got)
		}
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"data":[{"id":1,"name":"娱乐"},{"id":2,"name":"游戏"}]}}`), nil
	}))

	areas, err := c.GetLiveAreas(context.Background())
	if err != nil {
		t.Fatalf("GetLiveAreas error: %v", err)
	}
	if len(areas) != 2 || areas[0].ID != 1 || areas[0].Name != "娱乐" || areas[1].ID != 2 {
		t.Fatalf("unexpected areas: %+v", areas)
	}
}

// TestGetTianXuanList getList 端点：query 参数（platform/parent_area_id/area_id/sort_type/page/wts）、
// Referer/Origin、Pendant_info["2"] 解析。
func TestGetTianXuanList(t *testing.T) {
	ck := biliTestCookie()
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/xlive/web-interface/v1/second/getList" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if got := q.Get("platform"); got != "web" {
			t.Errorf("platform = %q, want web", got)
		}
		if got := q.Get("parent_area_id"); got != "2" {
			t.Errorf("parent_area_id = %q, want 2", got)
		}
		if got := q.Get("area_id"); got != "0" {
			t.Errorf("area_id = %q, want 0", got)
		}
		if got := q.Get("sort_type"); got != "hot" {
			t.Errorf("sort_type = %q, want hot", got)
		}
		if got := q.Get("page"); got != "2" {
			t.Errorf("page = %q, want 2", got)
		}
		if _, err := strconv.ParseInt(q.Get("wts"), 10, 64); err != nil {
			t.Errorf("wts 应为 Unix 秒: %q", q.Get("wts"))
		}
		if got := r.Header.Get("Referer"); got != "https://live.bilibili.com/" {
			t.Errorf("Referer = %q", got)
		}
		if got := r.Header.Get("Origin"); got != "https://live.bilibili.com" {
			t.Errorf("Origin = %q", got)
		}
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"new_tags":[{"id":10,"name":"热门","sort_type":"hot"}],"list":[{"roomid":1001,"uid":2002,"title":"测试直播间","uname":"主播A","Pendant_info":{"2":{"Pendent_id":504,"Content":"天选时刻"}}}],"has_more":1}}`), nil
	}))

	data, err := c.GetTianXuanList(context.Background(), ck, 2, 2, "hot")
	if err != nil {
		t.Fatalf("GetTianXuanList error: %v", err)
	}
	if len(data.List) != 1 || !data.List[0].IsTianXuan() {
		t.Fatalf("应解析出 1 个天选直播间: %+v", data.List)
	}
	if data.List[0].RoomID != 1001 || data.List[0].Uid != 2002 || data.List[0].Uname != "主播A" {
		t.Fatalf("unexpected list item: %+v", data.List[0])
	}
	if data.HasMore != 1 || len(data.NewTags) != 1 || data.NewTags[0].SortType != "hot" {
		t.Fatalf("unexpected data: %+v", data)
	}
}

// TestTianXuanRoomIsTianXuan 天选角标判定：Pendant_info 非空、含键 "2"、Pendent_id==504。
func TestTianXuanRoomIsTianXuan(t *testing.T) {
	cases := []struct {
		name string
		room TianXuanRoom
		want bool
	}{
		{"无角标", TianXuanRoom{}, false},
		{"角标为空", TianXuanRoom{PendantInfo: map[string]PendantInfo{}}, false},
		{"非2键", TianXuanRoom{PendantInfo: map[string]PendantInfo{"1": {PendentID: 504}}}, false},
		{"426百人成就", TianXuanRoom{PendantInfo: map[string]PendantInfo{"2": {PendentID: 426}}}, false},
		{"397新星主播", TianXuanRoom{PendantInfo: map[string]PendantInfo{"2": {PendentID: 397}}}, false},
		{"504天选时刻", TianXuanRoom{PendantInfo: map[string]PendantInfo{"2": {PendentID: 504}}}, true},
	}
	for _, tc := range cases {
		if got := tc.room.IsTianXuan(); got != tc.want {
			t.Fatalf("%s: IsTianXuan = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestCheckTianXuan Check 端点：roomid 参数、响应解析。
func TestCheckTianXuan(t *testing.T) {
	var gotRoomID string
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/xlive/lottery-interface/v1/Anchor/Check" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotRoomID = r.URL.Query().Get("roomid")
		if got := r.Header.Get("Referer"); got != "https://live.bilibili.com/" {
			t.Errorf("Referer = %q", got)
		}
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"id":555,"room_id":1001,"status":1,"award_name":"蓝牙耳机","award_num":1,"danmu":"测试","join_type":0,"require_type":1,"require_value":0,"require_text":"关注主播","gift_id":0,"gift_name":"","gift_num":0,"gift_price":0,"cur_gift_num":0,"send_gift_ensure":0}}`), nil
	}))

	ok, check, err := c.CheckTianXuan(context.Background(), biliTestCookie(), 1001)
	if err != nil {
		t.Fatalf("CheckTianXuan error: %v", err)
	}
	if !ok {
		t.Fatal("应 ok=true")
	}
	if check.ID != 555 || check.Status != 1 || check.AwardName != "蓝牙耳机" {
		t.Fatalf("unexpected check: %+v", check)
	}
	if check.RequireType == nil || *check.RequireType != 1 {
		t.Fatalf("require_type 应为 1: %+v", check.RequireType)
	}
	if gotRoomID != "1001" {
		t.Fatalf("roomid = %q, want 1001", gotRoomID)
	}
}

// TestCheckTianXuanNullData data 为 null 时返回 ok=false（无抽奖）。
func TestCheckTianXuanNullData(t *testing.T) {
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":null}`), nil
	}))

	ok, check, err := c.CheckTianXuan(context.Background(), biliTestCookie(), 1001)
	if err != nil {
		t.Fatalf("CheckTianXuan error: %v", err)
	}
	if ok || check != nil {
		t.Fatalf("data null 应 ok=false: ok=%v check=%+v", ok, check)
	}
}

// TestJoinTianXuan Join 端点：form 字段 id/gift_id/gift_num/csrf/csrf_token/visit_id/platform。
func TestJoinTianXuan(t *testing.T) {
	var gotBody string
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/xlive/lottery-interface/v1/Anchor/Join" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotBody = r.PostForm.Encode()
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"discount_id":0,"gold":100,"silver":200,"cur_gift_num":0,"goods_id":0,"new_order_id":0}}`), nil
	}))

	rt := 0
	check := &TianXuanCheck{ID: 555, GiftID: 11, GiftNum: 2, RequireType: &rt}
	if err := c.JoinTianXuan(context.Background(), biliTestCookie(), check); err != nil {
		t.Fatalf("JoinTianXuan error: %v", err)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("id") != "555" || form.Get("gift_id") != "11" || form.Get("gift_num") != "2" {
		t.Fatalf("unexpected id/gift fields: %s", gotBody)
	}
	if form.Get("csrf") != "test-jct" || form.Get("csrf_token") != "test-jct" {
		t.Fatalf("csrf 字段错误: %s", gotBody)
	}
	if form.Get("platform") != "pc" {
		t.Fatalf("platform = %q, want pc", form.Get("platform"))
	}
	if !visitIDRegex.MatchString(form.Get("visit_id")) {
		t.Fatalf("visit_id 格式错误: %q", form.Get("visit_id"))
	}
}

// TestLotteryVisitIDFormat visit_id：first(1-9) + 10 位小写字母数字 + last(0)。
func TestLotteryVisitIDFormat(t *testing.T) {
	for i := 0; i < 100; i++ {
		if !visitIDRegex.MatchString(randomLotteryVisitID()) {
			t.Fatalf("visit_id 格式错误: %q", randomLotteryVisitID())
		}
	}
}

// TestGetRecentFollowings 关注列表（时间倒序）：vmid/order_type 空/pn/ps/order/jsonp。
func TestGetRecentFollowings(t *testing.T) {
	var gotQuery string
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/x/relation/followings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotQuery = r.URL.RawQuery
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"list":[{"mid":1001,"uname":"up1"}]}}`), nil
	}))

	ups, err := c.GetRecentFollowings(context.Background(), biliTestCookie())
	if err != nil {
		t.Fatalf("GetRecentFollowings error: %v", err)
	}
	if len(ups) != 1 || ups[0].Mid != 1001 {
		t.Fatalf("unexpected: %+v", ups)
	}
	for _, want := range []string{"vmid=100", "order_type=", "pn=1", "ps=20", "order=desc", "jsonp=jsonp"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query %q 缺少 %q（应为关注时间倒序）", gotQuery, want)
		}
	}
}

// TestCreateRelationTag 创建关注分组：cross_domain/tag/csrf/Origin，返回 tagid。
func TestCreateRelationTag(t *testing.T) {
	var gotBody string
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/x/relation/tag/create" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("cross_domain"); got != "true" {
			t.Errorf("cross_domain = %q", got)
		}
		if got := r.Header.Get("Origin"); got != "https://space.bilibili.com" {
			t.Errorf("Origin = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotBody = r.PostForm.Encode()
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":{"tagid":99}}`), nil
	}))

	tagID, err := c.CreateRelationTag(context.Background(), biliTestCookie(), "天选时刻")
	if err != nil {
		t.Fatalf("CreateRelationTag error: %v", err)
	}
	if tagID != 99 {
		t.Fatalf("tagid = %d, want 99", tagID)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("tag") != "天选时刻" || form.Get("csrf") != "test-jct" {
		t.Fatalf("unexpected form: %s", gotBody)
	}
}

// TestCopyUpsToGroup 批量分组：fids 逗号拼接/tagids/csrf/jsonp/Origin/Referer。
func TestCopyUpsToGroup(t *testing.T) {
	var gotBody string
	c := biliTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/x/relation/tags/copyUsers" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Origin"); got != "https://space.bilibili.com" {
			t.Errorf("Origin = %q", got)
		}
		if got := r.Header.Get("Referer"); !strings.Contains(got, "fans/follow?tagid=-1") {
			t.Errorf("Referer = %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotBody = r.PostForm.Encode()
		return jsonResponse(`{"code":0,"message":"0","ttl":1,"data":null}`), nil
	}))

	if err := c.CopyUpsToGroup(context.Background(), biliTestCookie(), []int64{1001, 1002}, 55); err != nil {
		t.Fatalf("CopyUpsToGroup error: %v", err)
	}
	form, _ := url.ParseQuery(gotBody)
	if form.Get("fids") != "1001,1002" || form.Get("tagids") != "55" {
		t.Fatalf("unexpected form: %s", gotBody)
	}
	if form.Get("csrf") != "test-jct" || form.Get("jsonp") != "jsonp" {
		t.Fatalf("unexpected form: %s", gotBody)
	}
}

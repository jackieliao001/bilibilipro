package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleYAML = `
log:
  level: debug
  file: logs/app.log
bilibili:
  interval_seconds: 10
  user_agent: "UA-test"
  proxy: "http://127.0.0.1:7890"
cookies:
  - name: acct1
    cookie_str: "DedeUserID=1; SESSDATA=s1; bili_jct=j1"
tasks:
  daily:
    enabled: true
    watch_video: true
    share_video: false
    donate_coin: true
    donate_coins: 5
    protect_coins: 1
    save_coins_when_lv6: true
    donate_for_article: true
    select_like: true
    support_up_ids: "2,1024"
    device_platform: ios
  unfollow:
    enabled: true
    group_name: "天选时刻"
    count: 20
    retain_uids: "1,2"
push:
  dingtalk:
    enabled: true
    webhook_url: "https://oapi.dingtalk.com/robot/send?access_token=abc"
    secret: "SEC123"
`

func writeConfig(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	return path
}

func TestLoadConfigFull(t *testing.T) {
	path := writeConfig(t, "config.yaml", sampleYAML)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}

	if cfg.Log.Level != "debug" || cfg.Log.File != "logs/app.log" {
		t.Fatalf("Log 解析错误: %+v", cfg.Log)
	}
	if cfg.Bilibili.IntervalSeconds != 10 || cfg.Bilibili.UserAgent != "UA-test" || cfg.Bilibili.Proxy != "http://127.0.0.1:7890" {
		t.Fatalf("Bilibili 解析错误: %+v", cfg.Bilibili)
	}
	if len(cfg.Cookies) != 1 || cfg.Cookies[0].Name != "acct1" || cfg.Cookies[0].CookieStr == "" {
		t.Fatalf("Cookies 解析错误: %+v", cfg.Cookies)
	}
	d := cfg.Tasks.Daily
	if !d.Enabled || !d.WatchVideoEnabled() || d.ShareVideoEnabled() || !d.DonateCoinEnabled() ||
		d.DonateCoins != 5 || d.ProtectCoins != 1 ||
		!d.SaveCoinsWhenLv6 || !d.DonateForArticle || !d.SelectLike ||
		d.SupportUpIDs != "2,1024" || d.DevicePlatform != "ios" {
		t.Fatalf("Daily 解析错误: %+v", d)
	}
	// 显式配置的 bool 开关应解析为对应指针（非 nil）。
	if d.WatchVideo == nil || !*d.WatchVideo {
		t.Fatalf("watch_video: true 应解析为指向 true 的指针: %+v", d.WatchVideo)
	}
	if d.ShareVideo == nil || *d.ShareVideo {
		t.Fatalf("share_video: false 应解析为指向 false 的指针: %+v", d.ShareVideo)
	}
	if d.DonateCoin == nil || !*d.DonateCoin {
		t.Fatalf("donate_coin: true 应解析为指向 true 的指针: %+v", d.DonateCoin)
	}
	u := cfg.Tasks.Unfollow
	if !u.Enabled || u.GroupName != "天选时刻" || u.Count != 20 || u.RetainUIDs != "1,2" {
		t.Fatalf("Unfollow 解析错误: %+v", u)
	}
	dt := cfg.Push.DingTalk
	if !dt.Enabled || dt.WebhookURL != "https://oapi.dingtalk.com/robot/send?access_token=abc" || dt.Secret != "SEC123" {
		t.Fatalf("DingTalk 解析错误: %+v", dt)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	// 空 YAML：结构体零值，不报错。
	path := writeConfig(t, "empty.yaml", "")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("空配置应解析成功: %v", err)
	}
	if cfg.Tasks.Daily.SelectLike || cfg.Log.Level != "" {
		t.Fatalf("零值解析错误: %+v", cfg)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-exist.yaml")
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("文件不存在应返回错误")
	}
	if !strings.Contains(err.Error(), "config file not found") {
		t.Fatalf("错误信息不符合约定: %v", err)
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	path := writeConfig(t, "bad.yaml", "log: [unclosed")
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("非法 YAML 应返回错误")
	}
}

func TestEnvOverrideLogLevel(t *testing.T) {
	t.Setenv("RAY_LOG_LEVEL", "error")
	path := writeConfig(t, "env.yaml", "log:\n  level: info\n")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	if cfg.Log.Level != "error" {
		t.Fatalf("RAY_LOG_LEVEL 未生效: %q", cfg.Log.Level)
	}
}

func TestEnvOverrideDingTalkWebhook(t *testing.T) {
	t.Setenv("RAY_DINGTALK_WEBHOOK", "https://oapi.dingtalk.com/robot/send?access_token=env")
	path := writeConfig(t, "env2.yaml", "push:\n  dingtalk:\n    enabled: false\n    webhook_url: \"\"\n")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	if !cfg.Push.DingTalk.Enabled {
		t.Fatal("设置 RAY_DINGTALK_WEBHOOK 后应自动启用钉钉推送")
	}
	if cfg.Push.DingTalk.WebhookURL != "https://oapi.dingtalk.com/robot/send?access_token=env" {
		t.Fatalf("webhook 地址未覆盖: %q", cfg.Push.DingTalk.WebhookURL)
	}
}

// TestDailySwitchDefaults 验证三个开关在零值（未配置）时默认开启。
func TestDailySwitchDefaults(t *testing.T) {
	var d DailyConfig // 零值：三个开关均为 nil
	if !d.WatchVideoEnabled() {
		t.Fatal("WatchVideo 未配置时应默认开启")
	}
	if !d.ShareVideoEnabled() {
		t.Fatal("ShareVideo 未配置时应默认开启")
	}
	if !d.DonateCoinEnabled() {
		t.Fatal("DonateCoin 未配置时应默认开启")
	}
}

// TestDailySwitchExplicit 验证显式 true/false 均生效。
func TestDailySwitchExplicit(t *testing.T) {
	boolPtr := func(b bool) *bool { return &b }

	// 显式 false → 全部关闭。
	closed := DailyConfig{
		WatchVideo: boolPtr(false),
		ShareVideo: boolPtr(false),
		DonateCoin: boolPtr(false),
	}
	if closed.WatchVideoEnabled() || closed.ShareVideoEnabled() || closed.DonateCoinEnabled() {
		t.Fatalf("显式 false 应关闭全部开关: %+v", closed)
	}

	// 显式 true → 全部开启。
	opened := DailyConfig{
		WatchVideo: boolPtr(true),
		ShareVideo: boolPtr(true),
		DonateCoin: boolPtr(true),
	}
	if !opened.WatchVideoEnabled() || !opened.ShareVideoEnabled() || !opened.DonateCoinEnabled() {
		t.Fatalf("显式 true 应开启全部开关: %+v", opened)
	}
}

func TestEnvOverridePushChannels(t *testing.T) {
	t.Setenv("RAY_SERVERCHAN_KEY", "SCT-KEY")
	t.Setenv("RAY_PUSHPLUS_TOKEN", "PP-TOKEN")
	t.Setenv("RAY_WORKWEIXIN_WEBHOOK", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x")
	t.Setenv("RAY_TELEGRAM_BOT_TOKEN", "BOT-TOKEN")
	t.Setenv("RAY_TELEGRAM_CHAT_ID", "-100123")
	t.Setenv("RAY_CUSTOM_API_URL", "https://example.com/notify")

	path := writeConfig(t, "env3.yaml", "push: {}\n")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}

	if !cfg.Push.ServerChan.Enabled || cfg.Push.ServerChan.SendKey != "SCT-KEY" {
		t.Fatalf("ServerChan 环境变量未生效: %+v", cfg.Push.ServerChan)
	}
	if !cfg.Push.PushPlus.Enabled || cfg.Push.PushPlus.Token != "PP-TOKEN" {
		t.Fatalf("PushPlus 环境变量未生效: %+v", cfg.Push.PushPlus)
	}
	if !cfg.Push.WorkWeixin.Enabled || cfg.Push.WorkWeixin.WebhookURL == "" {
		t.Fatalf("WorkWeixin 环境变量未生效: %+v", cfg.Push.WorkWeixin)
	}
	if !cfg.Push.Telegram.Enabled || cfg.Push.Telegram.BotToken != "BOT-TOKEN" || cfg.Push.Telegram.ChatID != "-100123" {
		t.Fatalf("Telegram 环境变量未生效: %+v", cfg.Push.Telegram)
	}
	if !cfg.Push.CustomAPI.Enabled || cfg.Push.CustomAPI.URL != "https://example.com/notify" {
		t.Fatalf("CustomAPI 环境变量未生效: %+v", cfg.Push.CustomAPI)
	}
}

func TestEnvOverrideChannelsStayDisabled(t *testing.T) {
	// 未设置任何 RAY_* 推送环境变量时，各渠道保持 disabled。
	for _, k := range []string{
		"RAY_SERVERCHAN_KEY", "RAY_PUSHPLUS_TOKEN", "RAY_WORKWEIXIN_WEBHOOK",
		"RAY_TELEGRAM_BOT_TOKEN", "RAY_TELEGRAM_CHAT_ID", "RAY_CUSTOM_API_URL",
	} {
		if v, ok := os.LookupEnv(k); ok {
			os.Unsetenv(k)
			defer os.Setenv(k, v)
		}
	}
	path := writeConfig(t, "env4.yaml", "push: {}\n")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig 失败: %v", err)
	}
	if cfg.Push.ServerChan.Enabled || cfg.Push.PushPlus.Enabled || cfg.Push.WorkWeixin.Enabled ||
		cfg.Push.Telegram.Enabled || cfg.Push.CustomAPI.Enabled {
		t.Fatalf("默认状态下新渠道应全部关闭: %+v", cfg.Push)
	}
}

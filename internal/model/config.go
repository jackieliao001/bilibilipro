package model

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 是 YAML 根配置。
type Config struct {
	Log       LogConfig       `yaml:"log"`
	Bilibili  BilibiliConfig  `yaml:"bilibili"`
	Security  SecurityConfig  `yaml:"security"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
	Cookies   []CookieConfig  `yaml:"cookies"`
	Tasks     TasksConfig     `yaml:"tasks"`
	Push      PushConfig      `yaml:"push"`
}

// SecurityConfig 安全与防风控配置。
type SecurityConfig struct {
	RandomSleepEnabled bool `yaml:"random_sleep_enabled"` // 开关：任务触发后随机沉默一段时间再执行（时长程序随机，最晚不超过当日 23:00:00 开始执行，不会跨天）
}

// SchedulerConfig 常驻调度配置。
type SchedulerConfig struct {
	Enabled bool   `yaml:"enabled"` // 是否常驻调度（等价于运行命令时加 -cron 参数；服务器常驻部署时必须设为 true）
	Cron    string `yaml:"cron"`    // cron 表达式，如 "0 30 8 * * *" = 每天 08:30:00
}

// LogConfig 日志配置。
type LogConfig struct {
	Level string `yaml:"level"` // debug|info|warn|error
	File  string `yaml:"file"`  // 空 = 仅控制台
}

// BilibiliConfig B 站客户端配置。
type BilibiliConfig struct {
	IntervalSeconds int    `yaml:"interval_seconds"`
	UserAgent       string `yaml:"user_agent"`
	Proxy           string `yaml:"proxy"`
}

// CookieConfig 配置文件中内嵌的 cookie 条目。
type CookieConfig struct {
	Name      string `yaml:"name"`
	CookieStr string `yaml:"cookie_str"`
}

// TasksConfig 任务配置。
type TasksConfig struct {
	Daily    DailyConfig    `yaml:"daily"`
	Unfollow UnfollowConfig `yaml:"unfollow"`
}

// DailyConfig 每日任务配置。
// WatchVideo / ShareVideo / DonateCoin 为 *bool：nil 表示未配置（默认开启），
// 显式 false 表示关闭，显式 true 表示开启。
type DailyConfig struct {
	Enabled          bool   `yaml:"enabled"`
	WatchVideo       *bool  `yaml:"watch_video"`  // nil=默认开启
	ShareVideo       *bool  `yaml:"share_video"`  // nil=默认开启
	DonateCoin       *bool  `yaml:"donate_coin"`  // nil=默认开启（投币总开关）
	DonateCoins      int    `yaml:"donate_coins"` // 投币枚数 [0,5]，开关开启且数量>0 才投
	ProtectCoins     int    `yaml:"protect_coins"`
	SaveCoinsWhenLv6 bool   `yaml:"save_coins_when_lv6"`
	DonateForArticle bool   `yaml:"donate_for_article"`
	SelectLike       bool   `yaml:"select_like"`
	SupportUpIDs     string `yaml:"support_up_ids"`
	DevicePlatform   string `yaml:"device_platform"`
}

// WatchVideoEnabled 观看视频开关是否开启：nil 或 true → 开启；显式 false → 关闭。
func (d DailyConfig) WatchVideoEnabled() bool {
	return d.WatchVideo == nil || *d.WatchVideo
}

// ShareVideoEnabled 分享视频开关是否开启：nil 或 true → 开启；显式 false → 关闭。
func (d DailyConfig) ShareVideoEnabled() bool {
	return d.ShareVideo == nil || *d.ShareVideo
}

// DonateCoinEnabled 投币总开关是否开启：nil 或 true → 开启；显式 false → 关闭。
func (d DailyConfig) DonateCoinEnabled() bool {
	return d.DonateCoin == nil || *d.DonateCoin
}

// UnfollowConfig 取关任务配置。
type UnfollowConfig struct {
	Enabled    bool   `yaml:"enabled"`
	GroupName  string `yaml:"group_name"`
	Count      int    `yaml:"count"`
	RetainUIDs string `yaml:"retain_uids"`
}

// PushConfig 推送配置。
type PushConfig struct {
	DingTalk   DingTalkConfig   `yaml:"dingtalk"`
	ServerChan ServerChanConfig `yaml:"serverchan"`
	PushPlus   PushPlusConfig   `yaml:"pushplus"`
	WorkWeixin WorkWeixinConfig `yaml:"workweixin"`
	Telegram   TelegramConfig   `yaml:"telegram"`
	CustomAPI  CustomAPIConfig  `yaml:"custom_api"`
}

// DingTalkConfig 钉钉机器人配置。
type DingTalkConfig struct {
	Enabled    bool   `yaml:"enabled"`
	WebhookURL string `yaml:"webhook_url"`
	Secret     string `yaml:"secret"`
}

// ServerChanConfig Server酱（Turbo 版）推送配置。
type ServerChanConfig struct {
	Enabled bool   `yaml:"enabled"`
	SendKey string `yaml:"send_key"` // Turbo 版 SendKey（原 turboScKey；旧版 scKey 已废弃）
}

// PushPlusConfig PushPlus 推送配置。
type PushPlusConfig struct {
	Enabled bool   `yaml:"enabled"`
	Token   string `yaml:"token"`   // 平台令牌（必填）
	Channel string `yaml:"channel"` // 可选：wechat|webhook|cp|sms|mail，空=wechat
	Topic   string `yaml:"topic"`   // 可选：群组编码，群发用（channel 为 webhook 时无效）
	Webhook string `yaml:"webhook"` // 可选：webhook 编码（非地址），channel=webhook/cp 时填
}

// WorkWeixinConfig 企业微信群机器人推送配置。
type WorkWeixinConfig struct {
	Enabled    bool   `yaml:"enabled"`
	WebhookURL string `yaml:"webhook_url"` // 群机器人 Webhook 完整地址（必填）
}

// TelegramConfig Telegram Bot 推送配置。
type TelegramConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"` // 机器人令牌（必填）
	ChatID   string `yaml:"chat_id"`   // 会话 ID（必填）
	Proxy    string `yaml:"proxy"`     // 可选：代理 user:password@host:port
	APIHost  string `yaml:"api_host"`  // 可选：默认 https://api.telegram.org，可换反代
}

// CustomAPIConfig 自定义 API 推送配置。
type CustomAPIConfig struct {
	Enabled          bool   `yaml:"enabled"`
	URL              string `yaml:"url"`                // 目标 API 地址（必填）
	Placeholder      string `yaml:"placeholder"`        // 可选：占位符，默认 "#msg#"
	BodyJSONTemplate string `yaml:"body_json_template"` // 可选：请求体 JSON 模板，默认 {"msgtype":"markdown","markdown":{"content":#msg#}}
}

// LoadConfig 读取并解析 YAML 配置文件。
// 文件不存在时返回错误（"config file not found"），由调用方决定处理方式；
// 文件存在但解析失败同样返回错误。
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s（可复制 config.example.yaml 为 %s 后修改）", path, path)
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", path, err)
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}

// applyEnvOverrides 用环境变量覆盖配置。
// 覆盖项：
//   - RAY_LOG_LEVEL：日志级别（debug/info/warn/error）
//   - RAY_DINGTALK_WEBHOOK：钉钉机器人 Webhook 地址（设置后自动启用钉钉推送）
//   - RAY_SERVERCHAN_KEY：Server酱 SendKey（设置后自动启用 Server酱推送）
//   - RAY_PUSHPLUS_TOKEN：PushPlus 令牌（设置后自动启用 PushPlus 推送）
//   - RAY_WORKWEIXIN_WEBHOOK：企业微信机器人 Webhook 地址（设置后自动启用企业微信推送）
//   - RAY_TELEGRAM_BOT_TOKEN / RAY_TELEGRAM_CHAT_ID：Telegram 机器人令牌 / 会话 ID（设置后自动启用 Telegram 推送）
//   - RAY_CUSTOM_API_URL：自定义 API 地址（设置后自动启用自定义 API 推送）
//   - RAY_COOKIE_FILE：cookies 文件路径（由 main 在确定 cookie 文件路径时读取）
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("RAY_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("RAY_DINGTALK_WEBHOOK"); v != "" {
		cfg.Push.DingTalk.WebhookURL = v
		cfg.Push.DingTalk.Enabled = true
	}
	if v := os.Getenv("RAY_SERVERCHAN_KEY"); v != "" {
		cfg.Push.ServerChan.SendKey = v
		cfg.Push.ServerChan.Enabled = true
	}
	if v := os.Getenv("RAY_PUSHPLUS_TOKEN"); v != "" {
		cfg.Push.PushPlus.Token = v
		cfg.Push.PushPlus.Enabled = true
	}
	if v := os.Getenv("RAY_WORKWEIXIN_WEBHOOK"); v != "" {
		cfg.Push.WorkWeixin.WebhookURL = v
		cfg.Push.WorkWeixin.Enabled = true
	}
	if v := os.Getenv("RAY_TELEGRAM_BOT_TOKEN"); v != "" {
		cfg.Push.Telegram.BotToken = v
		cfg.Push.Telegram.Enabled = true
	}
	if v := os.Getenv("RAY_TELEGRAM_CHAT_ID"); v != "" {
		cfg.Push.Telegram.ChatID = v
		cfg.Push.Telegram.Enabled = true
	}
	if v := os.Getenv("RAY_CUSTOM_API_URL"); v != "" {
		cfg.Push.CustomAPI.URL = v
		cfg.Push.CustomAPI.Enabled = true
	}
}

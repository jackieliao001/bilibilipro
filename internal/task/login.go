package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/raywangqvq/bilitoolgo/internal/api/bilibili"
	"github.com/raywangqvq/bilitoolgo/internal/model"
)

// LoginTask 扫码登录任务：生成二维码 → 轮询扫码结果 → 补全 buvid3 → 合并保存 Cookie。
type LoginTask struct {
	cfg        *model.Config
	client     *bilibili.BiliClient
	logger     *slog.Logger
	cookieFile string
}

// NewLoginTask 创建登录任务（登录不需要已有 Cookie）。
func NewLoginTask(cfg *model.Config, client *bilibili.BiliClient, logger *slog.Logger, cookieFile string) Task {
	return &LoginTask{cfg: cfg, client: client, logger: logger, cookieFile: cookieFile}
}

// Name 任务名。
func (t *LoginTask) Name() string { return "Login" }

// Run 执行扫码登录流程。
func (t *LoginTask) Run(ctx context.Context) (*model.TaskResult, error) {
	result := &model.TaskResult{TaskName: t.Name()}

	qr, err := t.client.GenerateQRCode(ctx)
	if err != nil {
		return result, fmt.Errorf("生成二维码失败: %w", err)
	}
	t.printQR(qr)

	ck, err := t.pollLogin(ctx, qr.QrcodeKey)
	if err != nil {
		return result, err
	}

	// 补全 buvid3：访问首页获取 Set-Cookie 后合并（失败仅告警，不影响登录结果）。
	if ck.Buvid3 == "" {
		setCookies, err := t.client.GetHomePage(ctx, ck)
		if err != nil {
			t.logger.Warn("访问首页补全 buvid3 失败", "err", err)
		} else {
			ck.MergeSetCookie(setCookies)
			t.logger.Info("已补全 buvid3", "buvid3", ck.Buvid3)
		}
	}

	// 合并到现有 cookies（按 DedeUserID 去重：已存在则替换，不存在则追加）并保存。
	path := t.cookieFile
	if path == "" {
		path = cookieFilePath()
	}
	all, err := model.ParseCookiesFromFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.logger.Warn("读取已有 Cookie 文件失败，将仅保存本次登录结果", "err", err)
		all = nil
	}
	all = mergeCookies(all, ck)
	if err := model.SaveCookiesToFile(all, path); err != nil {
		return result, fmt.Errorf("保存 Cookie 失败: %w", err)
	}

	t.logger.Info("登录成功", "uid", ck.DedeUserID)
	fmt.Println("Cookie 已保存到 " + path)

	result.Accounts = append(result.Accounts, model.AccountResult{
		UserID:  ck.DedeUserID,
		Success: true,
		Steps: []model.StepResult{{
			Name:    "扫码登录",
			Status:  "ok",
			Message: "登录成功，Cookie 已保存",
		}},
	})
	return result, nil
}

// pollLogin 轮询扫码结果：最多 10 次，每次间隔 5 秒（可被 ctx 取消）。
func (t *LoginTask) pollLogin(ctx context.Context, qrcodeKey string) (*model.Cookie, error) {
	for i := 0; i < 10; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
		poll, err := t.client.PollQRCode(ctx, qrcodeKey)
		if err != nil {
			t.logger.Warn("轮询二维码状态失败", "err", err)
			continue
		}
		switch poll.Code {
		case 0:
			if poll.CookieStr == "" {
				t.logger.Warn("扫码成功但响应中无 Cookie，继续等待")
				continue
			}
			ck, err := model.ParseCookie(poll.CookieStr)
			if err != nil {
				return nil, fmt.Errorf("解析 Cookie 失败: %w", err)
			}
			if err := ck.Validate(); err != nil {
				return nil, fmt.Errorf("Cookie 校验失败: %w", err)
			}
			return ck, nil
		case 86038:
			return nil, errors.New("二维码已失效，请重新运行 login 命令")
		case 86090:
			t.logger.Info("已扫码，请在手机上确认登录")
		default:
			t.logger.Info("等待扫码...", "message", poll.Message)
		}
	}
	return nil, errors.New("等待扫码超时（10 次轮询约 50 秒），请重新运行 login 命令")
}

// printQR 终端打印二维码信息（QR 码展示允许直接输出到终端）。
func (t *LoginTask) printQR(qr *bilibili.QRCodeData) {
	fmt.Println("======== 请使用手机 B 站 App 扫码登录 ========")
	fmt.Println("二维码内容链接：")
	fmt.Println("  " + qr.URL)
	fmt.Println("在线二维码生成（打开链接即可看到二维码，或用任意二维码工具扫描下方内容）：")
	fmt.Println("  https://tool.lu/qrcode/basic.html?text=" + url.QueryEscape(qr.URL))
	fmt.Println("===========================================")
}

// mergeCookies 按 DedeUserID 去重合并：已存在则替换，不存在则追加。
func mergeCookies(existing []*model.Cookie, added *model.Cookie) []*model.Cookie {
	out := make([]*model.Cookie, 0, len(existing)+1)
	found := false
	for _, c := range existing {
		if c == nil {
			continue
		}
		if c.DedeUserID == added.DedeUserID {
			out = append(out, added)
			found = true
		} else {
			out = append(out, c)
		}
	}
	if !found {
		out = append(out, added)
	}
	return out
}

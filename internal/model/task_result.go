package model

import (
	"fmt"
	"strings"
	"time"
)

// StepResult 单步执行结果。
//
// Status 取值：ok | skip | fail
type StepResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// AccountResult 单个账号的任务执行结果。
type AccountResult struct {
	UserID  string       `json:"user_id"`
	UName   string       `json:"uname,omitempty"`
	Success bool         `json:"success"`
	Steps   []StepResult `json:"steps"`
}

// TaskResult 整个任务（可含多账号）的执行结果。
type TaskResult struct {
	TaskName string          `json:"task_name"`
	Duration time.Duration   `json:"duration"`
	Accounts []AccountResult `json:"accounts"`
}

// Summary 生成人类可读的 Markdown 摘要（用于控制台报告与消息推送正文）。
// 包含：账号总数/成功/失败、任务耗时、每个账号的功能步骤结果
// （成功 ✅ / 跳过 ⏭️ 含原因 / 失败 ❌ 含原因）。
func (r *TaskResult) Summary() string {
	if r == nil {
		return ""
	}
	title := r.TaskName
	if title == "" {
		title = "未命名任务"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("## BiliTool 任务执行报告：%s\n", title))

	succ, fail := 0, 0
	for _, a := range r.Accounts {
		if a.Success {
			succ++
		} else {
			fail++
		}
	}
	b.WriteString(fmt.Sprintf("\n账号总数：%d，成功：%d，失败：%d\n", len(r.Accounts), succ, fail))
	if r.Duration > 0 {
		b.WriteString(fmt.Sprintf("任务耗时：%s\n", r.Duration.Round(time.Second)))
	}

	var reasons []string
	for _, a := range r.Accounts {
		display := a.UName
		if display == "" {
			display = a.UserID
		}
		if display == "" {
			display = "未知账号"
		}
		mark := "✅"
		if !a.Success {
			mark = "❌"
		}
		b.WriteString(fmt.Sprintf("\n### %s %s\n", mark, display))

		hasStep := false
		for _, s := range a.Steps {
			hasStep = true
			switch normalizeStatus(s.Status) {
			case "success":
				if s.Message != "" {
					b.WriteString(fmt.Sprintf("- ✅ %s：%s\n", s.Name, s.Message))
				} else {
					b.WriteString(fmt.Sprintf("- ✅ %s\n", s.Name))
				}
			case "skip":
				if s.Message != "" {
					b.WriteString(fmt.Sprintf("- ⏭️ %s：%s\n", s.Name, s.Message))
				} else {
					b.WriteString(fmt.Sprintf("- ⏭️ %s\n", s.Name))
				}
			default:
				msg := s.Message
				if msg == "" {
					msg = s.Status
				}
				b.WriteString(fmt.Sprintf("- ❌ %s：%s\n", s.Name, msg))
				if !a.Success {
					reasons = append(reasons, fmt.Sprintf("%s（账号 %s）", msg, display))
				}
			}
		}
		if !hasStep {
			b.WriteString("- （无步骤记录）\n")
		}
	}

	if fail > 0 {
		b.WriteString("\n### 失败原因汇总\n")
		if len(reasons) == 0 {
			reasons = append(reasons, "未知错误")
		}
		for _, rsn := range reasons {
			b.WriteString("- " + rsn + "\n")
		}
	}
	return b.String()
}

// normalizeStatus 将步骤状态归一化；空状态视为 skip，避免误报失败。
// 兼容约定取值 ok | skip | fail 及 success 等别名。
func normalizeStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ok", "success", "done", "passed":
		return "success"
	case "skip", "skipped", "not_required", "n/a", "":
		return "skip"
	default:
		return "failed"
	}
}

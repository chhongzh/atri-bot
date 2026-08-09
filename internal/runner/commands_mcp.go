package runner

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/telebot.v4"
)

func (r *Runner) registerMCPCommands() error {
	if err := r.commands.RegisterProvider("mcp", "MCP 管理（管理员）", true); err != nil {
		return err
	}
	return r.commands.Register("mcp", "查看或修改用户 MCP 设置", "mcp", "/mcp [show|limit|internal] <user-id> [value]", r.commandMCP)
}

func (r *Runner) commandMCP(c telebot.Context, args []string) {
	sender := c.Sender()
	if sender == nil {
		return
	}
	ctx := context.Background()
	if len(args) == 0 || args[0] == "show" {
		if err := requireArgs(args, 2, "/mcp show <user-id>"); err != nil {
			r.commandError(c, err)
			return
		}
		targetID, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			r.commandError(c, fmt.Errorf("无效用户 ID：%w", err))
			return
		}
		r.showMCPSettings(c, ctx, targetID)
		return
	}
	if err := requireArgs(args, 3, "/mcp [limit|internal] <user-id> <value>"); err != nil {
		r.commandError(c, err)
		return
	}
	targetID, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		r.commandError(c, fmt.Errorf("无效用户 ID：%w", err))
		return
	}
	switch args[0] {
	case "limit":
		value, parseErr := strconv.Atoi(args[2])
		if parseErr != nil || value < 0 {
			err = fmt.Errorf("limit 必须是非负整数（0 表示恢复默认）")
			break
		}
		err = r.accounts.SetMCPMaxTools(ctx, sender.ID, targetID, value)
	case "internal":
		var value *bool
		switch strings.ToLower(args[2]) {
		case "on", "enable":
			enabled := true
			value = &enabled
		case "off", "disable":
			enabled := false
			value = &enabled
		case "default":
			value = nil
		default:
			err = fmt.Errorf("internal 值必须是 on/off/default")
		}
		if err == nil {
			err = r.accounts.SetMCPBlockInternal(ctx, sender.ID, targetID, value)
		}
	default:
		err = fmt.Errorf("未知 MCP 操作 %q", args[0])
	}
	if err != nil {
		r.commandError(c, err)
		return
	}
	r.chats.Invalidate(targetID)
	_ = r.sendSystemResultAndDelete(c, fmt.Sprintf("已更新用户 %d 的 MCP 设置，将在其下一条消息生效。", targetID))
}

func (r *Runner) showMCPSettings(c telebot.Context, ctx context.Context, targetID int64) {
	user, err := r.accounts.Get(ctx, targetID)
	if err != nil {
		r.commandError(c, err)
		return
	}
	limitText := "默认"
	if user.MCPMaxTools > 0 {
		limitText = fmt.Sprintf("%d", user.MCPMaxTools)
	}
	internalText := "默认"
	if user.MCPBlockInternal != nil {
		if *user.MCPBlockInternal {
			internalText = "开启"
		} else {
			internalText = "关闭"
		}
	}
	_ = r.sendSystemResultAndDelete(c, fmt.Sprintf(
		"用户 %d 的 MCP 设置：\n- 工具数量上限：%s\n- 内网地址检查：%s",
		targetID, limitText, internalText,
	))
}

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
	return r.commands.Register("mcp", "查看或修改用户 MCP 设置", "mcp", "/mcp <show|limit|internal> <user-id> [value]", r.commandMCP)
}

func (r *Runner) commandMCP(c telebot.Context, args []string) {
	sender := c.Sender()
	action := commandAction(args, "")
	if action == "" {
		r.commandError(c, fmt.Errorf("用法：/mcp <show|limit|internal> <user-id> [value]"))
		return
	}
	switch action {
	case "show", "limit", "internal":
	default:
		r.commandError(c, fmt.Errorf("未知 MCP 操作 %q", action))
		return
	}
	targetID, err := parseUserID(args, 1, "/mcp "+action+" <user-id> [value]")
	if err != nil {
		r.commandError(c, err)
		return
	}
	ctx := context.Background()
	if action == "show" {
		r.showMCPSettings(c, ctx, targetID)
		return
	}
	if err = requireArgs(args, 3, "/mcp <limit|internal> <user-id> <value>"); err != nil {
		r.commandError(c, err)
		return
	}

	switch action {
	case "limit":
		limit, parseErr := strconv.Atoi(strings.TrimSpace(args[2]))
		if parseErr != nil || limit < 0 {
			err = fmt.Errorf("limit 必须是非负整数（0 表示恢复默认）")
		} else {
			err = r.accounts.SetMCPMaxTools(ctx, sender.ID, targetID, limit)
		}
	case "internal":
		value, parseErr := parseOptionalBool(args[2])
		if parseErr != nil {
			err = parseErr
		} else {
			err = r.accounts.SetMCPBlockInternal(ctx, sender.ID, targetID, value)
		}
	}
	if err != nil {
		r.commandError(c, err)
		return
	}
	r.chats.Invalidate(targetID)
	_ = r.sendSystemResultAndDelete(c, fmt.Sprintf("已更新用户 %d 的 MCP 设置，将在其下一条消息生效。", targetID))
}

func parseOptionalBool(value string) (*bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "enable":
		enabled := true
		return &enabled, nil
	case "off", "disable":
		enabled := false
		return &enabled, nil
	case "default":
		return nil, nil
	default:
		return nil, fmt.Errorf("internal 值必须是 on/off/default")
	}
}

func (r *Runner) showMCPSettings(c telebot.Context, ctx context.Context, targetID int64) {
	user, err := r.accounts.Get(ctx, targetID)
	if err != nil {
		r.commandError(c, err)
		return
	}
	limit := "默认"
	if user.MCPMaxTools > 0 {
		limit = strconv.Itoa(user.MCPMaxTools)
	}
	blockInternal := "默认"
	if user.MCPBlockInternal != nil {
		blockInternal = "关闭"
		if *user.MCPBlockInternal {
			blockInternal = "开启"
		}
	}
	_ = r.sendSystemResultAndDelete(c, fmt.Sprintf(
		"用户 %d 的 MCP 设置：\n- 工具数量上限：%s\n- 内网地址检查：%s",
		targetID, limit, blockInternal,
	))
}

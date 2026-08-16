// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package runner

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/utils"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

func (r *Runner) registerMCPCommands() error {
	if err := r.commands.RegisterProvider("mcp", "MCP 管理（管理员）", true); err != nil {
		return err
	}
	return r.commands.Register("mcp", "查看或修改用户 MCP 设置", "mcp", "/mcp <show|limit> <user-id> [value]", r.commandMCP)
}

func (r *Runner) commandMCP(c telebot.Context, args []string) {
	sender := c.Sender()
	action := commandAction(args, "")
	if action == "" {
		r.commandError(c, errs.CommandUsage("/mcp <show|limit> <user-id> [value]"))
		return
	}
	switch action {
	case "show", "limit":
	default:
		r.commandError(c, errs.UnknownMCPAction(action))
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
	if err = requireArgs(args, 3, "/mcp limit <user-id> <value>"); err != nil {
		r.commandError(c, err)
		return
	}

	switch action {
	case "limit":
		limit, parseErr := strconv.Atoi(strings.TrimSpace(args[2]))
		if parseErr != nil || limit < 0 {
			err = errs.ErrInvalidMCPLimit
		} else {
			err = r.accounts.SetMCPMaxTools(ctx, sender.ID, targetID, limit)
		}
	}
	if err != nil {
		r.commandError(c, err)
		return
	}
	r.logger.Info("user MCP settings updated",
		append(utils.ExpandTelebotContext(c),
			zap.String("mcp_action", action),
			zap.Int64("target_user_id", targetID),
		)...,
	)
	r.chats.Invalidate(targetID)
	_ = r.sendSystemResultAndDelete(c, fmt.Sprintf("已更新用户 %d 的 MCP 设置，将在其下一条消息生效。", targetID))
}

func (r *Runner) showMCPSettings(c telebot.Context, ctx context.Context, targetID int64) {
	settings, err := r.accounts.Settings(ctx, targetID)
	if err != nil {
		r.commandError(c, err)
		return
	}
	limit := "默认"
	if settings.MCPMaxTools > 0 {
		limit = strconv.Itoa(settings.MCPMaxTools)
	}
	_ = r.sendSystemResultAndDelete(c, fmt.Sprintf(
		"用户 %d 的 MCP 设置：\n- 工具数量上限：%s",
		targetID, limit,
	))
}

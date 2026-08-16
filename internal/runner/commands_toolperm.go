// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package runner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/utils"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

func (r *Runner) registerToolPermCommands() error {
	if err := r.commands.RegisterProvider("toolperm", "工具权限管理（管理员）", true); err != nil {
		return err
	}
	return r.commands.Register("toolperm", "查看或修改用户工具权限", "toolperm", "/toolperm <list|allow|deny|reset> <user-id> [tool-name]", r.commandToolPerm)
}

func (r *Runner) commandToolPerm(c telebot.Context, args []string) {
	action := commandAction(args, "")
	if action == "" {
		r.commandError(c, errs.CommandUsage("/toolperm <list|allow|deny|reset> <user-id> [tool-name]"))
		return
	}
	switch action {
	case "list", "allow", "deny", "reset":
	default:
		r.commandError(c, errs.UnknownToolPermAction(action))
		return
	}
	targetID, err := parseUserID(args, 1, "/toolperm "+action+" <user-id> [tool-name]")
	if err != nil {
		r.commandError(c, err)
		return
	}
	ctx := context.Background()
	if action == "list" {
		r.showToolPermissions(c, ctx, targetID)
		return
	}
	if err = requireArgs(args, 3, "/toolperm <allow|deny|reset> <user-id> <tool-name>"); err != nil {
		r.commandError(c, err)
		return
	}
	toolName := strings.TrimSpace(args[2])
	if toolName == "" {
		r.commandError(c, errs.ErrEmptyToolName)
		return
	}
	switch action {
	case "allow":
		err = r.chats.SetToolPermission(ctx, targetID, toolName, true)
	case "deny":
		err = r.chats.SetToolPermission(ctx, targetID, toolName, false)
	case "reset":
		err = r.chats.ResetToolPermission(ctx, targetID, toolName)
	}
	if err != nil {
		r.commandError(c, err)
		return
	}
	r.logger.Info("user tool permission updated",
		append(utils.ExpandTelebotContext(c),
			zap.String("toolperm_action", action),
			zap.String("tool_name", toolName),
			zap.Int64("target_user_id", targetID),
		)...,
	)
	r.chats.Invalidate(targetID)
	_ = r.sendSystemResultAndDelete(c, fmt.Sprintf("已更新用户 %d 的工具权限，将在其下一条消息生效。", targetID))
}

func (r *Runner) showToolPermissions(c telebot.Context, ctx context.Context, targetID int64) {
	names := r.tools.PermissionNames()
	sort.Strings(names)
	if err := r.sendListResult(c, func(builder *strings.Builder) error {
		fmt.Fprintf(builder, "用户 %d 的工具权限：\n", targetID)
		for _, name := range names {
			info, err := r.chats.ToolPermissionInfo(ctx, targetID, name)
			if err != nil {
				return err
			}
			state := "禁止"
			if info.Allowed {
				state = "允许"
			}
			defaultState := "禁止"
			if info.Default {
				defaultState = "允许"
			}
			marker := ""
			if info.Custom {
				marker = "（自定义）"
			}
			fmt.Fprintf(builder, "- %s：%s（默认 %s）%s\n", name, state, defaultState, marker)
		}
		return nil
	}); err != nil {
		r.commandError(c, err)
		return
	}
}

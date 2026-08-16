// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package runner

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/chhongzh/atri-bot/internal/utils"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

func (r *Runner) registerAdminCommands() error {
	if err := r.commands.RegisterProvider("admin", "用户与运行状态管理（管理员）", true); err != nil {
		return err
	}
	if err := r.commands.Register("admin", "分页列出所有管理员", "admins", "/admins [page]", r.commandAdmins); err != nil {
		return err
	}
	if err := r.commands.Register("admin", "分页列出用户账户", "users", "/users [all|banned] [page]", r.commandUsers); err != nil {
		return err
	}
	if err := r.commands.Register("admin", "分页列出活跃聊天状态", "active-users", "/active-users [page]", r.commandActiveUsers); err != nil {
		return err
	}
	if err := r.commands.Register("admin", "查看用户详情", "user", "/user <user-id>", r.commandUser); err != nil {
		return err
	}
	return r.commands.Register(
		"admin",
		"查看统计或管理账户权限",
		"admin",
		"/admin [stats|promote|demote|ban|unban|delete] [user-id]",
		r.commandAdmin,
	)
}

func (r *Runner) commandAdmin(c telebot.Context, args []string) {
	sender := c.Sender()
	action := commandAction(args, "stats")
	switch action {
	case "stats":
		r.showAdminStats(c, context.Background())
		return
	case "promote", "demote", "ban", "unban", "delete":
	default:
		r.commandError(c, errs.UnknownAdminAction(action))
		return
	}

	targetID, err := parseUserID(args, 1, "/admin <promote|demote|ban|unban|delete> <user-id>")
	if err != nil {
		r.commandError(c, err)
		return
	}
	ctx := context.Background()
	if err = r.changeAccount(ctx, sender.ID, targetID, action); err != nil {
		r.commandError(c, err)
		return
	}
	r.logger.Info("admin account operation completed",
		append(utils.ExpandTelebotContext(c),
			zap.String("admin_action", action),
			zap.Int64("target_user_id", targetID),
		)...,
	)

	r.chats.Invalidate(targetID)
	_ = r.sendSystemResultAndDelete(c, fmt.Sprintf("已对用户 %d 执行 %s。", targetID, action))
	r.sendAdminMessage(ctx, c.Bot(), adminMessage{
		Title:    "操作通知",
		Category: "账户管理",
		Fields: []adminMessageField{
			{Label: "操作管理员 ID", Value: strconv.FormatInt(sender.ID, 10)},
			{Label: "操作", Value: action},
			{Label: "目标用户 ID", Value: strconv.FormatInt(targetID, 10)},
		},
	})
}

func (r *Runner) changeAccount(ctx context.Context, actorID, targetID int64, action string) error {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "promote":
		return r.accounts.SetRole(ctx, actorID, targetID, model.RoleAdmin)
	case "demote":
		return r.accounts.SetRole(ctx, actorID, targetID, model.RoleUser)
	case "ban":
		return r.accounts.SetBanned(ctx, actorID, targetID, true)
	case "unban":
		return r.accounts.SetBanned(ctx, actorID, targetID, false)
	case "delete":
		return r.accounts.Delete(ctx, actorID, targetID)
	default:
		return errs.UnknownAdminAction(action)
	}
}

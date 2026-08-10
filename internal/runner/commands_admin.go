package runner

import (
	"context"
	"fmt"
	"strconv"

	"github.com/chhongzh/atri-bot/internal/account"
	"gopkg.in/telebot.v4"
)

func (r *Runner) registerAdminCommands() error {
	if err := r.commands.RegisterProvider("admin", "账户管理（管理员）", true); err != nil {
		return err
	}
	return r.commands.Register("admin", "统计、提权、降级、封禁与删除账户", "admin", "/admin [stats|promote|demote|ban|unban|delete] [user-id]", r.commandAdmin)
}

func (r *Runner) commandAdmin(c telebot.Context, args []string) {
	sender := c.Sender()
	if sender == nil {
		return
	}
	ctx := context.Background()
	if len(args) == 0 || args[0] == "stats" {
		stats, err := r.accounts.Stats(ctx)
		if err != nil {
			r.commandError(c, err)
			return
		}
		_ = r.sendSystemResultAndDelete(c, fmt.Sprintf("用户：%d\n管理员：%d\n封禁：%d", stats.Users, stats.Admins, stats.Banned))
		return
	}
	if err := requireArgs(args, 2, "/admin [promote|demote|ban|unban|delete] <user-id>"); err != nil {
		r.commandError(c, err)
		return
	}
	targetID, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		r.commandError(c, fmt.Errorf("无效用户 ID：%w", err))
		return
	}
	switch args[0] {
	case "promote":
		err = r.accounts.SetRole(ctx, sender.ID, targetID, account.RoleAdmin)
	case "demote":
		err = r.accounts.SetRole(ctx, sender.ID, targetID, account.RoleUser)
	case "ban":
		err = r.accounts.SetBanned(ctx, sender.ID, targetID, true)
	case "unban":
		err = r.accounts.SetBanned(ctx, sender.ID, targetID, false)
	case "delete":
		err = r.accounts.Delete(ctx, sender.ID, targetID)
	default:
		err = fmt.Errorf("未知管理员操作 %q", args[0])
	}
	if err != nil {
		r.commandError(c, err)
		return
	}
	r.chats.Invalidate(targetID)
	message := fmt.Sprintf("管理员 %d 执行了 %s，目标用户 %d。", sender.ID, args[0], targetID)
	_ = r.sendSystemResultAndDelete(c, message)
	r.sendAdminMessage(ctx, c.Bot(), adminMessage{
		Title:    "操作通知",
		Category: "账户管理",
		Fields: []adminMessageField{
			{Label: "操作管理员 ID", Value: strconv.FormatInt(sender.ID, 10)},
			{Label: "操作", Value: args[0]},
			{Label: "目标用户 ID", Value: strconv.FormatInt(targetID, 10)},
		},
	})
}

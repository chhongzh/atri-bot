package runner

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/telebot.v4"
)

func (r *Runner) registerAICommands() error {
	if err := r.commands.RegisterProvider("ai", "AI 配置", false); err != nil {
		return err
	}
	return r.commands.Register("ai", "查看或修改模型连接配置", "ai", "/ai [show|base-url|key|model|rounds] [value]", r.commandAI)
}

func (r *Runner) commandAI(c telebot.Context, args []string) {
	sender := c.Sender()
	if sender == nil {
		return
	}
	ctx := context.Background()
	if len(args) == 0 || args[0] == "show" {
		user, err := r.accounts.Get(ctx, sender.ID)
		if err != nil {
			r.commandError(c, err)
			return
		}
		_ = r.sendSystemResultAndDelete(c, fmt.Sprintf(
			"Base URL: %s\nModel: %s\nAPI Key: %s\nMax Rounds: %d",
			showAIValue(user.AIBaseURL),
			showAIValue(user.AIModel),
			maskSecret(user.AIAPIKey),
			user.AIMaxRounds,
		))
		return
	}
	if err := requireArgs(args, 2, "/ai [base-url|key|model|rounds] <value>"); err != nil {
		r.commandError(c, err)
		return
	}
	value := strings.Join(args[1:], " ")
	var err error
	switch args[0] {
	case "base-url":
		err = r.accounts.SetAIBaseURL(ctx, sender.ID, value)
	case "key":
		err = r.accounts.SetAIAPIKey(ctx, sender.ID, value)
	case "model":
		err = r.accounts.SetAIModel(ctx, sender.ID, value)
	case "rounds":
		rounds, parseErr := strconv.Atoi(value)
		if parseErr != nil || rounds <= 0 {
			err = fmt.Errorf("rounds 必须是正整数")
			break
		}
		err = r.accounts.SetAIMaxRounds(ctx, sender.ID, rounds)
	default:
		err = fmt.Errorf("未知 AI 配置项 %q", args[0])
	}
	if err != nil {
		r.commandError(c, err)
		return
	}
	r.chats.Invalidate(sender.ID)
	_ = r.sendSystemResultAndDelete(c, "AI 配置已更新，将在下一条消息生效。")
}

func maskSecret(secret string) string {
	if secret == "" {
		return "<未设置>"
	}
	if len(secret) <= 8 {
		return "********"
	}
	return secret[:4] + "…" + secret[len(secret)-4:]
}

func showAIValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<未设置>"
	}
	return value
}

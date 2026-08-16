// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package runner

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/chhongzh/atri-bot/internal/constants"
	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/utils"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

func (r *Runner) registerAICommands() error {
	if err := r.commands.RegisterProvider("ai", "AI 配置", false); err != nil {
		return err
	}
	return r.commands.Register("ai", "查看或修改模型连接配置", "ai", "/ai [show|base-url|key|model|rounds|image-size] [value]", r.commandAI)
}

func (r *Runner) commandAI(c telebot.Context, args []string) {
	sender := c.Sender()
	ctx := context.Background()
	action := commandAction(args, "show")
	switch action {
	case "show":
		settings, err := r.accounts.Settings(ctx, sender.ID)
		if err != nil {
			r.commandError(c, err)
			return
		}
		_ = r.sendSystemResultAndDelete(c, fmt.Sprintf(
			"Base URL: %s\nModel: %s\nAPI Key: %s\nMax Rounds: %d\nImage Max Edge: %d",
			showAIValue(settings.AIBaseURL),
			showAIValue(settings.AIModel),
			maskSecret(settings.AIAPIKey),
			settings.AIMaxRounds,
			settings.AIImageMaxEdge,
		))
		return
	case "base-url", "key", "model", "rounds", "image-size":
	default:
		r.commandError(c, errs.UnknownAIItem(action))
		return
	}

	value, err := requiredValue(args, 1, "/ai [base-url|key|model|rounds|image-size] <value>")
	if err != nil {
		r.commandError(c, err)
		return
	}
	switch action {
	case "base-url":
		err = r.accounts.SetAIBaseURL(ctx, sender.ID, value)
	case "key":
		err = r.accounts.SetAIAPIKey(ctx, sender.ID, value)
	case "model":
		err = r.accounts.SetAIModel(ctx, sender.ID, value)
	case "rounds":
		rounds, parseErr := strconv.Atoi(value)
		if parseErr != nil || rounds <= 0 {
			err = errs.ErrInvalidRounds
		} else {
			err = r.accounts.SetAIMaxRounds(ctx, sender.ID, rounds)
		}
	case "image-size":
		maxEdge, parseErr := strconv.Atoi(value)
		if parseErr != nil || maxEdge <= 0 || maxEdge > constants.MaxImageMaxEdge {
			err = errs.InvalidImageSize(constants.MaxImageMaxEdge)
		} else {
			err = r.accounts.SetAIImageMaxEdge(ctx, sender.ID, maxEdge)
		}
	}
	if err != nil {
		r.commandError(c, err)
		return
	}
	r.logger.Info("user AI configuration updated",
		append(utils.ExpandTelebotContext(c), zap.String("config_item", action))...,
	)
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

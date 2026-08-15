// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package runner

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/chhongzh/atri-bot/internal/utils"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

//go:embed admin_message.j2
var adminMessageTemplate string

var adminMessagePrompt = prompt.FromMessages(schema.Jinja2, schema.SystemMessage(adminMessageTemplate))

type adminMessage struct {
	Title       string
	Category    string
	Fields      []adminMessageField
	DetailLabel string
	Detail      string
}

type adminMessageField struct {
	Label string
	Value string
}

func (r *Runner) sendAdminMessage(ctx context.Context, bot telebot.API, message adminMessage) {
	r.logger.Debug("sending administrator notification", zap.String("notification_category", message.Category))
	admins, err := r.accounts.Admins(ctx)
	if err != nil {
		r.logger.Error("failed to list administrators for notification", zap.Error(err))
		return
	}

	content, err := renderAdminMessage(ctx, message)
	if err != nil {
		r.logger.Error("failed to render administrator notification", zap.String("notification_category", message.Category), zap.Error(err))
		return
	}
	for _, admin := range admins {
		if _, err := bot.Send(&telebot.User{ID: admin.TelegramID}, content, telebot.ModeMarkdownV2); err != nil {
			r.logger.Warn("failed to send administrator notification",
				zap.Int64("admin_id", admin.TelegramID),
				zap.String("notification_category", message.Category),
				zap.Error(err),
			)
		}
	}
}

func renderAdminMessage(ctx context.Context, message adminMessage) (string, error) {
	fields := make([]adminMessageField, len(message.Fields))
	for index, field := range message.Fields {
		fields[index] = adminMessageField{
			Label: utils.EscapeMarkdownV2Text(field.Label),
			Value: utils.EscapeMarkdownV2Code(field.Value),
		}
	}
	values := map[string]any{
		"Message": adminMessage{
			Title:       utils.EscapeMarkdownV2Text(message.Title),
			Category:    utils.EscapeMarkdownV2Text(message.Category),
			Fields:      fields,
			DetailLabel: utils.EscapeMarkdownV2Text(message.DetailLabel),
			Detail:      utils.EscapeMarkdownV2Code(message.Detail),
		},
	}
	messages, err := adminMessagePrompt.Format(ctx, values)
	if err != nil {
		return "", err
	}
	if len(messages) != 1 {
		return "", fmt.Errorf("administrator notification template returned %d messages", len(messages))
	}
	return strings.TrimSpace(messages[0].Content), nil
}

// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package utils

import (
	"unicode/utf8"

	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

// telebotTextPreviewLimit 日志中文本预览的最大字符数。
const telebotTextPreviewLimit = 128

// ExpandUserFields 生成当前发送者对应的 zap 字段。
func ExpandUserFields(c telebot.Context) []zap.Field {
	if c == nil || c.Sender() == nil {
		return nil
	}
	return []zap.Field{
		zap.Int64("user_id", c.Sender().ID),
		zap.String("username", c.Sender().Username),
	}
}

// ExpandTelebotContext 展开 Telegram 上下文中的关键键值对为 zap 字段，
// 供 runner 等顶层调用方统一注入日志，避免各处手工拼接字段。
func ExpandTelebotContext(c telebot.Context) []zap.Field {
	if c == nil {
		return nil
	}
	var fields []zap.Field
	fields = append(fields, ExpandUserFields(c)...)
	if chat := c.Chat(); chat != nil {
		fields = append(fields,
			zap.Int64("chat_id", chat.ID),
			zap.String("chat_type", string(chat.Type)),
		)
	}
	if message := c.Message(); message != nil {
		fields = append(fields, zap.Int("message_id", message.ID))
	}
	return fields
}

func telebotTextPreview(text string) string {
	if utf8.RuneCountInString(text) <= telebotTextPreviewLimit {
		return text
	}
	return string([]rune(text)[:telebotTextPreviewLimit]) + "…"
}

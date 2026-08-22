// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package utils

import (
	"strings"
	"unicode/utf8"

	"gopkg.in/telebot.v4"
)

const telegramTextLimit = 4096

func SendTelegramText(c telebot.Context, text string, opts ...interface{}) error {
	for _, part := range SplitTelegramText(text, telegramTextLimit) {
		if err := c.Send(part, opts...); err != nil {
			return err
		}
	}
	return nil
}

func SplitTelegramText(text string, limit int) []string {
	if text == "" {
		return nil
	}
	return splitTelegramTextBlock(text, limit)
}

func splitTelegramTextBlock(text string, limit int) []string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	runes := []rune(text)
	parts := make([]string, 0, len(runes)/limit+1)
	for len(runes) > 0 {
		end := min(limit, len(runes))
		if end < len(runes) {
			candidate := string(runes[:end])
			if newline := strings.LastIndex(candidate, "\n"); newline >= 0 {
				newlineEnd := utf8.RuneCountInString(candidate[:newline+1])
				if newlineEnd > limit/2 {
					end = newlineEnd
				}
			}
		}
		parts = append(parts, string(runes[:end]))
		runes = runes[end:]
	}
	return parts
}

func FormatTelegramUsername(user *telebot.User) string {
	return strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
}

func EscapeMarkdownV2Code(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(value, "`", "\\`")
}

func EscapeMarkdownV2Text(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(value)
}

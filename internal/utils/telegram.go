package utils

import (
	"strings"
	"unicode/utf8"

	"gopkg.in/telebot.v4"
)

const telegramTextLimit = 4096

func TelegramSendText(c telebot.Context, text string) error {
	for _, part := range TelegramSplitText(text, telegramTextLimit) {
		if err := c.Send(part); err != nil {
			return err
		}
	}
	return nil
}

func TelegramSplitText(text string, limit int) []string {
	blocks := strings.Split(text, "\n\n")
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block == "" {
			continue
		}
		parts = append(parts, telegramSplitTextBlock(block, limit)...)
	}
	return parts
}

func telegramSplitTextBlock(text string, limit int) []string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return []string{text}
	}
	runes := []rune(text)
	parts := make([]string, 0, len(runes)/limit+1)
	for len(runes) > 0 {
		end := min(limit, len(runes))
		if end < len(runes) {
			if newline := strings.LastIndex(string(runes[:end]), "\n"); newline > limit/2 {
				end = utf8.RuneCountInString(string(runes[:newline+1]))
			}
		}
		parts = append(parts, string(runes[:end]))
		runes = runes[end:]
	}
	return parts
}

func TelegramFormatUsername(user *telebot.User) string {
	return strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
}

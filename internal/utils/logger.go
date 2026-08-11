package utils

import (
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

func ExpandUserFields(c telebot.Context) []zap.Field {
	return []zap.Field{
		zap.Int64("user_id", c.Sender().ID),
		zap.String("username", c.Sender().Username),
	}
}

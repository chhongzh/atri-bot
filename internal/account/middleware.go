package account

import (
	"context"
	"strings"

	"gopkg.in/telebot.v4"
)

func (m *Manager) UserMiddleware(next telebot.HandlerFunc) telebot.HandlerFunc {
	return func(c telebot.Context) error {
		sender := c.Sender()
		user, _, err := m.EnsureUser(context.Background(), sender.ID, sender.Username, displayName(sender))
		if err != nil {
			return err
		}
		if user.Banned {
			return c.Send("你的账户已被封禁。")
		}
		return next(c)
	}
}

func (m *Manager) AdminOnly(next telebot.HandlerFunc) telebot.HandlerFunc {
	return func(c telebot.Context) error {
		sender := c.Sender()
		ok, err := m.IsAdmin(context.Background(), sender.ID)
		if err != nil {
			return err
		}
		if !ok {
			return c.Send("这个操作需要管理员权限。")
		}
		return next(c)
	}
}

func displayName(user *telebot.User) string {
	return strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
}

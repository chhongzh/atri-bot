// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package account

import (
	"context"

	"github.com/chhongzh/atri-bot/internal/utils"
	"gopkg.in/telebot.v4"
)

func (m *Manager) UserMiddleware(next telebot.HandlerFunc) telebot.HandlerFunc {
	return func(c telebot.Context) error {
		sender := c.Sender()
		user, _, err := m.EnsureUser(context.Background(), sender.ID, sender.Username, utils.FormatTelegramUsername(sender))
		if err != nil {
			return err
		}
		if user.Banned {
			return c.Send("你的账户已被封禁。")
		}
		return next(c)
	}
}

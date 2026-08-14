// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/chhongzh/atri-bot/internal/utils"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

func (r *Runner) registerCharacterCommands() error {
	if err := r.commands.RegisterProvider("character", "角色与会话", false); err != nil {
		return err
	}
	if err := r.commands.Register("character", "列出所有角色", "characters", "/characters", r.commandCharacters); err != nil {
		return err
	}
	if err := r.commands.Register("character", "查看或切换角色", "character", "/character [character-id]", r.commandCharacter); err != nil {
		return err
	}
	return nil
}

func (r *Runner) commandCharacters(c telebot.Context, _ []string) {
	characters := r.characters.List()
	if len(characters) == 0 {
		_ = r.sendSystemResultAndDelete(c, "当前没有可用角色。")
		return
	}
	_ = r.sendListResult(c, func(builder *strings.Builder) error {
		builder.WriteString("可用角色：\n")
		for _, character := range characters {
			fmt.Fprintf(builder, "- %s — %s（%s）\n", character.ID, character.Name(), character.ProviderID)
		}
		return nil
	})
}

func (r *Runner) commandCharacter(c telebot.Context, args []string) {
	sender := c.Sender()
	ctx := context.Background()
	if len(args) == 0 {
		settings, err := r.accounts.Settings(ctx, sender.ID)
		if err != nil {
			r.commandError(c, err)
			return
		}
		if settings.CharacterID == "" {
			_ = r.sendSystemResultAndDelete(c, "尚未选择角色，发送消息时会自动选择默认角色。")
			return
		}
		_ = r.sendSystemResultAndDelete(c, "当前角色："+settings.CharacterID)
		return
	}

	characterID := strings.TrimSpace(args[0])
	if _, ok := r.characters.Get(characterID); !ok {
		r.commandError(c, fmt.Errorf("角色 %q 不存在", characterID))
		return
	}
	if err := r.accounts.SetCharacter(ctx, sender.ID, characterID); err != nil {
		r.commandError(c, err)
		return
	}
	r.logger.Info("user switched character",
		append(utils.ExpandTelebotContext(c), zap.String("character_id", characterID))...,
	)
	r.chats.Invalidate(sender.ID)
	_ = r.sendSystemResultAndDelete(c, "已切换角色为 "+characterID+"。")
}

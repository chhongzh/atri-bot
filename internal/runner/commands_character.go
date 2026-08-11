package runner

import (
	"context"
	"fmt"
	"strings"

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
	return r.commands.Register("character", "清空当前角色会话", "session-clear", "/session-clear", r.commandSessionClear)
}

func (r *Runner) commandCharacters(c telebot.Context, _ []string) {
	characters := r.characters.List()
	if len(characters) == 0 {
		_ = r.sendSystemResultAndDelete(c, "当前没有可用角色。")
		return
	}
	var builder strings.Builder
	builder.WriteString("可用角色：\n")
	for _, character := range characters {
		fmt.Fprintf(&builder, "- %s — %s（%s）\n", character.ID, character.Name(), character.ProviderID)
	}
	_ = r.sendSystemResultAndDelete(c, strings.TrimSpace(builder.String()))
}

func (r *Runner) commandCharacter(c telebot.Context, args []string) {
	sender := c.Sender()
	if sender == nil {
		return
	}
	ctx := context.Background()
	if len(args) == 0 {
		user, err := r.accounts.Get(ctx, sender.ID)
		if err != nil {
			r.commandError(c, err)
			return
		}
		if user.CharacterID == "" {
			_ = r.sendSystemResultAndDelete(c, "尚未选择角色，发送消息时会自动选择默认角色。")
			return
		}
		_ = r.sendSystemResultAndDelete(c, "当前角色："+user.CharacterID)
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
	r.chats.Invalidate(sender.ID)
	_ = r.sendSystemResultAndDelete(c, "已切换角色为 "+characterID+"。")
}

func (r *Runner) commandSessionClear(c telebot.Context, _ []string) {
	sender := c.Sender()
	if sender == nil {
		return
	}
	ctx := context.Background()
	user, err := r.accounts.Get(ctx, sender.ID)
	if err != nil {
		r.commandError(c, err)
		return
	}
	if user.CharacterID == "" {
		r.commandError(c, fmt.Errorf("尚未选择角色"))
		return
	}
	if err = r.sessions.Clear(ctx, sender.ID, user.CharacterID); err != nil {
		r.commandError(c, err)
		return
	}
	r.chats.Invalidate(sender.ID)
	_ = r.sendSystemResultAndDelete(c, "当前角色的会话已清空。")
}

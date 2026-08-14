package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chhongzh/atri-bot/internal/utils"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

func (r *Runner) registerProviderCommands() error {
	if err := r.commands.RegisterProvider("provider", "角色 Provider 管理（管理员）", true); err != nil {
		return err
	}
	if err := r.commands.Register("provider", "列出所有角色 Provider", "providers", "/providers", r.commandProviders); err != nil {
		return err
	}
	return r.commands.Register("provider", "新增、修改、删除或刷新角色来源", "provider", "/provider <add|set|remove|refresh> ...", r.commandProvider)
}

func (r *Runner) commandProviders(c telebot.Context, _ []string) {
	r.listCharacterProviders(c, context.Background())
}

func (r *Runner) commandProvider(c telebot.Context, args []string) {
	action := commandAction(args, "")
	if action == "" {
		r.commandError(c, fmt.Errorf("用法：/provider <add|set|remove|refresh>"))
		return
	}
	if action == "list" {
		r.listCharacterProviders(c, context.Background())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var err error
	switch action {
	case "add":
		if err = requireArgs(args, 3, "/provider add <id> <url> [branch]"); err == nil {
			err = r.characters.AddRemote(ctx, strings.TrimSpace(args[1]), strings.TrimSpace(args[2]), optionalArg(args, 3))
		}
	case "set":
		if err = requireArgs(args, 3, "/provider set <id> <url> [branch]"); err == nil {
			err = r.characters.UpdateRemote(ctx, strings.TrimSpace(args[1]), strings.TrimSpace(args[2]), optionalArg(args, 3))
		}
	case "remove":
		if err = requireArgs(args, 2, "/provider remove <id>"); err == nil {
			err = r.characters.Remove(ctx, strings.TrimSpace(args[1]))
		}
	case "refresh":
		err = r.characters.Reload(ctx)
	default:
		err = fmt.Errorf("未知 Provider 操作 %q", action)
	}
	if err != nil {
		r.commandError(c, err)
		return
	}
	providerID := ""
	if len(args) > 1 {
		providerID = strings.TrimSpace(args[1])
	}
	r.logger.Info("character provider updated",
		append(utils.ExpandTelebotContext(c),
			zap.String("provider_action", action),
			zap.String("provider_id", providerID),
		)...,
	)
	r.chats.InvalidateAll()
	_ = r.sendSystemResultAndDelete(c, "角色 Provider 已更新。")
}

func (r *Runner) listCharacterProviders(c telebot.Context, ctx context.Context) {
	records, err := r.characters.Providers(ctx)
	if err != nil {
		r.commandError(c, err)
		return
	}
	if len(records) == 0 {
		_ = r.sendSystemResultAndDelete(c, "当前没有可用角色 Provider。")
		return
	}
	_ = r.sendListResult(c, func(builder *strings.Builder) error {
		builder.WriteString("角色 Providers：\n")
		for _, record := range records {
			location := record.URL
			if location == "" {
				location = record.Path
			}
			if record.Branch == "" {
				fmt.Fprintf(builder, "- %s — %s（%s）\n", record.ID, location, record.Kind)
				continue
			}
			fmt.Fprintf(builder, "- %s — %s（%s，%s）\n", record.ID, location, record.Kind, record.Branch)
		}
		return nil
	})
}

package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gopkg.in/telebot.v4"
)

func (r *Runner) registerProviderCommands() error {
	if err := r.commands.RegisterProvider("provider", "角色 Provider 管理（管理员）", true); err != nil {
		return err
	}
	return r.commands.Register("provider", "管理角色来源", "provider", "/provider [list|add|set|remove|refresh]", r.commandProvider)
}

func (r *Runner) commandProvider(c telebot.Context, args []string) {
	if len(args) == 0 || args[0] == "list" {
		records, err := r.characters.Providers(context.Background())
		if err != nil {
			r.commandError(c, err)
			return
		}
		var builder strings.Builder
		builder.WriteString("角色 Providers：\n")
		for _, record := range records {
			fmt.Fprintf(&builder, "- %s (%s) %s#%s\n", record.ID, record.Kind, record.URL, record.Branch)
		}
		_ = r.sendSystemResultAndDelete(c, strings.TrimSpace(builder.String()))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var err error
	switch args[0] {
	case "add":
		if err = requireArgs(args, 3, "/provider add <id> <url> [branch]"); err == nil {
			branch := ""
			if len(args) > 3 {
				branch = args[3]
			}
			err = r.characters.AddRemote(ctx, args[1], args[2], branch)
		}
	case "set":
		if err = requireArgs(args, 3, "/provider set <id> <url> [branch]"); err == nil {
			branch := ""
			if len(args) > 3 {
				branch = args[3]
			}
			err = r.characters.UpdateRemote(ctx, args[1], args[2], branch)
		}
	case "remove":
		if err = requireArgs(args, 2, "/provider remove <id>"); err == nil {
			err = r.characters.Remove(ctx, args[1])
		}
	case "refresh":
		err = r.characters.Reload(ctx)
	default:
		err = fmt.Errorf("未知 provider 操作 %q", args[0])
	}
	if err != nil {
		r.commandError(c, err)
		return
	}
	r.chats.InvalidateAll()
	_ = r.sendSystemResultAndDelete(c, "角色 Provider 已更新。")
}

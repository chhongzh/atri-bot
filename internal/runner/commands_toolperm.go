package runner

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/telebot.v4"
)

func (r *Runner) registerToolPermCommands() error {
	if err := r.commands.RegisterProvider("toolperm", "工具权限管理（管理员）", true); err != nil {
		return err
	}
	return r.commands.Register("toolperm", "查看或修改用户工具权限", "toolperm", "/toolperm [list|allow|deny|reset] <user-id> [tool-name]", r.commandToolPerm)
}

func (r *Runner) commandToolPerm(c telebot.Context, args []string) {
	sender := c.Sender()
	if sender == nil {
		return
	}
	ctx := context.Background()
	if len(args) == 0 || args[0] == "list" {
		if err := requireArgs(args, 2, "/toolperm list <user-id>"); err != nil {
			r.commandError(c, err)
			return
		}
		targetID, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			r.commandError(c, fmt.Errorf("无效用户 ID：%w", err))
			return
		}
		r.showToolPermissions(c, ctx, targetID)
		return
	}
	if err := requireArgs(args, 3, "/toolperm [allow|deny|reset] <user-id> <tool-name>"); err != nil {
		r.commandError(c, err)
		return
	}
	targetID, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		r.commandError(c, fmt.Errorf("无效用户 ID：%w", err))
		return
	}
	toolName := args[2]
	switch args[0] {
	case "allow":
		err = r.chats.SetToolPermission(ctx, targetID, toolName, true)
	case "deny":
		err = r.chats.SetToolPermission(ctx, targetID, toolName, false)
	case "reset":
		err = r.chats.ResetToolPermission(ctx, targetID, toolName)
	default:
		err = fmt.Errorf("未知工具权限操作 %q", args[0])
	}
	if err != nil {
		r.commandError(c, err)
		return
	}
	r.chats.Invalidate(targetID)
	_ = r.sendSystemResultAndDelete(c, fmt.Sprintf("已更新用户 %d 的工具权限，将在其下一条消息生效。", targetID))
}

func (r *Runner) showToolPermissions(c telebot.Context, ctx context.Context, targetID int64) {
	names := r.tools.AllNames()
	sort.Strings(names)
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("用户 %d 的工具权限：\n", targetID))
	for _, name := range names {
		info, err := r.chats.ToolPermissionInfo(ctx, targetID, name)
		if err != nil {
			r.commandError(c, err)
			return
		}
		state := "禁止"
		if info.Allowed {
			state = "允许"
		}
		defaultState := "允许"
		if !info.Default {
			defaultState = "禁止"
		}
		marker := ""
		if info.Custom {
			marker = "（自定义）"
		}
		fmt.Fprintf(&builder, "- %s：%s（默认 %s）%s\n", name, state, defaultState, marker)
	}
	_ = r.sendSystemResultAndDelete(c, strings.TrimSpace(builder.String()))
}

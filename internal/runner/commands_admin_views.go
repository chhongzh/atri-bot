package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chhongzh/atri-bot/internal/account"
	"github.com/chhongzh/atri-bot/internal/chat"
	"gopkg.in/telebot.v4"
)

const adminListLimit = 30
const activeUserListLimit = 15

func (r *Runner) commandAdmins(c telebot.Context, _ []string) {
	role := account.RoleAdmin
	r.listAccounts(c, context.Background(), account.UserListFilter{Role: &role}, "管理员")
}

func (r *Runner) commandUsers(c telebot.Context, args []string) {
	filter, label, err := userListFilter(args)
	if err != nil {
		r.commandError(c, err)
		return
	}
	r.listAccounts(c, context.Background(), filter, label)
}

func (r *Runner) commandActiveUsers(c telebot.Context, _ []string) {
	ctx := context.Background()
	activeUsers := r.chats.ActiveUsers()
	if len(activeUsers) == 0 {
		_ = r.sendSystemResultAndDelete(c, "当前没有活跃的聊天状态。")
		return
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "活跃聊天状态（共 %d 位，最多显示 %d 位）：\n", len(activeUsers), activeUserListLimit)
	for index, active := range activeUsers {
		if index >= activeUserListLimit {
			break
		}
		user, err := r.accounts.Get(ctx, active.UserID)
		if err != nil {
			r.commandError(c, err)
			return
		}
		characterID := active.CharacterID
		if characterID == "" {
			characterID = "未选择"
		}
		fmt.Fprintf(
			&builder,
			"- %s\n  角色：%s；最近活动：%s\n",
			formatAccountUser(*user),
			characterID,
			formatAccountTime(active.LastActiveAt),
		)
	}
	_ = r.sendSystemResultAndDelete(c, strings.TrimSpace(builder.String()))
}

func (r *Runner) commandUser(c telebot.Context, args []string) {
	targetID, err := parseUserID(args, 0, "/user <user-id>")
	if err != nil {
		r.commandError(c, err)
		return
	}
	ctx := context.Background()
	user, err := r.accounts.Get(ctx, targetID)
	if err != nil {
		r.commandError(c, err)
		return
	}

	chatStatus := "未加载"
	if active, ok := findActiveUser(r.chats.ActiveUsers(), targetID); ok {
		chatStatus = "活跃，最近活动：" + formatAccountTime(active.LastActiveAt)
	}
	characterID := user.CharacterID
	if characterID == "" {
		characterID = "未选择"
	}
	_ = r.sendSystemResultAndDelete(c, fmt.Sprintf(
		"用户详情：\n- %s\n- 角色：%s\n- 聊天状态：%s\n- AI 配置：Base URL %s，API Key %s，模型 %s，最大轮数 %d\n- 创建时间：%s\n- 更新时间：%s",
		formatAccountUser(*user),
		characterID,
		chatStatus,
		configurationState(user.AIBaseURL),
		configurationState(user.AIAPIKey),
		configurationState(user.AIModel),
		user.AIMaxRounds,
		formatAccountTime(user.CreatedAt),
		formatAccountTime(user.UpdatedAt),
	))
}

func (r *Runner) showAdminStats(c telebot.Context, ctx context.Context) {
	stats, err := r.accounts.Stats(ctx)
	if err != nil {
		r.commandError(c, err)
		return
	}
	_ = r.sendSystemResultAndDelete(c, fmt.Sprintf(
		"账户统计：\n- 用户：%d\n- 管理员：%d\n- 已封禁：%d\n- 活跃聊天状态：%d",
		stats.Users,
		stats.Admins,
		stats.Banned,
		len(r.chats.ActiveUsers()),
	))
}

func (r *Runner) listAccounts(c telebot.Context, ctx context.Context, filter account.UserListFilter, label string) {
	filter.Limit = adminListLimit
	users, err := r.accounts.List(ctx, filter)
	if err != nil {
		r.commandError(c, err)
		return
	}
	if len(users) == 0 {
		_ = r.sendSystemResultAndDelete(c, "没有符合条件的"+label+"。")
		return
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s（最多显示 %d 位）：\n", label, adminListLimit)
	for _, user := range users {
		fmt.Fprintf(&builder, "- %s\n", formatAccountUser(user))
	}
	_ = r.sendSystemResultAndDelete(c, strings.TrimSpace(builder.String()))
}

func userListFilter(args []string) (account.UserListFilter, string, error) {
	switch commandAction(args, "all") {
	case "all":
		return account.UserListFilter{}, "用户", nil
	case "banned":
		banned := true
		return account.UserListFilter{Banned: &banned}, "已封禁用户", nil
	default:
		return account.UserListFilter{}, "", fmt.Errorf("用法：/users [all|banned]")
	}
}

func formatAccountUser(user account.User) string {
	username := singleLine(user.Username)
	displayName := singleLine(user.DisplayName)
	identity := displayName
	if username != "" {
		identity = "@" + strings.TrimPrefix(username, "@")
		if displayName != "" && !strings.EqualFold(displayName, username) {
			identity += "（" + displayName + "）"
		}
	}
	if identity == "" {
		identity = "未设置名称"
	}
	role := "用户"
	if user.Role == account.RoleAdmin {
		role = "管理员"
	}
	if user.Banned {
		role += "，已封禁"
	}
	return fmt.Sprintf("%s — %d（%s）", identity, user.TelegramID, role)
}

func findActiveUser(users []chat.ActiveUser, targetID int64) (chat.ActiveUser, bool) {
	for _, user := range users {
		if user.UserID == targetID {
			return user, true
		}
	}
	return chat.ActiveUser{}, false
}

func configurationState(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未设置"
	}
	return "已设置"
}

func formatAccountTime(value time.Time) string {
	if value.IsZero() {
		return "未知"
	}
	return value.Local().Format("2006-01-02 15:04:05")
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

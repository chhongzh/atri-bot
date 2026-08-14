package runner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chhongzh/atri-bot/internal/chat"
	"github.com/chhongzh/atri-bot/internal/model"
	"gopkg.in/telebot.v4"
)

const accountPageSize = 15
const activeUserPageSize = 10

func (r *Runner) commandAdmins(c telebot.Context, args []string) {
	page, err := parseOptionalPage(args, 0, "/admins [page]")
	if err != nil {
		r.commandError(c, err)
		return
	}
	role := model.RoleAdmin
	r.listAccounts(c, context.Background(), model.UserListFilter{Role: &role}, "管理员", page, "/admins")
}

func (r *Runner) commandUsers(c telebot.Context, args []string) {
	filter, label, page, pageCommand, err := userListRequest(args)
	if err != nil {
		r.commandError(c, err)
		return
	}
	r.listAccounts(c, context.Background(), filter, label, page, pageCommand)
}

func (r *Runner) commandActiveUsers(c telebot.Context, args []string) {
	page, err := parseOptionalPage(args, 0, "/active-users [page]")
	if err != nil {
		r.commandError(c, err)
		return
	}
	ctx := context.Background()
	activeUsers := r.chats.ActiveUsers()
	if len(activeUsers) == 0 {
		_ = r.sendSystemResultAndDelete(c, "当前没有活跃的聊天状态。")
		return
	}
	pages := pageCount(len(activeUsers), activeUserPageSize)
	if err := validatePage(page, pages); err != nil {
		r.commandError(c, err)
		return
	}
	start := (page - 1) * activeUserPageSize
	end := min(start+activeUserPageSize, len(activeUsers))
	if err := r.sendListResult(c, func(builder *strings.Builder) error {
		fmt.Fprintf(builder, "活跃聊天状态（第 %d/%d 页，共 %d 位）：\n", page, pages, len(activeUsers))
		for _, active := range activeUsers[start:end] {
			user, err := r.accounts.Get(ctx, active.UserID)
			if err != nil {
				return err
			}
			characterID := active.CharacterID
			if characterID == "" {
				characterID = "未选择"
			}
			fmt.Fprintf(
				builder,
				"- %s\n  角色：%s；最近活动：%s\n",
				formatAccountUser(*user),
				characterID,
				formatAccountTime(active.LastActiveAt),
			)
		}
		writePageFooter(builder, page, pages, "/active-users")
		return nil
	}); err != nil {
		r.commandError(c, err)
		return
	}
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
	settings, err := r.accounts.Settings(ctx, targetID)
	if err != nil {
		r.commandError(c, err)
		return
	}
	characterID := settings.CharacterID
	if characterID == "" {
		characterID = "未选择"
	}
	_ = r.sendSystemResultAndDelete(c, fmt.Sprintf(
		"用户详情：\n- %s\n- 角色：%s\n- 聊天状态：%s\n- AI 配置：Base URL %s，API Key %s，模型 %s，最大轮数 %d\n- 创建时间：%s\n- 更新时间：%s",
		formatAccountUser(*user),
		characterID,
		chatStatus,
		configurationState(settings.AIBaseURL),
		configurationState(settings.AIAPIKey),
		configurationState(settings.AIModel),
		settings.AIMaxRounds,
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

func (r *Runner) listAccounts(
	c telebot.Context,
	ctx context.Context,
	filter model.UserListFilter,
	label string,
	page int,
	pageCommand string,
) {
	result, err := r.accounts.ListPage(ctx, filter, page, accountPageSize)
	if err != nil {
		r.commandError(c, err)
		return
	}
	if result.Total == 0 {
		_ = r.sendSystemResultAndDelete(c, "没有符合条件的"+label+"。")
		return
	}
	if err = validatePage(page, result.Pages); err != nil {
		r.commandError(c, err)
		return
	}
	_ = r.sendListResult(c, func(builder *strings.Builder) error {
		fmt.Fprintf(builder, "%s（第 %d/%d 页，共 %d 位）：\n", label, page, result.Pages, result.Total)
		for _, user := range result.Users {
			fmt.Fprintf(builder, "- %s\n", formatAccountUser(user))
		}
		writePageFooter(builder, page, result.Pages, pageCommand)
		return nil
	})
}

func userListRequest(args []string) (model.UserListFilter, string, int, string, error) {
	const usage = "/users [all|banned] [page]"
	action := commandAction(args, "all")
	pageIndex := 1
	if _, err := strconv.Atoi(action); err == nil {
		action = "all"
		pageIndex = 0
	}
	page, err := parseOptionalPage(args, pageIndex, usage)
	if err != nil {
		return model.UserListFilter{}, "", 0, "", err
	}
	switch action {
	case "all":
		return model.UserListFilter{}, "用户", page, "/users all", nil
	case "banned":
		banned := true
		return model.UserListFilter{Banned: &banned}, "已封禁用户", page, "/users banned", nil
	default:
		return model.UserListFilter{}, "", 0, "", fmt.Errorf("用法：%s", usage)
	}
}

func pageCount(total, pageSize int) int {
	return (total + pageSize - 1) / pageSize
}

func validatePage(page, pages int) error {
	if page > pages {
		return fmt.Errorf("页码 %d 超出范围，共 %d 页", page, pages)
	}
	return nil
}

func writePageFooter(builder *strings.Builder, page, pages int, pageCommand string) {
	if page < pages {
		fmt.Fprintf(builder, "\n下一页：%s %d", pageCommand, page+1)
	}
	if page > 1 {
		fmt.Fprintf(builder, "\n上一页：%s %d", pageCommand, page-1)
	}
}

func formatAccountUser(user model.User) string {
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
	if user.Role == model.RoleAdmin {
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

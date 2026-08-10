package runner

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

func (r *Runner) registerCommands() error {
	registrations := []func() error{
		r.registerCharacterCommands,
		r.registerAICommands,
		r.registerProviderCommands,
		r.registerToolPermCommands,
		r.registerMCPCommands,
		r.registerAdminCommands,
	}
	for _, register := range registrations {
		if err := register(); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) commandStart(c telebot.Context, _ []string) {
	_ = r.sendSystemResultAndDelete(c, "你好，使用 /help 查看可用命令。")
}

func (r *Runner) commandError(c telebot.Context, err error) {
	if err == nil {
		return
	}
	if c.Sender() != nil {
		r.logger.Error("command failed",
			zap.Int64("user_id", c.Sender().ID),
			zap.Error(err),
		)
	}
	_ = r.sendSystemResultAndDelete(c, "操作失败："+err.Error())
}

func (r *Runner) broadcastAdmins(ctx context.Context, message string) {
	admins, err := r.accounts.Admins(ctx)
	if err != nil {
		r.logger.Error("failed to list administrators", zap.Error(err))
		return
	}
	for _, admin := range admins {
		recipient := &telebot.User{ID: admin.TelegramID}
		if _, err = r.bot.Send(recipient, message); err != nil {
			r.logger.Warn("failed to notify administrator",
				zap.Int64("admin_id", admin.TelegramID),
				zap.Error(err),
			)
			continue
		}
		r.deleteSystemResult(recipient)
	}
}

func (r *Runner) notifyAdminsOfError(ctx context.Context, bot telebot.API, user *telebot.User, err error) {
	if r.accounts == nil || bot == nil || user == nil {
		return
	}
	admins, listErr := r.accounts.Admins(ctx)
	if listErr != nil {
		r.logger.Error("failed to list administrators for error notification", zap.Error(listErr))
		return
	}

	errorMessage := "未知错误"
	if err != nil {
		errorMessage = err.Error()
	}
	message := fmt.Sprintf("用户处理消息时发生错误。\n用户 ID：%d\n用户名：@%s\n错误：\n%s", user.ID, user.Username, errorMessage)
	for _, admin := range admins {
		if _, sendErr := bot.Send(&telebot.User{ID: admin.TelegramID}, message); sendErr != nil {
			r.logger.Warn("failed to notify administrator about user error",
				zap.Int64("admin_id", admin.TelegramID),
				zap.Int64("user_id", user.ID),
				zap.Error(sendErr),
			)
		}
	}
}

func requireArgs(args []string, count int, usage string) error {
	if len(args) < count {
		return fmt.Errorf("用法：%s", usage)
	}
	return nil
}

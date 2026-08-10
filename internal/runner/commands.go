package runner

import (
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

func requireArgs(args []string, count int, usage string) error {
	if len(args) < count {
		return fmt.Errorf("用法：%s", usage)
	}
	return nil
}

// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package runner

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/chhongzh/atri-bot/internal/utils"
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
	r.logger.Debug("start command handled", utils.ExpandTelebotContext(c)...)
	_ = r.sendSystemResultAndDelete(c, "你好，使用 /help 查看可用命令。")
}

func (r *Runner) commandError(c telebot.Context, err error) {
	r.logger.Error("command failed",
		append(utils.ExpandTelebotContext(c), zap.Error(err))...,
	)
	_ = r.sendSystemResultAndDelete(c, "操作失败："+err.Error())
}

func (r *Runner) sendListResult(c telebot.Context, build func(*strings.Builder) error) error {
	var builder strings.Builder
	if err := build(&builder); err != nil {
		return err
	}
	return r.sendSystemResultAndDelete(c, strings.TrimSpace(builder.String()))
}

func commandAction(args []string, defaultAction string) string {
	if len(args) == 0 {
		return defaultAction
	}
	return strings.ToLower(strings.TrimSpace(args[0]))
}

func requireArgs(args []string, count int, usage string) error {
	if len(args) < count {
		return fmt.Errorf("用法：%s", usage)
	}
	return nil
}

func parseUserID(args []string, index int, usage string) (int64, error) {
	if err := requireArgs(args, index+1, usage); err != nil {
		return 0, err
	}
	userID, err := strconv.ParseInt(strings.TrimSpace(args[index]), 10, 64)
	if err != nil || userID <= 0 {
		return 0, fmt.Errorf("用户 ID 必须是正整数")
	}
	return userID, nil
}

func parseOptionalPage(args []string, index int, usage string) (int, error) {
	if len(args) <= index {
		return 1, nil
	}
	if len(args) != index+1 {
		return 0, fmt.Errorf("用法：%s", usage)
	}
	page, err := strconv.Atoi(strings.TrimSpace(args[index]))
	if err != nil || page <= 0 {
		return 0, fmt.Errorf("页码必须是正整数")
	}
	return page, nil
}

func requiredValue(args []string, index int, usage string) (string, error) {
	if err := requireArgs(args, index+1, usage); err != nil {
		return "", err
	}
	value := strings.TrimSpace(strings.Join(args[index:], " "))
	if value == "" {
		return "", fmt.Errorf("用法：%s", usage)
	}
	return value, nil
}

func optionalArg(args []string, index int) string {
	if index >= len(args) {
		return ""
	}
	return strings.TrimSpace(args[index])
}

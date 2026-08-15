// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chhongzh/atri-bot/internal/command"
	"github.com/chhongzh/atri-bot/internal/debounce"
	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/utils"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

const (
	chatActionRefreshInterval = 4 * time.Second
	chatDebounceInterval      = 5 * time.Second
	errorResultFormat         = "发生了错误，请联系机器人管理员处理：\n```\n%s\n```"
)

func (r *Runner) handlerForText(c telebot.Context) error {
	text := c.Text()
	isCommand := command.IsCommandText(text)
	if isCommand {
		if err := r.commands.Dispatch(c, text); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	fields := utils.ExpandTelebotContext(c)
	userFields := utils.ExpandUserFields(c)
	preparation := r.chats.Prepare(c)
	debounceStartedAt := time.Now()
	r.logger.Debug("chat message waiting for debounce", userFields...)
	if err := r.debouncer.Wait(ctx, c.Sender().ID); err != nil {
		switch {
		case errors.Is(err, debounce.ErrSuperseded):
			r.logger.Debug("chat message superseded during debounce", userFields...)
			return nil
		case errors.Is(err, debounce.ErrClosed):
			r.logger.Debug("chat message discarded while runner is stopping", userFields...)
			return nil
		default:
			return err
		}
	}
	r.logger.Debug("chat debounce completed",
		append(userFields, zap.Duration("elapsed", time.Since(debounceStartedAt)))...,
	)
	if err := preparation.Wait(ctx); err != nil {
		return r.handleChatError(c, err, fields)
	}
	if err := c.Notify(telebot.Typing); err != nil {
		return err
	}
	go r.maintainChatAction(ctx, c)

	r.logger.Debug("handling chat message", append(fields, zap.Bool("command", isCommand))...)
	start := time.Now()
	err := r.chats.Chat(ctx, c, text)
	if err = r.handleChatError(c, err, fields); err != nil {
		return err
	}
	r.logger.Debug("chat round completed", append(fields, zap.Duration("elapsed", time.Since(start)))...)
	return nil
}

func (r *Runner) handleChatError(c telebot.Context, err error, fields []zap.Field) error {
	if errors.Is(err, errs.ErrAIConfigIncomplete) {
		r.logger.Warn("user attempted chat without complete AI config", fields...)
		if err = c.Send("缺少 AI 配置，请先使用/ai配置你自己的 AI 连接"); err == nil {
			r.onMessageSent(c)
		}
		return err
	}
	return err
}

func (r *Runner) maintainChatAction(ctx context.Context, c telebot.Context) {
	ticker := time.NewTicker(chatActionRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Notify(telebot.Typing); err != nil {
				r.logger.Debug("failed to refresh chat action",
					append(utils.ExpandTelebotContext(c), zap.Error(err))...,
				)
			}
		}
	}
}

func (r *Runner) handlerForUnsupportedMedia(telebot.Context) error {
	return nil
}

func (r *Runner) handlerForError(err error, c telebot.Context) {
	if err == nil {
		err = errors.New("未知错误")
	}
	fields := []zap.Field{zap.Error(err)}
	if c == nil || c.Sender() == nil {
		r.logger.Error("failed to handle telegram update", fields...)
		return
	}
	fields = append(fields, utils.ExpandTelebotContext(c)...)
	r.logger.Error("failed to handle telegram update", fields...)

	r.sendAdminMessage(context.Background(), c.Bot(), adminMessage{
		Title:    "错误通知",
		Category: "消息处理错误",
		Fields: []adminMessageField{
			{Label: "用户 ID", Value: fmt.Sprint(c.Sender().ID)},
			{Label: "用户名", Value: "@" + c.Sender().Username},
		},
		DetailLabel: "错误详情",
		Detail:      err.Error(),
	})
	if sendErr := r.sendSystemResultAndDeleteOpts(c, formatErrorResult(err), telebot.ModeMarkdownV2); sendErr != nil {
		r.logger.Warn("failed to send error result to user",
			append(utils.ExpandTelebotContext(c), zap.Error(sendErr))...,
		)
	}
}

func formatErrorResult(err error) string {
	return fmt.Sprintf(errorResultFormat, utils.EscapeMarkdownV2Code(err.Error()))
}

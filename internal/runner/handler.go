// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chhongzh/atri-bot/internal/command"
	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/utils"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

const (
	chatActionRefreshInterval = 4 * time.Second
	errorResultFormat         = "发生了错误，请联系机器人管理员处理：\n```\n%s\n```"
)

func (r *Runner) handlerForText(c telebot.Context) error {
	receivedAt := time.Now()
	text := c.Text()
	isCommand := command.IsCommandText(text)
	fields := utils.ExpandTelebotContext(c)
	r.logger.Debug("telegram text handler started", append(fields, zap.Bool("command", isCommand))...)
	if isCommand {
		if err := r.commands.Dispatch(c, text); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	preparationStartedAt := time.Now()
	preparation := r.chats.Prepare(c)
	if err := preparation.Wait(ctx); err != nil {
		r.logger.Debug("chat state preparation wait failed",
			append(fields,
				zap.Duration("preparation_duration", time.Since(preparationStartedAt)),
				zap.Duration("total_elapsed", time.Since(receivedAt)),
				zap.Error(err),
			)...,
		)
		return r.handleChatError(c, err, fields)
	}
	preparationDuration := time.Since(preparationStartedAt)
	r.logger.Debug("chat state ready for telegram message",
		append(fields,
			zap.Duration("preparation_duration", preparationDuration),
			zap.Duration("total_elapsed", time.Since(receivedAt)),
		)...,
	)
	notifyStartedAt := time.Now()
	if err := c.Notify(telebot.Typing); err != nil {
		return err
	}
	notifyDuration := time.Since(notifyStartedAt)
	go r.maintainChatAction(ctx, c)

	chatStartedAt := time.Now()
	chatErr := r.chats.Chat(ctx, c, text, receivedAt)
	chatDuration := time.Since(chatStartedAt)
	if err := r.handleChatError(c, chatErr, fields); err != nil {
		r.logger.Debug("telegram chat request failed",
			append(fields,
				zap.Duration("preparation_duration", preparationDuration),
				zap.Duration("typing_notification_duration", notifyDuration),
				zap.Duration("chat_duration", chatDuration),
				zap.Duration("total_elapsed", time.Since(receivedAt)),
				zap.Error(err),
			)...,
		)
		return err
	}
	r.logger.Info("telegram chat request completed",
		append(fields,
			zap.Duration("preparation_duration", preparationDuration),
			zap.Duration("typing_notification_duration", notifyDuration),
			zap.Duration("chat_duration", chatDuration),
			zap.Duration("total_elapsed", time.Since(receivedAt)),
		)...,
	)
	return nil
}

func (r *Runner) handleChatError(c telebot.Context, err error, fields []zap.Field) error {
	if errors.Is(err, errs.ErrTurnPreempted) {
		return nil
	}
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

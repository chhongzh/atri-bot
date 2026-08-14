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
	errorResultPrefix         = "发生了错误，请联系机器人管理员处理：\n```\n"
)

func (r *Runner) handlerForText(c telebot.Context) error {
	text := c.Text()
	isCommand := command.IsCommandText(text)
	if isCommand {
		handled, err := r.commands.Dispatch(c, text)
		if handled || err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := c.Notify(telebot.Typing); err != nil {
		return err
	}
	go r.maintainChatAction(ctx, c)

	fields := utils.ExpandTelebotContext(c)
	r.logger.Debug("handling chat message", append(fields, zap.Bool("command", isCommand))...)
	start := time.Now()
	err := r.chats.Chat(ctx, c, text)
	if errors.Is(err, errs.ErrAIConfigIncomplete) {
		r.logger.Warn("user attempted chat without complete AI config", fields...)
		if err = c.Send("缺少 AI 配置，请先使用/ai配置你自己的 AI 连接"); err == nil {
			r.onMessageSent(c)
		}
		return err
	}
	if err != nil {
		return err
	}
	r.logger.Debug("chat round completed", append(fields, zap.Duration("elapsed", time.Since(start)))...)
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
		Detail:      formatErrorDetail(err),
	})
	if sendErr := r.sendSystemResultAndDeleteOpts(c, formatErrorResult(err), telebot.ModeMarkdownV2); sendErr != nil {
		r.logger.Warn("failed to send error result to user",
			append(utils.ExpandTelebotContext(c), zap.Error(sendErr))...,
		)
	}
}

func formatErrorResult(err error) string {
	return errorResultPrefix + escapeMarkdownV2Code(formatErrorDetail(err)) + "\n```"
}

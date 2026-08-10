package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chhongzh/atri-bot/internal/chat"
	"github.com/chhongzh/atri-bot/internal/command"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

const (
	chatActionRefreshInterval = 4 * time.Second
	errorResultPrefix         = "发生了错误，请联系机器人管理员处理：\n```\n"
)

func (r *Runner) handlerForText(c telebot.Context) error {
	text := c.Text()
	if command.IsCommandText(text) {
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

	err := r.chats.Chat(ctx, c, text)
	if errors.Is(err, chat.ErrAIConfigIncomplete) {
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
				r.logger.Debug("failed to refresh chat action", zap.Error(err))
			}
		}
	}
}

func (r *Runner) handlerForUnsupportedMedia(telebot.Context) error {
	return nil
}

func (r *Runner) handlerForError(err error, c telebot.Context) {
	fields := []zap.Field{zap.Error(err)}
	if c != nil && c.Sender() != nil {
		fields = append(fields,
			zap.Int64("user_id", c.Sender().ID),
			zap.String("username", c.Sender().Username),
		)
	}
	r.logger.Error("failed to handle telegram update", fields...)

	if c == nil || c.Sender() == nil {
		return
	}
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
			zap.Int64("user_id", c.Sender().ID),
			zap.Error(sendErr),
		)
	}
}

func formatErrorResult(err error) string {
	return errorResultPrefix + escapeMarkdownV2Code(formatErrorDetail(err)) + "\n```"
}

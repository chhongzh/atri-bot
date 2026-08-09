package runner

import (
	"context"
	"errors"
	"time"

	"github.com/chhongzh/atri-bot/internal/chat"
	"github.com/chhongzh/atri-bot/internal/command"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

const chatActionRefreshInterval = 4 * time.Second

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
		return c.Send("请先配置你自己的 AI 连接")
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
}

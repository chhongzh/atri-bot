// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chhongzh/atri-bot/internal/command"
	"github.com/chhongzh/atri-bot/internal/constants"
	"github.com/chhongzh/atri-bot/internal/errs"
	filesmanager "github.com/chhongzh/atri-bot/internal/files"
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

		return nil
	}
	return r.handleChatRequest(c, receivedAt, telebot.Typing, func(ctx context.Context) error {
		return r.chats.Chat(ctx, c, text, receivedAt)
	})
}

func (r *Runner) handleChatRequest(
	c telebot.Context,
	receivedAt time.Time,
	action telebot.ChatAction,
	chat func(context.Context) error,
) error {
	fields := utils.ExpandTelebotContext(c)
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
	if err := c.Notify(action); err != nil {
		return err
	}
	notifyDuration := time.Since(notifyStartedAt)
	go r.maintainChatAction(ctx, c, action)

	chatStartedAt := time.Now()
	chatErr := chat(ctx)
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

func (r *Runner) maintainChatAction(ctx context.Context, c telebot.Context, action telebot.ChatAction) {
	ticker := time.NewTicker(chatActionRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Notify(action); err != nil {
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

func (r *Runner) handlerForMedia(c telebot.Context) error {
	receivedAt := time.Now()
	settings, err := r.accounts.Settings(context.Background(), c.Sender().ID)
	if err != nil {
		return err
	}
	kind, file, name, caption := telegramMedia(c.Message())
	if file == nil {
		return nil
	}
	if kind == "" {
		return nil
	}
	if settings.CharacterID == "" {
		if _, ok := r.characters.Default(); !ok {
			return errs.ErrNoCharacters
		}
	}
	if strings.TrimSpace(caption) == "" {
		caption = "用户发送了" + map[string]string{"image": "一张图片", "audio": "一段音频", "video": "一段视频"}[kind]
	}
	return r.handleChatRequest(c, receivedAt, mediaChatAction(kind), func(ctx context.Context) error {
		if file.FileSize > constants.MaxUploadFileBytes {
			return fmt.Errorf("媒体文件不能超过 %d MB", constants.MaxUploadFileBytes>>20)
		}
		body, fileErr := c.Bot().File(file)
		if fileErr != nil {
			return fmt.Errorf("从 Telegram 读取媒体: %w", fileErr)
		}
		ref, saveErr := r.files.Save(ctx, kind, name, settings.AIImageMaxEdge, body, file.FileSize)
		if saveErr != nil {
			return fmt.Errorf("保存媒体: %w", saveErr)
		}
		return r.chats.ChatMedia(ctx, c, caption, []filesmanager.Ref{ref}, receivedAt)
	})
}

func mediaChatAction(kind string) telebot.ChatAction {
	switch kind {
	case "image":
		return telebot.UploadingPhoto
	case "audio":
		return telebot.UploadingAudio
	case "video":
		return telebot.UploadingVideo
	default:
		return telebot.Typing
	}
}

func telegramMedia(message *telebot.Message) (kind string, file *telebot.File, name, caption string) {
	if message == nil {
		return
	}
	switch {
	case message.Photo != nil:
		return "image", &message.Photo.File, "photo.jpg", message.Photo.Caption
	case message.Voice != nil:
		return "audio", &message.Voice.File, "voice.ogg", message.Voice.Caption
	case message.Audio != nil:
		return "audio", &message.Audio.File, message.Audio.FileName, message.Audio.Caption
	case message.Video != nil:
		return "video", &message.Video.File, message.Video.FileName, message.Video.Caption
	case message.Animation != nil:
		return "video", &message.Animation.File, message.Animation.FileName, message.Animation.Caption
	case message.VideoNote != nil:
		return "video", &message.VideoNote.File, "video-note.mp4", ""
	case message.Document != nil:
		mime := strings.ToLower(message.Document.MIME)
		if strings.HasPrefix(mime, "image/") {
			return "image", &message.Document.File, message.Document.FileName, message.Document.Caption
		}
		if strings.HasPrefix(mime, "audio/") {
			return "audio", &message.Document.File, message.Document.FileName, message.Document.Caption
		}
		if strings.HasPrefix(mime, "video/") {
			return "video", &message.Document.File, message.Document.FileName, message.Document.Caption
		}
	}
	return
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

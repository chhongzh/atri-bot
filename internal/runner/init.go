// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package runner

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/chhongzh/atri-bot/internal/account"
	"github.com/chhongzh/atri-bot/internal/character"
	"github.com/chhongzh/atri-bot/internal/chat"
	"github.com/chhongzh/atri-bot/internal/command"
	configmanager "github.com/chhongzh/atri-bot/internal/config"
	"github.com/chhongzh/atri-bot/internal/constants"
	filesmanager "github.com/chhongzh/atri-bot/internal/files"
	mcpmanager "github.com/chhongzh/atri-bot/internal/mcp"
	memorymanager "github.com/chhongzh/atri-bot/internal/memory"
	"github.com/chhongzh/atri-bot/internal/security"
	"github.com/chhongzh/atri-bot/internal/session"
	"github.com/chhongzh/atri-bot/internal/tools"
	builtinconfig "github.com/chhongzh/atri-bot/internal/tools/builtin/config"
	builtinexit "github.com/chhongzh/atri-bot/internal/tools/builtin/exit"
	builtinmcp "github.com/chhongzh/atri-bot/internal/tools/builtin/mcp"
	builtinmemory "github.com/chhongzh/atri-bot/internal/tools/builtin/memory"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

func (r *Runner) Init(ctx context.Context) error {
	if err := r.normalizeConfig(); err != nil {
		r.logger.Error("failed to normalize runner config", zap.Error(err))
		return err
	}
	r.logger.Info("runner initialization started",
		zap.String("cwd", r.cfg.CWD),
		zap.Int("default_max_rounds", r.cfg.DefaultMaxRounds),
		zap.Int("default_mcp_max_tools", r.cfg.MCPDefaultMaxTools),
	)

	r.configs = configmanager.New(r.db)
	r.accounts = account.New(r.db, r.logger, r.configs, r.cfg.DefaultMaxRounds, r.cfg.DefaultImageMaxEdge)
	if err := r.accounts.Init(); err != nil {
		r.logger.Error("failed to initialize account manager", zap.Error(err))
		return err
	}
	r.logger.Debug("account manager initialized")
	if err := r.configs.Set(ctx, configmanager.RuntimeSettingsKey, configmanager.RuntimeSettings{
		DefaultMaxRounds:       r.cfg.DefaultMaxRounds,
		DefaultImageMaxEdge:    r.cfg.DefaultImageMaxEdge,
		DefaultToolPermissions: r.cfg.DefaultToolPermissions,
		MCPDefaultMaxTools:     r.cfg.MCPDefaultMaxTools,
	}); err != nil {
		r.logger.Error("failed to persist runtime settings", zap.Error(err))
		return err
	}
	r.mcp = mcpmanager.New(context.WithoutCancel(ctx), r.logger, r.db, r.accounts, r.configs, r.cfg.AllowPrivateIP)
	if err := r.mcp.Init(); err != nil {
		r.logger.Error("failed to initialize mcp manager", zap.Error(err))
		return err
	}
	r.logger.Debug("mcp manager initialized")
	r.sessions = session.New(r.db, r.logger)
	if err := r.sessions.Init(); err != nil {
		r.logger.Error("failed to initialize session manager", zap.Error(err))
		return err
	}
	r.logger.Debug("session manager initialized")
	r.memories = memorymanager.New(r.db, r.logger)
	if err := r.memories.Init(); err != nil {
		r.logger.Error("failed to initialize memory manager", zap.Error(err))
		return err
	}
	r.logger.Debug("memory manager initialized")
	r.files = filesmanager.New(context.WithoutCancel(ctx), filepath.Join(r.cfg.CWD, "data", "files"), r.cfg.FilesMaxStorageBytes, r.cfg.FilesCleanupAfter, r.logger)
	if err := r.files.Init(); err != nil {
		return err
	}
	r.tools = tools.New(r.db, r.logger)
	if err := r.tools.RegisterAll(r.cfg.ToolRegistrars...); err != nil {
		r.logger.Error("failed to register tools", zap.Error(err))
		return err
	}
	if err := r.tools.Init(); err != nil {
		r.logger.Error("failed to initialize tool manager", zap.Error(err))
		return err
	}
	r.logger.Debug("tool manager initialized")
	if err := builtinconfig.Register(r.tools); err != nil {
		r.logger.Error("failed to register builtin config tool", zap.Error(err))
		return err
	}
	if err := builtinexit.Register(r.tools, r.logger); err != nil {
		r.logger.Error("failed to register builtin exit tool", zap.Error(err))
		return err
	}
	if err := builtinmemory.Register(r.tools, r.memories); err != nil {
		r.logger.Error("failed to register builtin memory tools", zap.Error(err))
		return err
	}
	if err := builtinmcp.Register(r.tools, r.mcp); err != nil {
		r.logger.Error("failed to register builtin mcp tool", zap.Error(err))
		return err
	}
	r.characters = character.New(r.db, r.logger, character.Config{
		CWD:          r.cfg.CWD,
		RemoteURL:    r.cfg.CharacterRepositoryURL,
		RemoteBranch: r.cfg.CharacterBranch,
	})
	if err := r.characters.Init(ctx); err != nil {
		r.logger.Error("failed to initialize character manager", zap.Error(err))
		return err
	}
	r.logger.Debug("character manager initialized")
	r.chats = chat.NewWithMemory(context.WithoutCancel(ctx), r.logger, r.db, r.accounts, r.configs, r.characters, r.sessions, r.tools, r.mcp, r.files, r.memories, chat.Config{
		StateTTL:          r.cfg.StateTTL,
		ModelTimeout:      r.cfg.AIModelTimeout,
		AllowPrivateIP:    r.cfg.AllowPrivateIP,
		SendLoadingResult: r.sendLoadingResultAndDelete,
		OnMessageSent:     r.onMessageSent,
	})
	if err := r.chats.Init(); err != nil {
		r.logger.Error("failed to initialize chat manager", zap.Error(err))
		return err
	}
	r.logger.Debug("chat manager initialized")
	r.commands = command.New(r.accounts, r.commandStart, r.sendSystemResultAndDelete)
	if err := r.registerCommands(); err != nil {
		r.logger.Error("failed to register commands", zap.Error(err))
		return err
	}
	if err := r.initBot(); err != nil {
		r.logger.Error("failed to create telegram bot", zap.Error(err))
		return err
	}
	r.logger.Debug("telegram bot created")

	r.bot.Use(r.middlewareForSender, r.middlewareForLogging, r.middlewareForSystemResultCleanup, r.accounts.UserMiddleware)
	r.bot.Handle(telebot.OnText, r.handlerForText)
	r.bot.Handle(telebot.OnPhoto, r.handlerForMedia)
	r.bot.Handle(telebot.OnVoice, r.handlerForMedia)
	r.bot.Handle(telebot.OnAudio, r.handlerForMedia)
	r.bot.Handle(telebot.OnVideo, r.handlerForMedia)
	r.bot.Handle(telebot.OnAnimation, r.handlerForMedia)
	r.bot.Handle(telebot.OnVideoNote, r.handlerForMedia)
	r.bot.Handle(telebot.OnDocument, r.handlerForMedia)
	for _, endpoint := range []string{
		telebot.OnContact,
		telebot.OnLocation,
		telebot.OnVenue,
		telebot.OnGame,
		telebot.OnDice,
		telebot.OnInvoice,
		telebot.OnPayment,
		telebot.OnRefund,
	} {
		r.bot.Handle(endpoint, r.handlerForUnsupportedMedia)
	}
	r.logger.Info("runner initialization completed")
	return nil
}

func (r *Runner) initBot() error {
	bot, err := telebot.NewBot(telebot.Settings{
		Token: r.cfg.BotToken,
		Client: &http.Client{
			Transport: security.DefaultSafeHTTPTransport(r.cfg.AllowPrivateIP),
		},
		OnError: r.handlerForError,
	})
	if err != nil {
		return err
	}
	r.bot = bot
	return nil
}

func (r *Runner) normalizeConfig() error {
	if r.cfg.CWD == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		r.cfg.CWD = cwd
	}
	abs, err := filepath.Abs(r.cfg.CWD)
	if err != nil {
		return err
	}
	r.cfg.CWD = abs
	if r.cfg.CharacterRepositoryURL == "" {
		r.cfg.CharacterRepositoryURL = constants.DefaultCharacterRepositoryURL
	}
	if r.cfg.CharacterBranch == "" {
		r.cfg.CharacterBranch = constants.DefaultCharacterBranch
	}
	for _, directory := range []string{"data", "chardefs"} {
		if err = os.MkdirAll(filepath.Join(r.cfg.CWD, directory), 0o755); err != nil {
			return err
		}
	}
	r.logger.Debug("runner config normalized",
		zap.String("cwd", r.cfg.CWD),
		zap.String("character_repository_url", r.cfg.CharacterRepositoryURL),
		zap.String("character_branch", r.cfg.CharacterBranch),
	)
	return nil
}

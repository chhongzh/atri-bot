package runner

import (
	"context"
	"os"
	"path/filepath"

	"github.com/chhongzh/atri-bot/internal/account"
	"github.com/chhongzh/atri-bot/internal/character"
	"github.com/chhongzh/atri-bot/internal/chat"
	"github.com/chhongzh/atri-bot/internal/command"
	mcpmanager "github.com/chhongzh/atri-bot/internal/mcp"
	"github.com/chhongzh/atri-bot/internal/session"
	"github.com/chhongzh/atri-bot/internal/tools"
	builtinconfig "github.com/chhongzh/atri-bot/internal/tools/builtin/config"
	builtinmcp "github.com/chhongzh/atri-bot/internal/tools/builtin/mcp"
	"gopkg.in/telebot.v4"
)

func (r *Runner) Init(ctx context.Context) error {
	if err := r.normalizeConfig(); err != nil {
		return err
	}

	r.accounts = account.New(r.db, r.logger, r.cfg.DefaultMaxRounds)
	if err := r.accounts.Init(); err != nil {
		return err
	}
	r.mcp = mcpmanager.New(context.WithoutCancel(ctx), r.logger, r.db, r.accounts, mcpmanager.Config{
		DefaultMaxTools: r.cfg.MCPDefaultMaxTools,
		BlockInternal:   r.cfg.MCPBlockInternal,
	})
	if err := r.mcp.Init(); err != nil {
		return err
	}
	r.sessions = session.New(r.db)
	if err := r.sessions.Init(); err != nil {
		return err
	}
	r.tools = tools.New(r.db, r.logger)
	if err := r.tools.RegisterAll(r.cfg.ToolRegistrars...); err != nil {
		return err
	}
	if err := r.tools.Init(); err != nil {
		return err
	}
	if err := builtinconfig.Register(r.tools); err != nil {
		return err
	}
	if err := builtinmcp.Register(r.tools, r.mcp); err != nil {
		return err
	}
	r.characters = character.New(r.db, r.logger, character.Config{
		CWD:          r.cfg.CWD,
		RemoteURL:    r.cfg.CharacterRepositoryURL,
		RemoteBranch: r.cfg.CharacterBranch,
	})
	if err := r.characters.Init(ctx); err != nil {
		return err
	}
	r.chats = chat.New(context.WithoutCancel(ctx), r.logger, r.db, r.accounts, r.characters, r.sessions, r.tools, r.mcp, chat.Config{
		StateTTL:               r.cfg.StateTTL,
		ModelTimeout:           r.cfg.AIModelTimeout,
		DefaultToolPermissions: r.cfg.DefaultToolPermissions,
		SendLoadingResult:      r.sendLoadingResultAndDelete,
		OnMessageSent:          r.onMessageSent,
	})
	if err := r.chats.Init(); err != nil {
		return err
	}
	r.commands = command.New(r.accounts, r.commandStart, r.sendSystemResultAndDelete)
	if err := r.registerCommands(); err != nil {
		return err
	}
	if err := r.initBot(); err != nil {
		return err
	}

	r.bot.Use(r.middlewareForSender, r.middlewareForLogging, r.middlewareForSystemResultCleanup, r.accounts.UserMiddleware)
	r.bot.Handle(telebot.OnText, r.handlerForText)
	for _, endpoint := range []string{
		telebot.OnMedia,
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
	return nil
}

func (r *Runner) initBot() error {
	bot, err := telebot.NewBot(telebot.Settings{
		Token:   r.cfg.BotToken,
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
		r.cfg.CharacterRepositoryURL = DefaultCharacterRepositoryURL
	}
	if r.cfg.CharacterBranch == "" {
		r.cfg.CharacterBranch = DefaultCharacterBranch
	}
	for _, directory := range []string{"data", "chardefs"} {
		if err = os.MkdirAll(filepath.Join(r.cfg.CWD, directory), 0o755); err != nil {
			return err
		}
	}
	return nil
}

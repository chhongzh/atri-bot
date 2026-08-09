package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/chhongzh/atri-bot/internal/account"
	"github.com/chhongzh/atri-bot/internal/character"
	"github.com/chhongzh/atri-bot/internal/chat"
	"github.com/chhongzh/atri-bot/internal/command"
	"github.com/chhongzh/atri-bot/internal/session"
	"github.com/chhongzh/atri-bot/internal/tools"
	"github.com/chhongzh/atri-bot/internal/tools/builtin"
	"gopkg.in/telebot.v4"
)

func (r *Runner) Init(ctx context.Context) error {
	if r.logger == nil {
		return errors.New("runner logger is nil")
	}
	if r.cfg == nil {
		return errors.New("runner config is nil")
	}
	if r.db == nil {
		return errors.New("runner database is nil")
	}
	if err := r.normalizeConfig(); err != nil {
		return err
	}

	r.accounts = account.New(r.db, r.logger, r.cfg.DefaultMaxRounds)
	if err := r.accounts.Init(); err != nil {
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
	if err := builtin.Register(r.tools); err != nil {
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
	r.chats = chat.New(context.WithoutCancel(ctx), r.logger, r.accounts, r.characters, r.sessions, r.tools, chat.Config{
		StateTTL:     r.cfg.StateTTL,
		ModelTimeout: r.cfg.AIModelTimeout,
	})
	r.commands = command.New(r.accounts, r.commandStart, r.sendSystemResultAndDelete)
	if err := r.registerCommands(); err != nil {
		return err
	}
	if err := r.initBot(); err != nil {
		return err
	}

	r.bot.Use(r.middlewareForLogging, r.middlewareForCommandResultCleanup, r.accounts.UserMiddleware)
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

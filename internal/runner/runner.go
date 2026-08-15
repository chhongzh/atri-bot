// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package runner

import (
	"sync"
	"time"

	"github.com/chhongzh/atri-bot/internal/account"
	"github.com/chhongzh/atri-bot/internal/character"
	"github.com/chhongzh/atri-bot/internal/chat"
	"github.com/chhongzh/atri-bot/internal/command"
	configmanager "github.com/chhongzh/atri-bot/internal/config"
	filesmanager "github.com/chhongzh/atri-bot/internal/files"
	mcpmanager "github.com/chhongzh/atri-bot/internal/mcp"
	"github.com/chhongzh/atri-bot/internal/session"
	"github.com/chhongzh/atri-bot/internal/tools"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
	"gorm.io/gorm"
)

const (
	DefaultCharacterRepositoryURL = "https://github.com/mihari-bot/chardef"
	DefaultCharacterBranch        = "main"
)

type Config struct {
	BotToken string
	CWD      string

	CharacterRepositoryURL string
	CharacterBranch        string

	StateTTL               time.Duration
	DefaultMaxRounds       int
	DefaultImageMaxEdge    int
	DefaultToolPermissions map[string]bool

	AIModelTimeout time.Duration
	AllowPrivateIP bool

	MCPDefaultMaxTools   int
	FilesMaxStorageBytes int64
	FilesCleanupAfter    time.Duration

	ToolRegistrars []tools.Registrar
}

type Runner struct {
	cfg    *Config
	logger *zap.Logger
	db     *gorm.DB
	bot    *telebot.Bot

	accounts   *account.Manager
	configs    *configmanager.Manager
	characters *character.Manager
	chats      *chat.Manager
	commands   *command.Manager
	sessions   *session.Manager
	tools      *tools.Manager
	mcp        *mcpmanager.Manager
	files      *filesmanager.Manager

	systemResultMu      sync.Mutex
	systemResultDeletes map[int64]func()
	stopping            bool
}

func New(logger *zap.Logger, cfg *Config, db *gorm.DB) *Runner {
	return &Runner{
		logger:              logger,
		cfg:                 cfg,
		db:                  db,
		systemResultDeletes: make(map[int64]func()),
	}
}

func (r *Runner) Start() {
	r.logger.Info("telegram bot polling started")
	r.bot.Start()
}

func (r *Runner) Stop() {
	r.logger.Info("runner shutting down")
	r.deleteAllSystemResults()
	r.logger.Debug("system results cleared")
	r.chats.Shutdown()
	r.logger.Debug("chat states shut down")
	r.files.Close()
	r.logger.Debug("media file cleanup stopped")
	r.mcp.Close()
	r.logger.Debug("mcp workers closed")
	r.bot.Stop()
	r.logger.Info("telegram bot stopped")
}

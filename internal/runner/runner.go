package runner

import (
	"sync"
	"time"

	"github.com/chhongzh/atri-bot/internal/account"
	"github.com/chhongzh/atri-bot/internal/character"
	"github.com/chhongzh/atri-bot/internal/chat"
	"github.com/chhongzh/atri-bot/internal/command"
	configmanager "github.com/chhongzh/atri-bot/internal/config"
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
	DefaultToolPermissions map[string]bool

	AIModelTimeout time.Duration
	AllowPrivateIP bool

	MCPDefaultMaxTools int

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
	r.mcp.Close()
	r.logger.Debug("mcp workers closed")
	r.bot.Stop()
	r.logger.Info("telegram bot stopped")
}

func (r *Runner) DB() *gorm.DB {
	return r.db
}

func (r *Runner) Bot() *telebot.Bot {
	return r.bot
}

func (r *Runner) AccountManager() *account.Manager {
	return r.accounts
}

func (r *Runner) ConfigManager() *configmanager.Manager {
	return r.configs
}

func (r *Runner) CharacterManager() *character.Manager {
	return r.characters
}

func (r *Runner) ChatManager() *chat.Manager {
	return r.chats
}

func (r *Runner) CommandManager() *command.Manager {
	return r.commands
}

func (r *Runner) SessionManager() *session.Manager {
	return r.sessions
}

func (r *Runner) ToolManager() *tools.Manager {
	return r.tools
}

func (r *Runner) MCPManager() *mcpmanager.Manager {
	return r.mcp
}

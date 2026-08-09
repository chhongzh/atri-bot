package runner

import (
	"sync"
	"time"

	"github.com/chhongzh/atri-bot/internal/account"
	"github.com/chhongzh/atri-bot/internal/character"
	"github.com/chhongzh/atri-bot/internal/chat"
	"github.com/chhongzh/atri-bot/internal/command"
	mcpmanager "github.com/chhongzh/atri-bot/internal/mcp"
	"github.com/chhongzh/atri-bot/internal/session"
	"github.com/chhongzh/atri-bot/internal/tools"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
	"gorm.io/gorm"
)

const (
	DefaultCharacterRepositoryURL = "https://github.com/mihari-bot/chardef"
	DefaultCharacterBranch        = "v2"
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

	MCPDefaultMaxTools int
	MCPBlockInternal   bool

	ToolRegistrars []tools.Registrar
}

type Runner struct {
	cfg    *Config
	logger *zap.Logger
	db     *gorm.DB
	bot    *telebot.Bot

	accounts   *account.Manager
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
	r.bot.Start()
}

func (r *Runner) Stop() {
	r.deleteAllSystemResults()
	if r.chats != nil {
		r.chats.Shutdown()
	}
	if r.mcp != nil {
		r.mcp.Close()
	}
	if r.bot != nil {
		r.bot.Stop()
	}
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

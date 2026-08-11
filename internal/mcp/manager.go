package mcp

import (
	"context"
	"errors"
	"sync"

	"github.com/chhongzh/atri-bot/internal/account"
	"github.com/cloudwego/eino/components/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const DefaultMaxTools = 32

var (
	ErrProviderNotFound = errors.New("mcp provider not found")
	ErrProviderExists   = errors.New("mcp provider already exists")
	ErrProviderLimit    = errors.New("mcp provider limit reached")
	ErrInvalidJSON      = errors.New("invalid mcp provider json field")
	ErrPathNotFound     = errors.New("mcp provider path not found")
	ErrPathForbidden    = errors.New("mcp provider path is read-only")
	ErrLoaderClosed     = errors.New("mcp loader is closed")
)

type Config struct {
	DefaultMaxTools int
	BlockInternal   bool
}

// LoadResult owns the remote tools and connections loaded for one chat state.
type LoadResult struct {
	Tools []tool.BaseTool

	closeOnce sync.Once
	closers   []func()
}

func (r *LoadResult) Close() {
	r.closeOnce.Do(func() {
		for i := len(r.closers) - 1; i >= 0; i-- {
			r.closers[i]()
		}
	})
}

// Manager owns MCP provider records and connection lifetimes.
type Manager struct {
	db       *gorm.DB
	logger   *zap.Logger
	accounts *account.Manager
	cfg      Config

	ctx    context.Context
	cancel context.CancelFunc

	lifecycleMu sync.Mutex
	closed      bool
	requestsWG  sync.WaitGroup
	providersMu sync.Mutex

	onChangeMu sync.RWMutex
	onChange   func(userID int64)
}

func New(ctx context.Context, logger *zap.Logger, db *gorm.DB, accounts *account.Manager, cfg Config) *Manager {
	if cfg.DefaultMaxTools <= 0 {
		cfg.DefaultMaxTools = DefaultMaxTools
	}
	managerCtx, cancel := context.WithCancel(ctx)
	return &Manager{
		db:       db,
		logger:   logger,
		accounts: accounts,
		cfg:      cfg,
		ctx:      managerCtx,
		cancel:   cancel,
		onChange: func(int64) {},
	}
}

func (m *Manager) Init() error {
	return m.db.AutoMigrate(&MCPProvider{})
}

// Close cancels active loads and waits for them to exit. It is safe to call
// more than once.
func (m *Manager) Close() {
	m.lifecycleMu.Lock()
	if m.closed {
		m.lifecycleMu.Unlock()
		return
	}
	m.closed = true
	m.cancel()
	m.lifecycleMu.Unlock()

	m.requestsWG.Wait()
	m.logger.Info("stopped mcp loader")
}

func (m *Manager) SetOnChange(handler func(userID int64)) {
	m.onChangeMu.Lock()
	m.onChange = handler
	m.onChangeMu.Unlock()
}

func (m *Manager) notifyChange(userID int64) {
	m.onChangeMu.RLock()
	handler := m.onChange
	m.onChangeMu.RUnlock()
	handler(userID)
}

// Load loads one user's providers concurrently and returns only after every
// provider has finished.
func (m *Manager) Load(
	ctx context.Context,
	userID int64,
	gate func(context.Context) (bool, error),
) (*LoadResult, error) {
	m.lifecycleMu.Lock()
	if m.closed {
		m.lifecycleMu.Unlock()
		return nil, ErrLoaderClosed
	}
	requestCtx, cancel := context.WithCancel(m.ctx)
	m.requestsWG.Add(1)
	m.lifecycleMu.Unlock()

	stopCancel := context.AfterFunc(ctx, cancel)
	defer m.requestsWG.Done()
	if err := ctx.Err(); err != nil {
		stopCancel()
		cancel()
		return nil, err
	}
	result, err := m.loadUserTools(requestCtx, userID, gate)
	stopCancel()
	if err != nil {
		cancel()
		return nil, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		result.Close()
		cancel()
		return nil, ctxErr
	}
	result.closers = append(result.closers, cancel)
	return result, nil
}

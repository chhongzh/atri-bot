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

const (
	DefaultWorkers  = 32
	DefaultMaxTools = 32
)

var (
	ErrProviderNotFound = errors.New("mcp provider not found")
	ErrProviderExists   = errors.New("mcp provider already exists")
	ErrProviderLimit    = errors.New("mcp provider limit reached")
	ErrInvalidJSON      = errors.New("invalid mcp provider json field")
	ErrPathNotFound     = errors.New("mcp provider path not found")
	ErrPathForbidden    = errors.New("mcp provider path is read-only")
	ErrLoaderNotStarted = errors.New("mcp loader is not started")
	ErrLoaderClosed     = errors.New("mcp loader is closed")
)

type Config struct {
	Workers         int
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
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		for i := len(r.closers) - 1; i >= 0; i-- {
			r.closers[i]()
		}
	})
}

// Manager owns MCP provider records, connection lifetimes and the bounded
// provider loader pool.
type Manager struct {
	db       *gorm.DB
	logger   *zap.Logger
	accounts *account.Manager
	cfg      Config

	ctx    context.Context
	cancel context.CancelFunc
	jobs   chan providerLoadJob

	lifecycleMu sync.Mutex
	started     bool
	closed      bool
	workersWG   sync.WaitGroup
	requestsWG  sync.WaitGroup
	providersMu sync.Mutex

	onChangeMu sync.RWMutex
	onChange   func(userID int64)
}

func New(ctx context.Context, logger *zap.Logger, db *gorm.DB, accounts *account.Manager, cfg Config) *Manager {
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultWorkers
	}
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
		jobs:     make(chan providerLoadJob, cfg.Workers*4),
	}
}

func (m *Manager) Init() error {
	return m.db.AutoMigrate(&MCPProvider{})
}

// Start starts the fixed-size provider loader pool exactly once.
func (m *Manager) Start() error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.closed {
		return ErrLoaderClosed
	}
	if m.started {
		return nil
	}
	m.started = true
	for workerID := 0; workerID < m.cfg.Workers; workerID++ {
		m.workersWG.Add(1)
		go m.worker(workerID)
	}
	m.logger.Info("started mcp loader workers",
		zap.Int("workers", m.cfg.Workers),
		zap.Int("default_max_tools", m.cfg.DefaultMaxTools),
		zap.Bool("block_internal", m.cfg.BlockInternal),
	)
	return nil
}

// Close cancels queued loads and active connections, then waits for all
// loader requests and workers to exit. It is safe to call more than once.
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
	m.workersWG.Wait()
	m.logger.Info("stopped mcp loader workers")
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
	if handler != nil {
		handler(userID)
	}
}

// LoadAsync loads one user's providers through the shared worker pool. The
// returned cancel function belongs to the chat state and must be called when
// that state is invalidated.
func (m *Manager) LoadAsync(
	userID int64,
	gate func(context.Context) (bool, error),
	callback func(*LoadResult, error),
) (context.CancelFunc, error) {
	if callback == nil {
		return nil, errors.New("mcp load callback is nil")
	}

	m.lifecycleMu.Lock()
	if m.closed {
		m.lifecycleMu.Unlock()
		return nil, ErrLoaderClosed
	}
	if !m.started {
		m.lifecycleMu.Unlock()
		return nil, ErrLoaderNotStarted
	}
	requestCtx, cancel := context.WithCancel(m.ctx)
	m.requestsWG.Add(1)
	m.lifecycleMu.Unlock()

	go func() {
		defer m.requestsWG.Done()
		m.loadUserTools(requestCtx, userID, gate, callback)
	}()
	return cancel, nil
}

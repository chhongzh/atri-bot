// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package chat

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/chhongzh/atri-bot/internal/account"
	"github.com/chhongzh/atri-bot/internal/character"
	configmanager "github.com/chhongzh/atri-bot/internal/config"
	"github.com/chhongzh/atri-bot/internal/constants"
	"github.com/chhongzh/atri-bot/internal/errs"
	filesmanager "github.com/chhongzh/atri-bot/internal/files"
	mcpmanager "github.com/chhongzh/atri-bot/internal/mcp"
	memorymanager "github.com/chhongzh/atri-bot/internal/memory"
	"github.com/chhongzh/atri-bot/internal/session"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
	"gorm.io/gorm"
)

type Config struct {
	StateTTL          time.Duration
	ModelTimeout      time.Duration
	AllowPrivateIP    bool
	SendLoadingResult func(telebot.Context, string) error
	OnMessageSent     func(telebot.Context)
}

type Manager struct {
	logger     *zap.Logger
	db         *gorm.DB
	accounts   *account.Manager
	configs    *configmanager.Manager
	characters *character.Manager
	sessions   *session.Manager
	tools      *toolmanager.Manager
	mcp        *mcpmanager.Manager
	files      *filesmanager.Manager
	memories   *memorymanager.Manager
	cfg        Config

	defaultToolPermissions map[string]bool

	ctx    context.Context
	cancel context.CancelFunc

	mu                  sync.Mutex
	states              map[int64]*UserState
	preparations        map[int64]*Preparation
	stateForPreparation func(context.Context, int64, telebot.Context) (*UserState, error)
	stopping            bool
}

func New(
	ctx context.Context,
	logger *zap.Logger,
	db *gorm.DB,
	accounts *account.Manager,
	configs *configmanager.Manager,
	characters *character.Manager,
	sessions *session.Manager,
	tools *toolmanager.Manager,
	mcpManager *mcpmanager.Manager,
	files *filesmanager.Manager,
	cfg Config,
) *Manager {
	return NewWithMemory(ctx, logger, db, accounts, configs, characters, sessions, tools, mcpManager, files, nil, cfg)
}

func NewWithMemory(
	ctx context.Context,
	logger *zap.Logger,
	db *gorm.DB,
	accounts *account.Manager,
	configs *configmanager.Manager,
	characters *character.Manager,
	sessions *session.Manager,
	tools *toolmanager.Manager,
	mcpManager *mcpmanager.Manager,
	files *filesmanager.Manager,
	memories *memorymanager.Manager,
	cfg Config,
) *Manager {
	if cfg.StateTTL <= 0 {
		cfg.StateTTL = constants.DefaultChatStateTTL
	}
	if cfg.ModelTimeout <= 0 {
		cfg.ModelTimeout = constants.DefaultAIModelTimeout
	}
	managerCtx, cancel := context.WithCancel(ctx)
	manager := &Manager{
		logger:                 logger,
		db:                     db,
		accounts:               accounts,
		configs:                configs,
		characters:             characters,
		sessions:               sessions,
		tools:                  tools,
		mcp:                    mcpManager,
		files:                  files,
		memories:               memories,
		cfg:                    cfg,
		defaultToolPermissions: make(map[string]bool),
		ctx:                    managerCtx,
		cancel:                 cancel,
		states:                 make(map[int64]*UserState),
		preparations:           make(map[int64]*Preparation),
	}
	manager.stateForPreparation = manager.state
	mcpManager.SetOnChange(manager.markStateStale)
	return manager
}

// markStateStale lets the current tool call finish. The state is invalidated
// before the user's next message, when the new provider set can be loaded.
func (m *Manager) markStateStale(userID int64) {
	m.mu.Lock()
	state := m.states[userID]
	m.mu.Unlock()
	if state != nil {
		state.markStale()
	}
}

func (m *Manager) Chat(ctx context.Context, c telebot.Context, text string, receivedAt time.Time) error {
	return m.chat(ctx, c, text, nil, receivedAt)
}

func (m *Manager) ChatMedia(ctx context.Context, c telebot.Context, text string, refs []filesmanager.Ref, receivedAt time.Time) error {
	ids := make([]string, len(refs))
	for index, ref := range refs {
		ids[index] = ref.ID
	}
	return m.chat(ctx, c, text, ids, receivedAt)
}

func (m *Manager) chat(ctx context.Context, c telebot.Context, text string, fileRefs []string, receivedAt time.Time) error {
	sender := c.Sender()
	request := newRequest(c, text, receivedAt)
	request.FileRefs = fileRefs
	for attempt := 0; attempt < 2; attempt++ {
		stateStartedAt := time.Now()
		state, err := m.state(ctx, sender.ID, c)
		if err != nil {
			m.logger.Debug("chat state lookup failed",
				zap.Int64("user_id", sender.ID),
				zap.Int("message_id", request.MessageID),
				zap.Duration("state_duration", time.Since(stateStartedAt)),
				zap.Duration("request_elapsed", time.Since(request.ReceivedAt)),
				zap.Error(err),
			)
			return err
		}
		stateDuration := time.Since(stateStartedAt)
		state.loopMu.Lock()
		if state.closed {
			state.loopMu.Unlock()
			m.removeState(state)
			continue
		}
		persistStartedAt := time.Now()
		shouldRestart, appendErr := m.prepareUserRequest(ctx, state, request)
		if appendErr != nil {
			state.loopMu.Unlock()
			m.logger.Debug("failed to persist chat user message",
				append(chatTraceFields(state, request),
					zap.Duration("session_write_duration", time.Since(persistStartedAt)),
					zap.Duration("request_elapsed", time.Since(request.ReceivedAt)),
					zap.Error(appendErr),
				)...,
			)
			return appendErr
		}
		persistDuration := time.Since(persistStartedAt)
		if shouldRestart {
			preemptStartedAt := time.Now()
			oldLoop := state.TurnLoop
			state.markImmediatePreempt(oldLoop)
			oldLoop.Stop(
				adk.WithImmediate(),
				adk.WithSkipCheckpoint(),
			)
			oldResult := oldLoop.Wait()
			if errors.Is(oldResult.ExitReason, errs.ErrInterruptedOutputPersistence) {
				state.loopMu.Unlock()
				return oldResult.ExitReason
			}
			appendStartedAt := time.Now()
			appendErr = m.appendPreparedUserRequest(ctx, state, request)
			persistDuration += time.Since(appendStartedAt)
			if appendErr != nil {
				state.loopMu.Unlock()
				m.logger.Debug("failed to persist interrupted chat user message",
					append(chatTraceFields(state, request),
						zap.Duration("session_write_duration", persistDuration),
						zap.Duration("preempt_duration", time.Since(preemptStartedAt)),
						zap.Duration("request_elapsed", time.Since(request.ReceivedAt)),
						zap.Error(appendErr),
					)...,
				)
				return appendErr
			}
			state.TurnLoop = m.newTurnLoop(state)
			m.startTurnLoop(state, state.TurnLoop)
			m.logger.Debug("immediately preempted chat turn",
				append(chatTraceFields(state, request),
					zap.Duration("preempt_duration", time.Since(preemptStartedAt)),
					zap.Duration("request_elapsed", time.Since(request.ReceivedAt)),
				)...,
			)
		}
		request.QueuedAt = time.Now()
		fields := chatTraceFields(state, request)
		m.logger.Debug("chat request persisted and queued",
			append(fields,
				zap.Int("attempt", attempt+1),
				zap.Duration("state_duration", stateDuration),
				zap.Duration("session_write_duration", persistDuration),
				zap.Duration("request_elapsed", time.Since(request.ReceivedAt)),
			)...,
		)
		accepted, _ := state.TurnLoop.Push(request)
		state.loopMu.Unlock()
		if accepted {
			select {
			case err = <-request.done:
				completionFields := append(chatTraceFields(state, request),
					zap.Duration("turn_loop_duration", time.Since(request.QueuedAt)),
					zap.Duration("request_elapsed", time.Since(request.ReceivedAt)),
				)
				if err != nil && !errors.Is(err, errs.ErrTurnPreempted) {
					m.logger.Debug("chat request completed with error", append(completionFields, zap.Error(err))...)
				} else {
					m.logger.Debug("chat request completed", completionFields...)
				}
				if errors.Is(err, errs.ErrTurnPreempted) {
					return nil
				}
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		m.logger.Debug("chat request rejected by stopped turn loop",
			append(fields, zap.Duration("request_elapsed", time.Since(request.ReceivedAt)))...,
		)
		m.Invalidate(sender.ID)
	}
	return errs.ErrStateStopped
}

func (m *Manager) prepareUserRequest(ctx context.Context, state *UserState, request *Request) (bool, error) {
	state.roundMu.Lock()
	defer state.roundMu.Unlock()

	if request.RoundID != 0 {
		state.activeRoundID = request.RoundID
		state.roundRevision = max(state.roundRevision, request.Revision)
		return false, nil
	}
	if state.activeRoundID == 0 {
		roundID, err := m.sessions.StartRound(ctx, state.UserID, state.CharacterID, request.message())
		if err != nil {
			return false, err
		}
		state.activeRoundID = roundID
		state.roundRevision = 1
		request.RoundID = roundID
		request.Revision = state.roundRevision
		return false, nil
	}
	state.roundRevision++
	request.RoundID = state.activeRoundID
	request.Revision = state.roundRevision
	return true, nil
}

func (m *Manager) appendPreparedUserRequest(ctx context.Context, state *UserState, request *Request) error {
	state.roundMu.Lock()
	defer state.roundMu.Unlock()
	if state.activeRoundID != request.RoundID || state.roundRevision != request.Revision {
		return errs.ErrChatRoundChanged
	}
	return m.sessions.AppendUser(ctx, state.UserID, state.CharacterID, request.RoundID, request.message())
}

func (m *Manager) Invalidate(userID int64) {
	m.cancelPreparation(userID)
	m.invalidateState(userID)
}

func (m *Manager) invalidateState(userID int64) {
	m.mu.Lock()
	state := m.states[userID]
	m.mu.Unlock()
	if state == nil {
		return
	}
	loop, claimed := m.claimState(state, nil)
	if !claimed {
		return
	}
	loop.Stop(
		adk.WithGracefulTimeout(5*time.Second),
		adk.WithSkipCheckpoint(),
		adk.WithStopCause("state invalidated"),
	)
	loop.Wait()
	state.closeMCP()
}

func (m *Manager) InvalidateAll() {
	m.cancelAllPreparations(false)
	states := m.snapshotStates()
	claimedStates := make([]*UserState, 0, len(states))
	loops := make([]*adk.TurnLoop[*Request, *schema.Message], 0, len(states))
	for _, state := range states {
		loop, claimed := m.claimState(state, nil)
		if !claimed {
			continue
		}
		claimedStates = append(claimedStates, state)
		loops = append(loops, loop)
		loop.Stop(
			adk.WithGracefulTimeout(5*time.Second),
			adk.WithSkipCheckpoint(),
			adk.WithStopCause("all states invalidated"),
		)
	}
	for _, loop := range loops {
		loop.Wait()
	}
	for _, state := range claimedStates {
		state.closeMCP()
	}
}

// ActiveUsers returns the chat states currently retained in memory.
// A retained state is available to serve a user until its TTL expires.
func (m *Manager) ActiveUsers() []ActiveUser {
	states := m.snapshotStates()

	users := make([]ActiveUser, 0, len(states))
	for _, state := range states {
		users = append(users, state.activeUser())
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].UserID < users[j].UserID
	})
	return users
}

func (m *Manager) Shutdown() {
	m.cancel()
	m.cancelAllPreparations(true)
	states := m.snapshotStates()
	claimedStates := make([]*UserState, 0, len(states))
	loops := make([]*adk.TurnLoop[*Request, *schema.Message], 0, len(states))
	for _, state := range states {
		loop, claimed := m.claimState(state, nil)
		if !claimed {
			continue
		}
		claimedStates = append(claimedStates, state)
		loops = append(loops, loop)
		loop.Stop(adk.WithImmediate(), adk.WithSkipCheckpoint(), adk.WithStopCause("shutdown"))
	}
	for _, loop := range loops {
		loop.Wait()
	}
	for _, state := range claimedStates {
		state.closeMCP()
	}
	m.mu.Lock()
	m.states = make(map[int64]*UserState)
	m.mu.Unlock()
}

func (m *Manager) snapshotStates() []*UserState {
	m.mu.Lock()
	defer m.mu.Unlock()
	states := make([]*UserState, 0, len(m.states))
	for _, state := range m.states {
		states = append(states, state)
	}
	return states
}

func (m *Manager) removeState(state *UserState) {
	m.mu.Lock()
	if m.states[state.UserID] == state {
		delete(m.states, state.UserID)
	}
	m.mu.Unlock()
}

func (m *Manager) claimState(
	state *UserState,
	expectedLoop *adk.TurnLoop[*Request, *schema.Message],
) (*adk.TurnLoop[*Request, *schema.Message], bool) {
	state.loopMu.Lock()
	defer state.loopMu.Unlock()
	if state.closed || expectedLoop != nil && state.TurnLoop != expectedLoop {
		return nil, false
	}
	state.closed = true
	loop := state.TurnLoop
	m.removeState(state)
	return loop, true
}

func (m *Manager) state(ctx context.Context, userID int64, c telebot.Context) (*UserState, error) {
	m.mu.Lock()
	if state := m.states[userID]; state != nil {
		if !state.isStale() {
			state.TelebotContext = c
			state.touch()
			m.mu.Unlock()
			return state, nil
		}
		m.mu.Unlock()
		m.invalidateState(userID)
	} else {
		m.mu.Unlock()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	state, err := m.newState(ctx, userID, c)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if err = ctx.Err(); err != nil {
		m.mu.Unlock()
		state.closeMCP()
		return nil, err
	}
	if existing := m.states[userID]; existing != nil {
		m.mu.Unlock()
		state.TurnLoop.Stop(adk.WithImmediate(), adk.WithSkipCheckpoint())
		state.closeMCP()
		return existing, nil
	}
	m.states[userID] = state
	m.mu.Unlock()

	m.startTurnLoop(state, state.TurnLoop)
	return state, nil
}

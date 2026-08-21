// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
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
	"github.com/chhongzh/atri-bot/internal/msgops"
	"github.com/chhongzh/atri-bot/internal/security"
	"github.com/chhongzh/atri-bot/internal/session"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/chhongzh/atri-bot/internal/utils"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/dynamictool/toolsearch"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	pkgErrors "github.com/pkg/errors"
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

func (m *Manager) startTurnLoop(state *UserState, loop *adk.TurnLoop[*Request, *schema.Message]) {
	loop.Run(m.ctx)
	loop.Stop(
		adk.UntilIdleFor(m.cfg.StateTTL),
		adk.WithSkipCheckpoint(),
		adk.WithStopCause("state expired"),
	)
	go m.watchState(state, loop)
}

func (m *Manager) newState(ctx context.Context, userID int64, c telebot.Context) (*UserState, error) {
	startedAt := time.Now()
	if err := m.cfg.SendLoadingResult(c, "正在加载聊天状态，请稍候。"); err != nil {
		m.logger.Warn("failed to send chat state loading message",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
	}

	settings, err := m.accounts.Settings(ctx, userID)
	if err != nil {
		return nil, err
	}
	characterID := settings.CharacterID
	if characterID == "" {
		return nil, errs.ErrCharacterNotSelected
	} else if _, ok := m.characters.Get(characterID); !ok {
		return nil, errs.CharacterUnavailable(characterID)
	}

	missing := make([]string, 0, 3)
	if strings.TrimSpace(settings.AIBaseURL) == "" {
		missing = append(missing, "base-url")
	}
	if strings.TrimSpace(settings.AIAPIKey) == "" {
		missing = append(missing, "key")
	}
	if strings.TrimSpace(settings.AIModel) == "" {
		missing = append(missing, "model")
	}
	if len(missing) > 0 {
		return nil, errs.AIConfigIncomplete(missing)
	}
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: strings.TrimSpace(settings.AIBaseURL),
		APIKey:  strings.TrimSpace(settings.AIAPIKey),
		Model:   strings.TrimSpace(settings.AIModel),
		Timeout: m.cfg.ModelTimeout,
		HTTPClient: &http.Client{
			Transport: security.DefaultSafeHTTPTransport(m.cfg.AllowPrivateIP),
			Timeout:   m.cfg.ModelTimeout,
		},
	})
	if err != nil {
		return nil, err
	}
	modelForAgent := model.ToolCallingChatModel(&filesModel{inner: chatModel, files: m.files})
	mcpResult, err := m.mcp.Load(ctx, userID, func(ctx context.Context) (bool, error) {
		return m.ToolAllowed(ctx, userID, "mcp")
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errs.ErrLoaderClosed) {
			return nil, err
		}
		m.logger.Warn("mcp loading failed",
			zap.Int64("user_id", userID),
			zap.Error(err),
		)
		mcpResult = &mcpmanager.LoadResult{}
	}
	var mcpTools []tool.BaseTool
	if len(mcpResult.Tools) == 0 {
		mcpResult.Close()
	} else {
		mcpTools = mcpResult.Tools
	}
	state := &UserState{
		UserID:         userID,
		CharacterID:    characterID,
		MaxRounds:      settings.AIMaxRounds,
		TelebotContext: c,
		CreatedAt:      time.Now(),
		LastActiveAt:   time.Now(),
	}
	agent, err := m.buildAgent(ctx, modelForAgent, userID, mcpTools)
	if err != nil {
		mcpResult.Close()
		return nil, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	state.Agent = agent
	state.Runner = runner
	state.mcpClose = mcpResult.Close
	state.TurnLoop = m.newTurnLoop(state)
	if len(mcpTools) > 0 {
		m.logger.Info("attached mcp tools to chat state",
			zap.Int64("user_id", userID),
			zap.Int("mcp_tools", len(mcpTools)),
		)
	}
	m.logger.Info("created user chat state",
		zap.Int64("user_id", userID),
		zap.String("character_id", characterID),
		zap.Duration("elapsed", time.Since(startedAt)),
	)
	return state, nil
}

func (m *Manager) newTurnLoop(state *UserState) *adk.TurnLoop[*Request, *schema.Message] {
	var loop *adk.TurnLoop[*Request, *schema.Message]
	loop = adk.NewTurnLoop(adk.TurnLoopConfig[*Request, *schema.Message]{
		GenInput: func(turnCtx context.Context, _ *adk.TurnLoop[*Request, *schema.Message], items []*Request) (*adk.GenInputResult[*Request, *schema.Message], error) {
			return m.genInput(turnCtx, state, items)
		},
		PrepareAgent: func(context.Context, *adk.TurnLoop[*Request, *schema.Message], []*Request) (adk.Agent, error) {
			return state.agent(), nil
		},
		OnAgentEvents: func(turnCtx context.Context, turn *adk.TurnContext[*Request, *schema.Message], events *adk.AsyncIterator[*adk.AgentEvent]) error {
			return m.onAgentEvents(turnCtx, state, loop, turn, events)
		},
	})
	return loop
}

func (m *Manager) buildAgent(
	ctx context.Context,
	model model.ToolCallingChatModel,
	userID int64,
	mcpTools []tool.BaseTool,
) (*adk.ChatModelAgent, error) {
	static, err := m.allowedTools(ctx, userID)
	if err != nil {
		return nil, err
	}
	return buildAgentWithTools(ctx, model, static, mcpTools)
}

func buildAgentWithTools(
	ctx context.Context,
	model model.ToolCallingChatModel,
	static []tool.BaseTool,
	mcpTools []tool.BaseTool,
) (*adk.ChatModelAgent, error) {
	handlers := []adk.ChatModelAgentMiddleware{&safeToolMiddleware{}}
	if len(mcpTools) > 0 {
		search, searchErr := toolsearch.New(ctx, &toolsearch.Config{DynamicTools: mcpTools})
		if searchErr != nil {
			return nil, pkgErrors.Wrap(searchErr, "create MCP tool search")
		}
		handlers = append([]adk.ChatModelAgentMiddleware{search}, handlers...)
	}
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Model:       model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: toolNodeConfig(static)},
		Handlers:    handlers,
	})
}

func (m *Manager) renderSystemPrompt(ctx context.Context, state *UserState, c telebot.Context) (string, error) {
	sender := c.Sender()
	username := utils.FindFirstNonEmpty(sender.Username, utils.FormatTelegramUsername(sender))
	return m.characters.RenderSystemPrompt(ctx, state.CharacterID, username)
}

func (m *Manager) genInput(ctx context.Context, state *UserState, items []*Request) (*adk.GenInputResult[*Request, *schema.Message], error) {
	startedAt := time.Now()
	latest := items[len(items)-1]
	for _, item := range items {
		item.TurnAt = startedAt
	}
	roundID := latest.RoundID
	fields := chatTraceFields(state, latest)
	m.logger.Debug("chat turn input preparation started",
		append(fields,
			zap.Int("consumed_requests", len(items)),
			zap.Duration("queue_wait", durationSince(latest.QueuedAt, startedAt)),
			zap.Duration("request_elapsed", durationSince(latest.ReceivedAt, startedAt)),
		)...,
	)
	for _, item := range items {
		if item.RoundID != roundID {
			err := errs.ErrTurnMixedSessionRounds
			m.logger.Warn("chat turn input preparation failed",
				append(fields,
					zap.String("stage", "validate_rounds"),
					zap.Duration("input_preparation_duration", time.Since(startedAt)),
					zap.Error(err),
				)...,
			)
			return nil, err
		}
	}
	promptStartedAt := time.Now()
	systemPrompt, err := m.renderSystemPrompt(ctx, state, latest.Context)
	if err != nil {
		m.logger.Warn("chat turn input preparation failed",
			append(fields,
				zap.String("stage", "render_system_prompt"),
				zap.Duration("system_prompt_duration", time.Since(promptStartedAt)),
				zap.Duration("input_preparation_duration", time.Since(startedAt)),
				zap.Error(err),
			)...,
		)
		return nil, err
	}
	var memoryMessage *schema.Message
	if m.memories != nil {
		memoryMessage, err = m.memories.Render(ctx, state.UserID)
		if err != nil {
			m.logger.Warn("chat turn input preparation failed",
				append(fields,
					zap.String("stage", "render_memory_prompt"),
					zap.Duration("system_prompt_duration", time.Since(promptStartedAt)),
					zap.Duration("input_preparation_duration", time.Since(startedAt)),
					zap.Error(err),
				)...,
			)
			return nil, err
		}
	}
	promptDuration := time.Since(promptStartedAt)
	runCtx := toolmanager.WithRunningState(ctx, &toolmanager.RunningState{
		UserID:         state.UserID,
		CharacterID:    state.CharacterID,
		TelebotContext: latest.Context,
	})
	sessionCtx, cancelSession := context.WithTimeout(context.WithoutCancel(runCtx), m.cfg.ModelTimeout)
	stopSession := context.AfterFunc(m.ctx, cancelSession)
	defer func() {
		stopSession()
		cancelSession()
	}()
	historyStartedAt := time.Now()
	history, err := m.sessions.Load(sessionCtx, state.UserID, state.CharacterID, roundID, session.CompressionOptions{
		MaxRounds: state.MaxRounds,
		Agent:     state.agent(),
		Memory:    memoryMessage,
	})
	if err != nil {
		m.logger.Warn("chat turn input preparation failed",
			append(fields,
				zap.String("stage", "load_session_history"),
				zap.Duration("system_prompt_duration", promptDuration),
				zap.Duration("history_load_duration", time.Since(historyStartedAt)),
				zap.Duration("input_preparation_duration", time.Since(startedAt)),
				zap.Error(err),
			)...,
		)
		return nil, err
	}
	historyDuration := time.Since(historyStartedAt)
	messages := make([]*schema.Message, 0, len(history)+2)
	messages = append(messages, schema.SystemMessage(systemPrompt))
	if memoryMessage != nil {
		messages = append(messages, memoryMessage)
	}
	messages = append(messages, history...)
	m.logger.Debug("prepared chat turn",
		append(fields,
			zap.Int("history_messages", len(history)),
			zap.Int("input_messages", len(messages)),
			zap.Int("consumed_requests", len(items)),
			zap.Duration("queue_wait", durationSince(latest.QueuedAt, startedAt)),
			zap.Duration("system_prompt_duration", promptDuration),
			zap.Duration("history_load_duration", historyDuration),
			zap.Duration("input_preparation_duration", time.Since(startedAt)),
			zap.Duration("request_elapsed", durationSince(latest.ReceivedAt, time.Now())),
		)...,
	)
	return &adk.GenInputResult[*Request, *schema.Message]{
		RunCtx:   runCtx,
		Input:    &adk.AgentInput{Messages: messages, EnableStreaming: true},
		Consumed: items,
	}, nil
}

func (m *Manager) onAgentEvents(
	ctx context.Context,
	state *UserState,
	loop *adk.TurnLoop[*Request, *schema.Message],
	turn *adk.TurnContext[*Request, *schema.Message],
	events *adk.AsyncIterator[*adk.AgentEvent],
) (resultErr error) {
	var (
		outputs    []*schema.Message
		turnErr    error
		silentExit bool
	)
	latest := turn.Consumed[len(turn.Consumed)-1]
	eventsStartedAt := time.Now()
	turnStartedAt := latest.TurnAt
	if turnStartedAt.IsZero() {
		turnStartedAt = eventsStartedAt
	}
	requestStartedAt := latest.ReceivedAt
	if requestStartedAt.IsZero() {
		requestStartedAt = turnStartedAt
	}
	fields := chatTraceFields(state, latest)
	outcome := "running"
	var (
		eventCount            int
		outputEventCount      int
		chunkCount            int
		contentCharacters     int
		toolCallCount         int
		sentBlocks            int
		streamReceiveDuration time.Duration
		persistenceDuration   time.Duration
		completionDuration    time.Duration
		firstEventAt          time.Time
		firstOutputAt         time.Time
		firstChunkAt          time.Time
		firstTokenAt          time.Time
		firstBlockReadyAt     time.Time
		firstBlockSentAt      time.Time
	)
	defer func() {
		traceFields := append(chatTraceFields(state, latest),
			zap.String("outcome", outcome),
			zap.Int("events", eventCount),
			zap.Int("output_events", outputEventCount),
			zap.Int("chunks", chunkCount),
			zap.Int("content_characters", contentCharacters),
			zap.Int("tool_calls", toolCallCount),
			zap.Int("output_messages", len(outputs)),
			zap.Int("sent_blocks", sentBlocks),
			zap.Duration("queue_wait", durationSince(latest.QueuedAt, turnStartedAt)),
			zap.Duration("first_event_latency", durationSince(turnStartedAt, firstEventAt)),
			zap.Duration("first_output_latency", durationSince(turnStartedAt, firstOutputAt)),
			zap.Duration("first_chunk_latency", durationSince(turnStartedAt, firstChunkAt)),
			zap.Duration("first_token_latency", durationSince(turnStartedAt, firstTokenAt)),
			zap.Duration("first_block_ready_latency", durationSince(turnStartedAt, firstBlockReadyAt)),
			zap.Duration("first_block_latency", durationSince(turnStartedAt, firstBlockSentAt)),
			zap.Duration("stream_receive_duration", streamReceiveDuration),
			zap.Duration("event_loop_duration", time.Since(eventsStartedAt)),
			zap.Duration("persistence_duration", persistenceDuration),
			zap.Duration("round_completion_duration", completionDuration),
			zap.Duration("turn_elapsed", time.Since(turnStartedAt)),
			zap.Duration("request_elapsed", time.Since(requestStartedAt)),
		)
		if resultErr != nil {
			traceFields = append(traceFields, zap.Error(resultErr))
		}
		m.logger.Info("chat turn trace completed", traceFields...)
	}()
	m.logger.Debug("agent event loop started",
		append(fields,
			zap.Duration("queue_wait", durationSince(latest.QueuedAt, turnStartedAt)),
			zap.Duration("request_elapsed", time.Since(requestStartedAt)),
		)...,
	)

	streamWriter := utils.NewAssistantStreamWriter(func(text string) error {
		blockReadyAt := time.Now()
		if firstBlockReadyAt.IsZero() {
			firstBlockReadyAt = blockReadyAt
		}
		sendStartedAt := time.Now()
		if err := utils.SendTelegramText(latest.Context, text); err != nil {
			m.logger.Warn("failed to send assistant stream block",
				append(chatTraceFields(state, latest),
					zap.Int("block", sentBlocks+1),
					zap.Int("characters", len([]rune(text))),
					zap.Duration("send_duration", time.Since(sendStartedAt)),
					zap.Duration("turn_elapsed", time.Since(turnStartedAt)),
					zap.Error(err),
				)...,
			)
			return err
		}
		sentBlocks++
		sentAt := time.Now()
		if firstBlockSentAt.IsZero() {
			firstBlockSentAt = sentAt
			m.logger.Info("first assistant stream block sent",
				append(chatTraceFields(state, latest),
					zap.Int("characters", len([]rune(text))),
					zap.Duration("first_block_ready_latency", durationSince(turnStartedAt, firstBlockReadyAt)),
					zap.Duration("first_block_latency", durationSince(turnStartedAt, firstBlockSentAt)),
					zap.Duration("request_elapsed", durationSince(requestStartedAt, firstBlockSentAt)),
				)...,
			)
		}
		m.logger.Debug("sent assistant stream block",
			append(chatTraceFields(state, latest),
				zap.Int("block", sentBlocks),
				zap.Int("characters", len([]rune(text))),
				zap.Duration("send_duration", sentAt.Sub(sendStartedAt)),
				zap.Duration("turn_elapsed", sentAt.Sub(turnStartedAt)),
			)...,
		)
		m.cfg.OnMessageSent(latest.Context)
		return nil
	})
	for {
		waitStartedAt := time.Now()
		event, ok := events.Next()
		eventReceivedAt := time.Now()
		if !ok {
			m.logger.Debug("agent event iterator exhausted",
				append(chatTraceFields(state, latest),
					zap.Duration("event_wait_duration", eventReceivedAt.Sub(waitStartedAt)),
					zap.Duration("event_loop_duration", eventReceivedAt.Sub(eventsStartedAt)),
				)...,
			)
			break
		}
		eventCount++
		if firstEventAt.IsZero() {
			firstEventAt = eventReceivedAt
			m.logger.Debug("received first agent event",
				append(chatTraceFields(state, latest),
					zap.Duration("first_event_latency", durationSince(turnStartedAt, firstEventAt)),
					zap.Duration("request_elapsed", durationSince(requestStartedAt, firstEventAt)),
				)...,
			)
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			m.logger.Debug("agent event returned error",
				append(chatTraceFields(state, latest),
					zap.Int("event", eventCount),
					zap.Duration("event_wait_duration", eventReceivedAt.Sub(waitStartedAt)),
					zap.Duration("turn_elapsed", eventReceivedAt.Sub(turnStartedAt)),
					zap.Error(event.Err),
				)...,
			)
			if turnErr == nil {
				turnErr = event.Err
			}
			continue
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			silentExit = true
			m.logger.Debug("agent requested interrupt",
				append(chatTraceFields(state, latest),
					zap.Int("event", eventCount),
					zap.Duration("turn_elapsed", eventReceivedAt.Sub(turnStartedAt)),
				)...,
			)
			continue
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		outputEventCount++
		if firstOutputAt.IsZero() {
			firstOutputAt = eventReceivedAt
			m.logger.Debug("received first agent output event",
				append(chatTraceFields(state, latest),
					zap.Duration("first_output_latency", durationSince(turnStartedAt, firstOutputAt)),
					zap.Duration("request_elapsed", durationSince(requestStartedAt, firstOutputAt)),
				)...,
			)
		}
		variant := event.Output.MessageOutput
		outputStartedAt := time.Now()
		variantChunks := 0
		variantCharacters := 0
		m.logger.Debug("consuming agent message output",
			append(chatTraceFields(state, latest),
				zap.Int("output_event", outputEventCount),
				zap.String("role", string(variant.Role)),
				zap.String("tool_name", variant.ToolName),
				zap.Bool("streaming", variant.IsStreaming),
				zap.Duration("event_wait_duration", eventReceivedAt.Sub(waitStartedAt)),
				zap.Duration("turn_elapsed", eventReceivedAt.Sub(turnStartedAt)),
			)...,
		)
		message, err := consumeMessageVariant(variant, func(chunk *schema.Message) error {
			chunkAt := time.Now()
			variantChunks++
			chunkCount++
			isAssistant := variant.Role == schema.Assistant || chunk.Role == schema.Assistant
			if !isAssistant {
				return nil
			}
			if firstChunkAt.IsZero() {
				firstChunkAt = chunkAt
				m.logger.Debug("received first assistant chunk",
					append(chatTraceFields(state, latest),
						zap.Duration("first_chunk_latency", durationSince(turnStartedAt, firstChunkAt)),
						zap.Duration("request_elapsed", durationSince(requestStartedAt, firstChunkAt)),
					)...,
				)
			}
			text := msgops.AssistantDeltaText(chunk)
			characters := len([]rune(text))
			variantCharacters += characters
			contentCharacters += characters
			if text != "" && firstTokenAt.IsZero() {
				firstTokenAt = chunkAt
				m.logger.Info("received first assistant text token",
					append(chatTraceFields(state, latest),
						zap.Duration("first_token_latency", durationSince(turnStartedAt, firstTokenAt)),
						zap.Duration("request_elapsed", durationSince(requestStartedAt, firstTokenAt)),
					)...,
				)
			}
			if err := streamWriter.Write(text); err != nil {
				return err
			}
			if len(msgops.ToolCalls(chunk)) > 0 {
				return streamWriter.Seal()
			}
			return nil
		})
		outputDuration := time.Since(outputStartedAt)
		streamReceiveDuration += outputDuration
		if message != nil {
			if message.Role == "" {
				message.Role = variant.Role
			}
			messageToolCalls := len(msgops.ToolCalls(message))
			toolCallCount += messageToolCalls
			m.logger.Debug("consumed agent message output",
				append(chatTraceFields(state, latest),
					zap.Int("output_event", outputEventCount),
					zap.String("role", string(message.Role)),
					zap.String("tool_name", message.ToolName),
					zap.Bool("streaming", variant.IsStreaming),
					zap.Int("chunks", variantChunks),
					zap.Int("content_characters", len([]rune(message.Content))),
					zap.Int("streamed_content_characters", variantCharacters),
					zap.Int("reasoning_characters", len([]rune(message.ReasoningContent))),
					zap.Int("tool_calls", messageToolCalls),
					zap.Duration("stream_duration", outputDuration),
					zap.Duration("turn_elapsed", time.Since(turnStartedAt)),
				)...,
			)
			outputs = append(outputs, message)
		}
		if err != nil {
			m.logger.Debug("failed while consuming agent message output",
				append(chatTraceFields(state, latest),
					zap.Int("output_event", outputEventCount),
					zap.Bool("streaming", variant.IsStreaming),
					zap.Int("chunks", variantChunks),
					zap.Duration("stream_duration", outputDuration),
					zap.Error(err),
				)...,
			)
			if turnErr == nil {
				turnErr = err
			}
			break
		}
	}

	stopped := false
	select {
	case <-turn.Stopped:
		stopped = true
	default:
	}
	if stopped {
		preemptedByMessage := state.isImmediatePreempt(loop)
		if preemptedByMessage {
			outcome = "preempted"
		} else {
			outcome = "stopped"
		}
		streamWriter.Discard()
		persistStartedAt := time.Now()
		persistErr := m.persistStoppedOutput(state, latest.RoundID, outputs)
		persistenceDuration += time.Since(persistStartedAt)
		if persistErr != nil {
			persistErr = errors.Join(errs.ErrInterruptedOutputPersistence, persistErr)
			outcome = "stop_persistence_error"
			completeRequests(turn.Consumed, persistErr)
			return persistErr
		}
		if preemptedByMessage {
			completeRequests(turn.Consumed, errs.ErrTurnPreempted)
		} else {
			completeRequests(turn.Consumed, errs.ErrStateStopped)
		}
		return nil
	}
	preempted := false
	select {
	case <-turn.Preempted:
		preempted = true
	default:
	}
	var cancelErr *adk.CancelError
	if errors.As(turnErr, &cancelErr) && state.isImmediatePreempt(loop) {
		preempted = true
	}
	if preempted {
		outcome = "preempted"
		streamWriter.Discard()
		persisted := outputs
		persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), m.cfg.ModelTimeout)
		persistStartedAt := time.Now()
		state.roundMu.Lock()
		err := m.sessions.AppendInterrupted(
			persistCtx,
			state.UserID,
			state.CharacterID,
			latest.RoundID,
			persisted...,
		)
		state.roundMu.Unlock()
		cancelPersist()
		persistenceDuration += time.Since(persistStartedAt)
		if err != nil {
			err = errors.Join(errs.ErrInterruptedOutputPersistence, err)
			outcome = "preempt_persistence_error"
			completeRequests(turn.Consumed, err)
			return err
		}
		completeRequests(turn.Consumed, errs.ErrTurnPreempted)
		m.logger.Debug("chat turn preempted",
			append(chatTraceFields(state, latest),
				zap.Int("persisted_messages", len(persisted)),
				zap.Int("delivered_blocks", sentBlocks),
				zap.Duration("persistence_duration", persistenceDuration),
				zap.Duration("turn_elapsed", time.Since(turnStartedAt)),
			)...,
		)
		return nil
	}
	if turnErr != nil {
		outcome = "agent_error"
		streamWriter.Discard()
		completeRequests(turn.Consumed, turnErr)
		return turnErr
	}
	if silentExit {
		streamWriter.Discard()
	} else {
		if err := streamWriter.Flush(); err != nil {
			outcome = "telegram_send_error"
			completeRequests(turn.Consumed, err)
			return err
		}
	}

	persisted := outputs
	var err error
	compressionCtx, cancelCompression := context.WithTimeout(context.WithoutCancel(ctx), m.cfg.ModelTimeout)
	stopCompression := context.AfterFunc(m.ctx, cancelCompression)
	defer func() {
		stopCompression()
		cancelCompression()
	}()
	var memoryMessage *schema.Message
	if m.memories != nil {
		memoryMessage, err = m.memories.Render(compressionCtx, state.UserID)
		if err != nil {
			m.logger.Warn("failed to render memory block for session compression",
				append(chatTraceFields(state, latest), zap.Error(err))...,
			)
			memoryMessage = nil
		}
	}
	completionStartedAt := time.Now()
	state.roundMu.Lock()
	if state.activeRoundID == latest.RoundID && state.roundRevision > latest.Revision {
		outcome = "superseded"
		currentRevision := state.roundRevision
		err = m.sessions.AppendInterrupted(
			compressionCtx,
			state.UserID,
			state.CharacterID,
			latest.RoundID,
			persisted...,
		)
		state.roundMu.Unlock()
		completionDuration += time.Since(completionStartedAt)
		persistenceDuration += completionDuration
		if err != nil {
			err = errors.Join(errs.ErrInterruptedOutputPersistence, err)
			outcome = "superseded_persistence_error"
			completeRequests(turn.Consumed, err)
			return err
		}
		completeRequests(turn.Consumed, errs.ErrTurnPreempted)
		m.logger.Debug("chat turn superseded before completion",
			append(chatTraceFields(state, latest),
				zap.Uint64("completed_revision", latest.Revision),
				zap.Uint64("current_revision", currentRevision),
				zap.Int("persisted_messages", len(persisted)),
				zap.Duration("persistence_duration", persistenceDuration),
				zap.Duration("turn_elapsed", time.Since(turnStartedAt)),
			)...,
		)
		return nil
	}
	err = m.sessions.CompleteRound(compressionCtx, state.UserID, state.CharacterID, latest.RoundID, session.CompressionOptions{
		MaxRounds: state.MaxRounds,
		Agent:     state.agent(),
		Memory:    memoryMessage,
	}, persisted...)
	if state.activeRoundID == latest.RoundID {
		state.activeRoundID = 0
		state.roundRevision = 0
	}
	state.roundMu.Unlock()
	completionDuration += time.Since(completionStartedAt)
	if err != nil {
		outcome = "round_completion_error"
		completeRequests(turn.Consumed, err)
		return err
	}

	completeRequests(turn.Consumed, nil)
	if silentExit {
		outcome = "silent_exit"
		m.logger.Info("chat turn exited without a reply",
			append(chatTraceFields(state, latest),
				zap.Duration("round_completion_duration", completionDuration),
				zap.Duration("turn_elapsed", time.Since(turnStartedAt)),
			)...,
		)
		return nil
	}
	outcome = "completed"
	m.logger.Info("completed chat turn",
		append(chatTraceFields(state, latest),
			zap.Int("output_messages", len(outputs)),
			zap.Int("sent_blocks", sentBlocks),
			zap.Duration("round_completion_duration", completionDuration),
			zap.Duration("turn_elapsed", time.Since(turnStartedAt)),
		)...,
	)
	return nil
}

func (m *Manager) persistStoppedOutput(state *UserState, roundID uint, messages []*schema.Message) error {
	if len(messages) == 0 {
		return nil
	}
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(m.ctx), m.cfg.ModelTimeout)
	defer cancel()
	if err := m.sessions.AppendInterrupted(ctx, state.UserID, state.CharacterID, roundID, messages...); err != nil {
		m.logger.Warn("failed to persist stopped chat output",
			zap.Int64("user_id", state.UserID),
			zap.String("character_id", state.CharacterID),
			zap.Uint("round_id", roundID),
			zap.Int("messages", len(messages)),
			zap.Duration("persistence_duration", time.Since(startedAt)),
			zap.Error(err),
		)
		return err
	}
	m.logger.Debug("persisted stopped chat output",
		zap.Int64("user_id", state.UserID),
		zap.String("character_id", state.CharacterID),
		zap.Uint("round_id", roundID),
		zap.Int("messages", len(messages)),
		zap.Duration("persistence_duration", time.Since(startedAt)),
	)
	return nil
}

func (m *Manager) watchState(state *UserState, loop *adk.TurnLoop[*Request, *schema.Message]) {
	result := loop.Wait()
	preemptedByMessage := state.takeImmediatePreempt(loop)
	var interruptErr *adk.InterruptError
	if preemptedByMessage {
		m.logger.Debug("user turn loop exited after immediate preemption",
			zap.Int64("user_id", state.UserID),
		)
	} else if errors.As(result.ExitReason, &interruptErr) {
		m.logger.Info("user turn loop exited after model silent exit",
			zap.Int64("user_id", state.UserID),
		)
	} else if result.ExitReason != nil {
		m.logger.Warn("user turn loop exited",
			zap.Int64("user_id", state.UserID),
			zap.Error(result.ExitReason),
		)
	}
	completionErr := errs.ErrStateStopped
	if preemptedByMessage {
		completionErr = errs.ErrTurnPreempted
	}
	for _, request := range result.UnhandledItems {
		request.complete(completionErr)
	}
	for _, request := range result.InterruptedItems {
		request.complete(completionErr)
	}
	if _, claimed := m.claimState(state, loop); !claimed {
		return
	}
	state.closeMCP()
}

func completeRequests(requests []*Request, err error) {
	for _, request := range requests {
		request.complete(err)
	}
}

func chatTraceFields(state *UserState, request *Request) []zap.Field {
	fields := []zap.Field{
		zap.String("trace_id", fmt.Sprintf("%d:%s:%d:%d", state.UserID, state.CharacterID, request.RoundID, request.Revision)),
		zap.Int64("user_id", state.UserID),
		zap.String("character_id", state.CharacterID),
		zap.Uint("round_id", request.RoundID),
		zap.Uint64("revision", request.Revision),
	}
	if request.MessageID != 0 {
		fields = append(fields, zap.Int("message_id", request.MessageID))
	}
	return fields
}

func durationSince(startedAt, endedAt time.Time) time.Duration {
	if startedAt.IsZero() || endedAt.IsZero() || endedAt.Before(startedAt) {
		return 0
	}
	return endedAt.Sub(startedAt)
}

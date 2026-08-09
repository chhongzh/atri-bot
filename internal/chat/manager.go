package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/chhongzh/atri-bot/internal/account"
	"github.com/chhongzh/atri-bot/internal/character"
	mcpmanager "github.com/chhongzh/atri-bot/internal/mcp"
	"github.com/chhongzh/atri-bot/internal/msgops"
	"github.com/chhongzh/atri-bot/internal/session"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/chhongzh/atri-bot/internal/utils"
	openai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
	"gorm.io/gorm"
)

var (
	ErrNoCharacters       = errors.New("no characters are available")
	ErrAIConfigIncomplete = errors.New("user AI config is incomplete")
	ErrTurnPreempted      = errors.New("turn preempted by a newer message")
	ErrStateStopped       = errors.New("user turn loop has stopped")
)

type Config struct {
	StateTTL               time.Duration
	ModelTimeout           time.Duration
	DefaultToolPermissions map[string]bool
	SendSystemResult       func(telebot.Context, string) error
}

type Manager struct {
	logger     *zap.Logger
	db         *gorm.DB
	accounts   *account.Manager
	characters *character.Manager
	sessions   *session.Manager
	tools      *toolmanager.Manager
	mcp        *mcpmanager.Manager
	cfg        Config

	defaultToolPermissions map[string]bool

	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	states map[int64]*UserState
}

func New(
	ctx context.Context,
	logger *zap.Logger,
	db *gorm.DB,
	accounts *account.Manager,
	characters *character.Manager,
	sessions *session.Manager,
	tools *toolmanager.Manager,
	mcpManager *mcpmanager.Manager,
	cfg Config,
) *Manager {
	if cfg.StateTTL <= 0 {
		cfg.StateTTL = 30 * time.Minute
	}
	if cfg.ModelTimeout <= 0 {
		cfg.ModelTimeout = 2 * time.Minute
	}
	managerCtx, cancel := context.WithCancel(ctx)
	manager := &Manager{
		logger:                 logger,
		db:                     db,
		accounts:               accounts,
		characters:             characters,
		sessions:               sessions,
		tools:                  tools,
		mcp:                    mcpManager,
		cfg:                    cfg,
		defaultToolPermissions: normalizeDefaultToolPermissions(logger, tools, cfg.DefaultToolPermissions),
		ctx:                    managerCtx,
		cancel:                 cancel,
		states:                 make(map[int64]*UserState),
	}
	if mcpManager != nil {
		mcpManager.SetOnChange(manager.markStateStale)
	}
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

func (m *Manager) Chat(ctx context.Context, c telebot.Context, text string) error {
	sender := c.Sender()
	if sender == nil {
		return errors.New("telegram sender is missing")
	}

	request := newRequest(c, text)
	for attempt := 0; attempt < 2; attempt++ {
		state, err := m.state(ctx, sender.ID, c)
		if err != nil {
			return err
		}
		accepted, _ := state.TurnLoop.Push(
			request,
			adk.WithPreempt[*Request, *schema.Message](adk.AnySafePoint),
		)
		if accepted {
			select {
			case err = <-request.done:
				if errors.Is(err, ErrTurnPreempted) {
					return nil
				}
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		m.Invalidate(sender.ID)
	}
	return ErrStateStopped
}

func (m *Manager) Invalidate(userID int64) {
	m.mu.Lock()
	state := m.states[userID]
	m.mu.Unlock()
	if state != nil {
		state.TurnLoop.Stop(
			adk.WithGracefulTimeout(5*time.Second),
			adk.WithSkipCheckpoint(),
			adk.WithStopCause("state invalidated"),
		)
		state.TurnLoop.Wait()
		state.closeMCP()
		m.mu.Lock()
		if m.states[userID] == state {
			delete(m.states, userID)
		}
		m.mu.Unlock()
	}
}

func (m *Manager) InvalidateAll() {
	m.mu.Lock()
	states := make([]*UserState, 0, len(m.states))
	for _, state := range m.states {
		states = append(states, state)
	}
	m.mu.Unlock()
	for _, state := range states {
		state.TurnLoop.Stop(
			adk.WithGracefulTimeout(5*time.Second),
			adk.WithSkipCheckpoint(),
			adk.WithStopCause("all states invalidated"),
		)
	}
	for _, state := range states {
		state.TurnLoop.Wait()
		state.closeMCP()
	}
	m.mu.Lock()
	for _, state := range states {
		if m.states[state.UserID] == state {
			delete(m.states, state.UserID)
		}
	}
	m.mu.Unlock()
}

func (m *Manager) Shutdown() {
	m.cancel()
	m.mu.Lock()
	states := make([]*UserState, 0, len(m.states))
	for _, state := range m.states {
		states = append(states, state)
	}
	m.mu.Unlock()
	for _, state := range states {
		state.TurnLoop.Stop(adk.WithImmediate(), adk.WithSkipCheckpoint(), adk.WithStopCause("shutdown"))
	}
	for _, state := range states {
		state.TurnLoop.Wait()
	}
	for _, state := range states {
		state.closeMCP()
	}
	m.mu.Lock()
	m.states = make(map[int64]*UserState)
	m.mu.Unlock()
}

func (m *Manager) state(ctx context.Context, userID int64, c telebot.Context) (*UserState, error) {
	m.mu.Lock()
	if state := m.states[userID]; state != nil {
		if !state.isStale() {
			state.TelebotContext = c
			m.mu.Unlock()
			return state, nil
		}
		m.mu.Unlock()
		m.Invalidate(userID)
	} else {
		m.mu.Unlock()
	}

	state, err := m.newState(ctx, userID, c)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if existing := m.states[userID]; existing != nil {
		m.mu.Unlock()
		state.TurnLoop.Stop(adk.WithImmediate(), adk.WithSkipCheckpoint())
		state.closeMCP()
		return existing, nil
	}
	m.states[userID] = state
	m.mu.Unlock()

	state.TurnLoop.Run(m.ctx)
	state.TurnLoop.Stop(
		adk.UntilIdleFor(m.cfg.StateTTL),
		adk.WithSkipCheckpoint(),
		adk.WithStopCause("state expired"),
	)
	go m.watchState(state)
	return state, nil
}

func (m *Manager) newState(ctx context.Context, userID int64, c telebot.Context) (*UserState, error) {
	startedAt := time.Now()
	if m.cfg.SendSystemResult != nil {
		if err := m.cfg.SendSystemResult(c, "正在加载聊天状态，请稍候。"); err != nil {
			m.logger.Warn("failed to send chat state loading message",
				zap.Int64("user_id", userID),
				zap.Error(err),
			)
		}
	}

	user, err := m.accounts.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	characterID := user.CharacterID
	if characterID == "" {
		defaultCharacter, ok := m.characters.Default()
		if !ok {
			return nil, ErrNoCharacters
		}
		characterID = defaultCharacter.ID
		if err = m.accounts.SetCharacter(ctx, userID, characterID); err != nil {
			return nil, err
		}
	} else if _, ok := m.characters.Get(characterID); !ok {
		return nil, fmt.Errorf("selected character %q is unavailable", characterID)
	}

	missing := make([]string, 0, 3)
	if strings.TrimSpace(user.AIBaseURL) == "" {
		missing = append(missing, "base-url")
	}
	if strings.TrimSpace(user.AIAPIKey) == "" {
		missing = append(missing, "key")
	}
	if strings.TrimSpace(user.AIModel) == "" {
		missing = append(missing, "model")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: missing %s; use /ai to configure", ErrAIConfigIncomplete, strings.Join(missing, ", "))
	}
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		BaseURL: strings.TrimSpace(user.AIBaseURL),
		APIKey:  strings.TrimSpace(user.AIAPIKey),
		Model:   strings.TrimSpace(user.AIModel),
		Timeout: m.cfg.ModelTimeout,
	})
	if err != nil {
		return nil, err
	}
	agent, err := m.buildAgent(ctx, chatModel, userID, nil)
	if err != nil {
		return nil, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	state := &UserState{
		UserID:         userID,
		CharacterID:    characterID,
		MaxRounds:      user.AIMaxRounds,
		Agent:          agent,
		Runner:         runner,
		TelebotContext: c,
		model:          chatModel,
	}
	state.TurnLoop = adk.NewTurnLoop(adk.TurnLoopConfig[*Request, *schema.Message]{
		GenInput: func(turnCtx context.Context, _ *adk.TurnLoop[*Request, *schema.Message], items []*Request) (*adk.GenInputResult[*Request, *schema.Message], error) {
			return m.genInput(turnCtx, state, items)
		},
		PrepareAgent: func(context.Context, *adk.TurnLoop[*Request, *schema.Message], []*Request) (adk.Agent, error) {
			return state.agent(), nil
		},
		OnAgentEvents: func(turnCtx context.Context, turn *adk.TurnContext[*Request, *schema.Message], events *adk.AsyncIterator[*adk.AgentEvent]) error {
			return m.onAgentEvents(turnCtx, state, turn, events)
		},
	})
	if m.mcp != nil {
		cancelLoad, loadErr := m.mcp.LoadAsync(userID, func(ctx context.Context) (bool, error) {
			return m.ToolAllowed(ctx, userID, "mcp")
		}, func(result *mcpmanager.LoadResult, loadErr error) {
			m.attachMCPTools(state, result, loadErr)
		})
		if loadErr != nil {
			m.logger.Warn("failed to enqueue mcp loading",
				zap.Int64("user_id", userID),
				zap.Error(loadErr),
			)
		} else {
			state.setMCPLoadCancel(cancelLoad)
		}
	}
	m.logger.Info("created user chat state",
		zap.Int64("user_id", userID),
		zap.String("character_id", characterID),
		zap.Duration("elapsed", time.Since(startedAt)),
	)
	return state, nil
}

func (m *Manager) buildAgent(
	ctx context.Context,
	model model.ChatModel,
	userID int64,
	mcpTools []tool.BaseTool,
) (*adk.ChatModelAgent, error) {
	static, err := m.allowedTools(ctx, userID)
	if err != nil {
		return nil, err
	}
	all := make([]tool.BaseTool, 0, len(static)+len(mcpTools))
	all = append(all, static...)
	all = append(all, mcpTools...)
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        fmt.Sprintf("atri_%d", userID),
		Description: "A Telegram character chat agent",
		Model:       model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: toolNodeConfig(all)},
		Handlers:    []adk.ChatModelAgentMiddleware{&safeToolMiddleware{}},
	})
}

func (m *Manager) attachMCPTools(state *UserState, result *mcpmanager.LoadResult, loadErr error) {
	if loadErr != nil {
		if errors.Is(loadErr, context.Canceled) {
			return
		}
		m.logger.Warn("mcp loading failed",
			zap.Int64("user_id", state.UserID),
			zap.Error(loadErr),
		)
		return
	}
	if result == nil {
		return
	}
	if len(result.Tools) == 0 {
		result.Close()
		return
	}
	agent, err := m.buildAgent(context.Background(), state.model, state.UserID, result.Tools)
	if err != nil {
		result.Close()
		m.logger.Warn("failed to rebuild agent with mcp tools",
			zap.Int64("user_id", state.UserID),
			zap.Error(err),
		)
		return
	}
	if !state.attachMCP(agent, result.Tools, result.Close) {
		result.Close()
		return
	}
	m.logger.Info("attached mcp tools to chat state",
		zap.Int64("user_id", state.UserID),
		zap.Int("mcp_tools", len(result.Tools)),
	)
}

func (m *Manager) genInput(ctx context.Context, state *UserState, items []*Request) (*adk.GenInputResult[*Request, *schema.Message], error) {
	if len(items) == 0 {
		return nil, errors.New("turn loop received no chat requests")
	}
	latest := items[len(items)-1]
	sender := latest.Context.Sender()
	username := ""
	if sender != nil {
		username = firstNonEmpty(sender.Username, strings.TrimSpace(sender.FirstName+" "+sender.LastName))
	}
	systemPrompt, err := m.characters.RenderSystemPrompt(ctx, state.CharacterID, username, time.Now())
	if err != nil {
		return nil, err
	}
	history, err := m.sessions.Load(ctx, state.UserID, state.CharacterID, state.MaxRounds)
	if err != nil {
		return nil, err
	}
	messages := make([]*schema.Message, 0, len(history)+len(items)+1)
	messages = append(messages, schema.SystemMessage(systemPrompt))
	messages = append(messages, history...)
	for _, item := range items {
		messages = append(messages, schema.UserMessage(item.Text))
	}
	runCtx := toolmanager.WithRunningState(ctx, &toolmanager.RunningState{
		UserID:         state.UserID,
		CharacterID:    state.CharacterID,
		TelebotContext: latest.Context,
	})
	m.logger.Debug("prepared chat turn",
		zap.Int64("user_id", state.UserID),
		zap.String("character_id", state.CharacterID),
		zap.Int("history_messages", len(history)),
		zap.Int("consumed_requests", len(items)),
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
	turn *adk.TurnContext[*Request, *schema.Message],
	events *adk.AsyncIterator[*adk.AgentEvent],
) error {
	var (
		outputs []*schema.Message
		turnErr error
	)
	latest := turn.Consumed[len(turn.Consumed)-1]
	streamWriter := newAssistantStreamWriter(func(text string) error {
		return utils.SendTelegramText(latest.Context, text)
	})
	for {
		event, ok := events.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			if turnErr == nil {
				turnErr = event.Err
			}
			continue
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		variant := event.Output.MessageOutput
		message, err := consumeMessageVariant(variant, func(chunk *schema.Message) error {
			if variant.Role != schema.Assistant && chunk.Role != schema.Assistant {
				return nil
			}
			if err := streamWriter.Write(msgops.AssistantDeltaText(chunk)); err != nil {
				return err
			}
			calls := msgops.ToolCalls(chunk)
			if len(calls) > 0 {
				if err := streamWriter.Flush(); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			completeRequests(turn.Consumed, err)
			return err
		}
		if message == nil {
			continue
		}
		if message.Role == "" {
			message.Role = variant.Role
		}
		outputs = append(outputs, message)
	}

	stopped := false
	select {
	case <-turn.Stopped:
		stopped = true
	default:
	}
	if stopped {
		completeRequests(turn.Consumed, ErrStateStopped)
		return nil
	}
	preempted := false
	select {
	case <-turn.Preempted:
		preempted = true
	default:
	}
	var cancelErr *adk.CancelError
	if errors.As(turnErr, &cancelErr) {
		preempted = true
	}
	if preempted {
		completeRequests(turn.Consumed, ErrTurnPreempted)
		m.logger.Debug("chat turn preempted", zap.Int64("user_id", state.UserID))
		return nil
	}
	if turnErr != nil {
		completeRequests(turn.Consumed, turnErr)
		return turnErr
	}
	if err := streamWriter.Flush(); err != nil {
		completeRequests(turn.Consumed, err)
		return err
	}

	persisted := make([]*schema.Message, 0, len(turn.Consumed)+len(outputs))
	for _, item := range turn.Consumed {
		persisted = append(persisted, schema.UserMessage(item.Text))
	}
	persisted = append(persisted, outputs...)
	if err := m.sessions.Append(ctx, state.UserID, state.CharacterID, state.MaxRounds, persisted...); err != nil {
		completeRequests(turn.Consumed, err)
		return err
	}

	completeRequests(turn.Consumed, nil)
	m.logger.Info("completed chat turn",
		zap.Int64("user_id", state.UserID),
		zap.String("character_id", state.CharacterID),
		zap.Int("output_messages", len(outputs)),
	)
	return nil
}

func consumeMessageVariant(
	variant *adk.MessageVariant,
	handleChunk func(*schema.Message) error,
) (*schema.Message, error) {
	if variant == nil {
		return nil, nil
	}
	if !variant.IsStreaming {
		message, err := variant.GetMessage()
		if err != nil || message == nil {
			return message, err
		}
		if err := handleChunk(message); err != nil {
			return nil, err
		}
		return message, nil
	}
	if variant.MessageStream == nil {
		return nil, errors.New("streaming message variant has no stream")
	}
	defer variant.MessageStream.Close()

	chunks := make([]*schema.Message, 0)
	for {
		chunk, err := variant.MessageStream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if chunk == nil {
			continue
		}
		if err := handleChunk(chunk); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return nil, errors.New("streaming message variant contains no chunks")
	}
	return schema.ConcatMessages(chunks)
}

func (m *Manager) watchState(state *UserState) {
	result := state.TurnLoop.Wait()
	if result.ExitReason != nil {
		m.logger.Warn("user turn loop exited",
			zap.Int64("user_id", state.UserID),
			zap.Error(result.ExitReason),
		)
	}
	for _, request := range result.UnhandledItems {
		request.complete(ErrStateStopped)
	}
	for _, request := range result.InterruptedItems {
		request.complete(ErrStateStopped)
	}
	m.mu.Lock()
	if m.states[state.UserID] == state {
		delete(m.states, state.UserID)
	}
	m.mu.Unlock()
	state.closeMCP()
}

func completeRequests(requests []*Request, err error) {
	for _, request := range requests {
		request.complete(err)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

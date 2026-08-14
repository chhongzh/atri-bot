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
	errs "github.com/chhongzh/atri-bot/internal/errs"
	mcpmanager "github.com/chhongzh/atri-bot/internal/mcp"
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
	configs *configmanager.Manager,
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
		configs:                configs,
		characters:             characters,
		sessions:               sessions,
		tools:                  tools,
		mcp:                    mcpManager,
		cfg:                    cfg,
		defaultToolPermissions: make(map[string]bool),
		ctx:                    managerCtx,
		cancel:                 cancel,
		states:                 make(map[int64]*UserState),
	}
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

func (m *Manager) Chat(ctx context.Context, c telebot.Context, text string) error {
	sender := c.Sender()
	request := newRequest(c, text)
	for attempt := 0; attempt < 2; attempt++ {
		state, err := m.state(ctx, sender.ID, c)
		if err != nil {
			return err
		}
		if err = m.sessions.Wait(ctx, state.UserID, state.CharacterID); err != nil {
			return err
		}
		accepted, _ := state.TurnLoop.Push(
			request,
			adk.WithPreemptTimeout[*Request, *schema.Message](adk.AnySafePoint, 0),
		)
		if accepted {
			select {
			case err = <-request.done:
				if errors.Is(err, errs.ErrTurnPreempted) {
					return nil
				}
				return err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		m.Invalidate(sender.ID)
	}
	return errs.ErrStateStopped
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
		m.removeState(state)
	}
}

func (m *Manager) InvalidateAll() {
	states := m.snapshotStates()
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
	for _, state := range states {
		m.removeState(state)
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
	states := m.snapshotStates()
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
		defaultCharacter, ok := m.characters.Default()
		if !ok {
			return nil, errs.ErrNoCharacters
		}
		characterID = defaultCharacter.ID
		if err = m.accounts.SetCharacter(ctx, userID, characterID); err != nil {
			return nil, err
		}
	} else if _, ok := m.characters.Get(characterID); !ok {
		return nil, fmt.Errorf("selected character %q is unavailable", characterID)
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
		return nil, fmt.Errorf("%w: missing %s", errs.ErrAIConfigIncomplete, strings.Join(missing, ", "))
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
	agent, err := m.buildAgent(ctx, chatModel, userID, mcpTools)
	if err != nil {
		mcpResult.Close()
		return nil, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	state.Agent = agent
	state.Runner = runner
	state.mcpClose = mcpResult.Close
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
			return nil, fmt.Errorf("create MCP tool search: %w", searchErr)
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
	interruptedMessages := state.startTurnMessages()
	latest := items[len(items)-1]
	systemPrompt, err := m.renderSystemPrompt(ctx, state, latest.Context)
	if err != nil {
		return nil, err
	}
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
	history, err := m.sessions.Load(sessionCtx, state.UserID, state.CharacterID, session.CompressionOptions{
		MaxRounds:    state.MaxRounds,
		SystemPrompt: systemPrompt,
		Agent:        state.agent(),
	})
	if err != nil {
		return nil, err
	}
	messages := make([]*schema.Message, 0, len(history)+len(interruptedMessages)+len(items)+1)
	messages = append(messages, schema.SystemMessage(systemPrompt))
	messages = append(messages, history...)
	messages = append(messages, interruptedMessages...)
	for _, item := range items {
		message, renderErr := m.characters.RenderUserMessage(ctx, item.Text, time.Now())
		if renderErr != nil {
			return nil, renderErr
		}
		messages = append(messages, message)
	}
	m.logger.Debug("prepared chat turn",
		zap.Int64("user_id", state.UserID),
		zap.String("character_id", state.CharacterID),
		zap.Int("history_messages", len(history)),
		zap.Int("resumed_messages", len(interruptedMessages)),
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
		outputs    []*schema.Message
		turnErr    error
		silentExit bool
	)
	latest := turn.Consumed[len(turn.Consumed)-1]
	sentBlocks := 0
	var deliveredBlocks []string
	streamWriter := utils.NewAssistantStreamWriter(func(text string) error {
		if err := utils.SendTelegramText(latest.Context, text); err != nil {
			return err
		}
		sentBlocks++
		deliveredBlocks = append(deliveredBlocks, text)
		m.logger.Debug("sent assistant stream block",
			zap.Int64("user_id", state.UserID),
			zap.String("character_id", state.CharacterID),
			zap.Int("block", sentBlocks),
			zap.Int("characters", len([]rune(text))),
		)
		m.cfg.OnMessageSent(latest.Context)
		return nil
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
		if event.Action != nil && event.Action.Interrupted != nil {
			silentExit = true
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
			if len(msgops.ToolCalls(chunk)) > 0 {
				return streamWriter.Seal()
			}
			return nil
		})
		if err != nil {
			if turnErr == nil {
				turnErr = err
			}
			break
		}
		if message.Role == "" {
			message.Role = variant.Role
		}
		if message.Role == schema.Assistant {
			m.logger.Debug("consumed assistant output",
				zap.Int64("user_id", state.UserID),
				zap.String("character_id", state.CharacterID),
				zap.Bool("streaming", variant.IsStreaming),
				zap.Int("content_characters", len([]rune(msgops.AssistantText(message)))),
				zap.Int("reasoning_characters", len([]rune(message.ReasoningContent))),
				zap.Int("tool_calls", len(msgops.ToolCalls(message))),
			)
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
		streamWriter.Discard()
		state.finishTurnMessages()
		completeRequests(turn.Consumed, errs.ErrStateStopped)
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
		streamWriter.Discard()
		interruptedMessages := state.finishTurnMessages()
		interruptedMessages = append(interruptedMessages, requestMessages(turn.Consumed)...)
		interruptedMessages = append(interruptedMessages, assistantMessages(deliveredBlocks)...)
		state.requeueMessages(interruptedMessages)
		completeRequests(turn.Consumed, errs.ErrTurnPreempted)
		m.logger.Debug("chat turn preempted",
			zap.Int64("user_id", state.UserID),
			zap.Int("queued_messages", len(interruptedMessages)),
			zap.Int("delivered_blocks", len(deliveredBlocks)),
		)
		return nil
	}
	if silentExit {
		streamWriter.Discard()
		state.finishTurnMessages()
		completeRequests(turn.Consumed, nil)
		m.logger.Info("chat turn exited without a reply",
			zap.Int64("user_id", state.UserID),
			zap.String("character_id", state.CharacterID),
		)
		return nil
	}
	if turnErr != nil {
		streamWriter.Discard()
		state.finishTurnMessages()
		completeRequests(turn.Consumed, turnErr)
		return turnErr
	}
	if err := streamWriter.Flush(); err != nil {
		state.finishTurnMessages()
		completeRequests(turn.Consumed, err)
		return err
	}

	interruptedMessages := state.finishTurnMessages()
	persisted := make([]*schema.Message, 0, len(interruptedMessages)+len(turn.Consumed)+len(outputs))
	persisted = append(persisted, interruptedMessages...)
	persisted = append(persisted, requestMessages(turn.Consumed)...)
	persisted = append(persisted, persistentAgentOutputs(outputs)...)
	systemPrompt, err := m.renderSystemPrompt(ctx, state, latest.Context)
	if err != nil {
		completeRequests(turn.Consumed, err)
		return err
	}
	compressionCtx, cancelCompression := context.WithTimeout(context.WithoutCancel(ctx), m.cfg.ModelTimeout)
	stopCompression := context.AfterFunc(m.ctx, cancelCompression)
	defer func() {
		stopCompression()
		cancelCompression()
	}()
	if err = m.sessions.AppendRound(compressionCtx, state.UserID, state.CharacterID, session.CompressionOptions{
		MaxRounds:    state.MaxRounds,
		SystemPrompt: systemPrompt,
		Agent:        state.agent(),
	}, persisted...); err != nil {
		completeRequests(turn.Consumed, err)
		return err
	}

	completeRequests(turn.Consumed, nil)
	m.logger.Info("completed chat turn",
		zap.Int64("user_id", state.UserID),
		zap.String("character_id", state.CharacterID),
		zap.Int("output_messages", len(outputs)),
		zap.Int("sent_blocks", sentBlocks),
	)
	return nil
}

func persistentAgentOutputs(outputs []*schema.Message) []*schema.Message {
	const toolSearchName = "tool_search"

	searchCallIDs := make(map[string]struct{})
	persisted := make([]*schema.Message, 0, len(outputs))
	for _, message := range outputs {
		if message == nil {
			continue
		}
		switch message.Role {
		case schema.Assistant:
			calls := make([]schema.ToolCall, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				if call.Function.Name == toolSearchName {
					searchCallIDs[call.ID] = struct{}{}
					continue
				}
				calls = append(calls, call)
			}
			if len(calls) == 0 && len(message.ToolCalls) > 0 && strings.TrimSpace(message.Content) == "" {
				continue
			}
			if len(calls) != len(message.ToolCalls) {
				copy := *message
				copy.ToolCalls = calls
				message = &copy
			}
		case schema.Tool:
			_, searched := searchCallIDs[message.ToolCallID]
			if searched || message.ToolName == toolSearchName {
				continue
			}
		}
		persisted = append(persisted, message)
	}
	return persisted
}

func (m *Manager) watchState(state *UserState) {
	result := state.TurnLoop.Wait()
	var interruptErr *adk.InterruptError
	if errors.As(result.ExitReason, &interruptErr) {
		m.logger.Info("user turn loop exited after model silent exit",
			zap.Int64("user_id", state.UserID),
		)
	} else if result.ExitReason != nil {
		m.logger.Warn("user turn loop exited",
			zap.Int64("user_id", state.UserID),
			zap.Error(result.ExitReason),
		)
	}
	for _, request := range result.UnhandledItems {
		request.complete(errs.ErrStateStopped)
	}
	for _, request := range result.InterruptedItems {
		request.complete(errs.ErrStateStopped)
	}
	m.removeState(state)
	state.closeMCP()
}

func completeRequests(requests []*Request, err error) {
	for _, request := range requests {
		request.complete(err)
	}
}

func requestMessages(requests []*Request) []*schema.Message {
	messages := make([]*schema.Message, 0, len(requests))
	for _, request := range requests {
		messages = append(messages, schema.UserMessage(request.Text))
	}
	return messages
}

func assistantMessages(blocks []string) []*schema.Message {
	messages := make([]*schema.Message, 0, len(blocks))
	for _, block := range blocks {
		messages = append(messages, schema.AssistantMessage(block, nil))
	}
	return messages
}

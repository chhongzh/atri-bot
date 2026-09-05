package chat

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/chhongzh/atri-bot/internal/errs"
	mcpmanager "github.com/chhongzh/atri-bot/internal/mcp"
	"github.com/chhongzh/atri-bot/internal/security"
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
)

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
	username := sender.Username
	if username == "" {
		username = utils.FormatTelegramUsername(sender)
	}

	return m.characters.RenderSystemPrompt(ctx, state.CharacterID, username)
}

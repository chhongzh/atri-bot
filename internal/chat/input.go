package chat

import (
	"context"
	"time"

	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/session"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

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

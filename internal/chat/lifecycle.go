package chat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

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
	if result.ExitReason != nil {
		completionErr = errors.Join(completionErr, result.ExitReason)
	}
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

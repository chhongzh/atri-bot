package chat

import (
	"context"
	"errors"
	"time"

	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/msgops"
	"github.com/chhongzh/atri-bot/internal/session"
	"github.com/chhongzh/atri-bot/internal/utils"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

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

	streamWriter := newAssistantStreamWriter(func(text string) error {
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

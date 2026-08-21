// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/chhongzh/atri-bot/internal/constants"
	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/chhongzh/atri-bot/internal/msgops"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
	pkgErrors "github.com/pkg/errors"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxPersistedToolResultRunes = 2_000
	maxPersistedToolRoundRunes  = 8_000
	toolResultTailRunes         = 400
)

//go:embed compression.j2
var compressionTemplate string

//go:embed message_meta.j2
var messageMetadataTemplate string

type CompressionOptions struct {
	MaxRounds int
	Agent     adk.Agent
	Memory    *schema.Message
}

type sessionKey struct {
	userID      int64
	characterID string
}

type sessionLock struct {
	gate chan struct{}
	refs int
}

type historyWindow struct {
	summary  *model.SessionSummary
	rounds   []model.SessionRound
	messages []*schema.Message
}

func (w *historyWindow) cutoffRoundID() uint {
	if w.summary == nil {
		return 0
	}
	return w.summary.CutoffRoundID
}

func (w *historyWindow) lastRoundID() uint {
	return w.rounds[len(w.rounds)-1].ID
}

type Manager struct {
	db              *gorm.DB
	logger          *zap.Logger
	compression     *prompt.DefaultChatTemplate
	messageMetadata *prompt.DefaultChatTemplate

	locksMu sync.Mutex
	locks   map[sessionKey]*sessionLock
}

func New(db *gorm.DB, logger *zap.Logger) *Manager {
	return &Manager{
		db:              db,
		logger:          logger,
		compression:     prompt.FromMessages(schema.Jinja2, schema.UserMessage(compressionTemplate)),
		messageMetadata: prompt.FromMessages(schema.Jinja2, schema.SystemMessage(messageMetadataTemplate)),
		locks:           make(map[sessionKey]*sessionLock),
	}
}

func (m *Manager) Init() error {
	return m.db.AutoMigrate(&model.SessionRound{}, &model.SessionMessage{}, &model.SessionSummary{})
}

func (m *Manager) StartRound(
	ctx context.Context,
	userID int64,
	characterID string,
	message *schema.Message,
) (uint, error) {
	release, err := m.lock(ctx, userID, characterID, "start_round")
	if err != nil {
		return 0, err
	}
	defer release()

	sentAt := time.Now()
	metadata, err := m.renderMessageMetadata(ctx, sentAt, false)
	if err != nil {
		return 0, err
	}
	round := model.SessionRound{UserID: userID, CharacterID: characterID}
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if createErr := tx.Create(&round).Error; createErr != nil {
			return createErr
		}
		metadataRecord, createErr := makeSessionMessage(userID, characterID, round.ID, false, metadata)
		if createErr != nil {
			return createErr
		}
		metadataRecord.MessageMetadata = true
		metadataRecord.CreatedAt = sentAt
		messageRecord, createErr := makeSessionMessage(userID, characterID, round.ID, false, message)
		if createErr != nil {
			return createErr
		}
		messageRecord.CreatedAt = sentAt
		return tx.Create(&[]model.SessionMessage{metadataRecord, messageRecord}).Error
	})
	if err != nil {
		return 0, err
	}
	m.logger.Debug("started session round",
		zap.Int64("user_id", userID),
		zap.String("character_id", characterID),
		zap.Uint("round_id", round.ID),
	)
	return round.ID, nil
}

func (m *Manager) AppendUser(
	ctx context.Context,
	userID int64,
	characterID string,
	roundID uint,
	message *schema.Message,
) error {
	release, err := m.lock(ctx, userID, characterID, "append_user")
	if err != nil {
		return err
	}
	defer release()

	sentAt := time.Now()
	metadata, err := m.renderMessageMetadata(ctx, sentAt, true)
	if err != nil {
		return err
	}
	metadataRecord, err := makeSessionMessage(userID, characterID, roundID, false, metadata)
	if err != nil {
		return err
	}
	messageRecord, err := makeSessionMessage(userID, characterID, roundID, false, message)
	if err != nil {
		return err
	}
	metadataRecord.MessageMetadata = true
	metadataRecord.CreatedAt = sentAt
	messageRecord.CreatedAt = sentAt
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		round, findErr := m.findRoundForUpdate(tx, userID, characterID, roundID)
		if findErr != nil {
			return findErr
		}
		if createErr := tx.Create(&[]model.SessionMessage{metadataRecord, messageRecord}).Error; createErr != nil {
			return createErr
		}
		if !round.Interrupted {
			return tx.Model(round).Update("interrupted", true).Error
		}
		return nil
	})
}

func (m *Manager) AppendInterrupted(
	ctx context.Context,
	userID int64,
	characterID string,
	roundID uint,
	messages ...*schema.Message,
) error {
	release, err := m.lock(ctx, userID, characterID, "append_interrupted")
	if err != nil {
		return err
	}
	defer release()
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if appendErr := m.appendMessagesDB(tx, userID, characterID, roundID, true, messages...); appendErr != nil {
			return appendErr
		}
		return tx.Model(&model.SessionRound{}).
			Where("id = ? AND user_id = ? AND character_id = ?", roundID, userID, characterID).
			Update("interrupted", true).Error
	})
}

func (m *Manager) CompleteRound(
	ctx context.Context,
	userID int64,
	characterID string,
	roundID uint,
	opts CompressionOptions,
	messages ...*schema.Message,
) error {
	release, err := m.lock(ctx, userID, characterID, "complete_round")
	if err != nil {
		return err
	}
	defer release()
	if err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if appendErr := m.appendMessagesDB(tx, userID, characterID, roundID, false, messages...); appendErr != nil {
			return appendErr
		}
		return tx.Model(&model.SessionRound{}).
			Where("id = ? AND user_id = ? AND character_id = ?", roundID, userID, characterID).
			Update("completed", true).Error
	}); err != nil {
		return err
	}
	m.logger.Debug("completed session round",
		zap.Int64("user_id", userID),
		zap.String("character_id", characterID),
		zap.Uint("round_id", roundID),
		zap.Int("messages", len(messages)),
	)
	window, err := m.loadHistoryWindow(ctx, userID, characterID, 0)
	if err != nil {
		return err
	}
	_, err = m.maybeCompress(ctx, userID, characterID, "complete_round", normalizeMaxRounds(opts.MaxRounds), opts, window)
	return err
}

func (m *Manager) Load(ctx context.Context, userID int64, characterID string, currentRoundID uint, opts CompressionOptions) ([]*schema.Message, error) {
	release, err := m.lock(ctx, userID, characterID, "load")
	if err != nil {
		return nil, err
	}
	defer release()

	window, err := m.loadHistoryWindow(ctx, userID, characterID, currentRoundID)
	if err != nil {
		return nil, err
	}
	maxRounds := normalizeMaxRounds(opts.MaxRounds)
	m.logger.Debug("loaded session history",
		zap.Int64("user_id", userID),
		zap.String("character_id", characterID),
		zap.Int("rounds_after_cutoff", len(window.rounds)),
		zap.Int("max_rounds", maxRounds),
		zap.Int("messages", len(window.messages)),
		zap.Bool("has_summary", window.summary != nil),
		zap.Uint("cutoff_round_id", window.cutoffRoundID()),
	)
	history, err := m.maybeCompress(ctx, userID, characterID, "load", maxRounds, opts, window)
	if err != nil || currentRoundID == 0 {
		return history, err
	}
	current, err := m.loadRound(ctx, userID, characterID, currentRoundID)
	if err != nil {
		return nil, err
	}
	return append(history, current...), nil
}

func (m *Manager) findRoundForUpdate(
	db *gorm.DB,
	userID int64,
	characterID string,
	roundID uint,
) (*model.SessionRound, error) {
	var round model.SessionRound
	err := db.
		Select("id", "interrupted", "completed").
		Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where("id = ? AND user_id = ? AND character_id = ?", roundID, userID, characterID).
		Take(&round).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.SessionRoundNotFound(roundID)
	}
	return &round, err
}

func (m *Manager) appendMessagesDB(
	db *gorm.DB,
	userID int64,
	characterID string,
	roundID uint,
	interrupted bool,
	messages ...*schema.Message,
) error {
	if len(messages) == 0 {
		return nil
	}
	persisted, compactedMessages, removedRunes := compactToolResults(messages)
	records := make([]model.SessionMessage, 0, len(persisted))
	for _, message := range persisted {
		if message == nil {
			continue
		}
		record, err := makeSessionMessage(userID, characterID, roundID, interrupted, message)
		if err != nil {
			return err
		}
		records = append(records, record)
	}
	if len(records) > 0 {
		if err := db.Create(&records).Error; err != nil {
			return err
		}
	}
	if compactedMessages > 0 {
		m.logger.Info("compacted tool results for session history",
			zap.Int64("user_id", userID),
			zap.String("character_id", characterID),
			zap.Uint("round_id", roundID),
			zap.Int("tool_messages", compactedMessages),
			zap.Int("removed_characters", removedRunes),
		)
	}
	return nil
}

func (m *Manager) Wait(ctx context.Context, userID int64, characterID string) error {
	release, err := m.lock(ctx, userID, characterID, "new_message")
	if err != nil {
		return err
	}
	release()
	return nil
}

func (m *Manager) maybeCompress(
	ctx context.Context,
	userID int64,
	characterID string,
	trigger string,
	maxRounds int,
	opts CompressionOptions,
	window *historyWindow,
) ([]*schema.Message, error) {
	if window.summary == nil && len(window.rounds) < maxRounds {
		return window.messages, nil
	}
	if window.summary != nil && len(window.rounds) == 0 {
		return window.messages, nil
	}
	m.logCompressionThreshold(userID, characterID, trigger, len(window.rounds), maxRounds, len(window.messages), window)
	return m.compress(ctx, userID, characterID, trigger, maxRounds, opts, window)
}

func (m *Manager) compress(
	ctx context.Context,
	userID int64,
	characterID string,
	trigger string,
	maxRounds int,
	opts CompressionOptions,
	window *historyWindow,
) ([]*schema.Message, error) {
	startedAt := time.Now()
	roundCount := len(window.rounds)
	cutoffRoundID := window.lastRoundID()
	m.logger.Info("session history compression started",
		zap.Int64("user_id", userID),
		zap.String("character_id", characterID),
		zap.String("trigger", trigger),
		zap.Int("rounds", roundCount),
		zap.Int("max_rounds", maxRounds),
		zap.Uint("previous_cutoff_round_id", window.cutoffRoundID()),
		zap.Uint("cutoff_round_id", cutoffRoundID),
		zap.Int("history_messages", len(window.messages)),
	)
	instruction, err := m.renderCompressionInstruction(ctx, roundCount, window.summary != nil)
	if err != nil {
		m.logCompressionFailure(userID, characterID, trigger, roundCount, cutoffRoundID, startedAt, err)
		return nil, err
	}
	input := compressionInput(window, opts.Memory)
	input = append(input, instruction)

	response, err := runCompressionAgent(ctx, opts.Agent, input)
	if err != nil {
		m.logCompressionFailure(userID, characterID, trigger, roundCount, cutoffRoundID, startedAt, err)
		return nil, pkgErrors.Wrap(err, "compress session history")
	}
	compressed := strings.TrimSpace(msgops.AssistantText(response))
	if compressed == "" {
		err = errs.ErrCompressionEmptyHistory
		m.logCompressionFailure(userID, characterID, trigger, roundCount, cutoffRoundID, startedAt, err)
		return nil, err
	}
	compressedMessage := schema.SystemMessage(compressed)
	record, err := makeSessionSummary(userID, characterID, cutoffRoundID, compressedMessage)
	if err != nil {
		m.logCompressionFailure(userID, characterID, trigger, roundCount, cutoffRoundID, startedAt, err)
		return nil, err
	}
	if err = m.db.WithContext(ctx).Create(&record).Error; err != nil {
		m.logCompressionFailure(userID, characterID, trigger, roundCount, cutoffRoundID, startedAt, err)
		return nil, err
	}
	m.logger.Info("session history compression completed",
		zap.Int64("user_id", userID),
		zap.String("character_id", characterID),
		zap.String("trigger", trigger),
		zap.Uint("summary_id", record.ID),
		zap.Uint("cutoff_round_id", cutoffRoundID),
		zap.Int("compressed_rounds", roundCount),
		zap.Int("input_messages", len(input)),
		zap.Int("compressed_characters", len([]rune(compressed))),
		zap.Duration("elapsed", time.Since(startedAt)),
	)
	return []*schema.Message{compressedMessage}, nil
}

func compressionInput(window *historyWindow, memory *schema.Message) []*schema.Message {
	if memory == nil {
		return append([]*schema.Message(nil), window.messages...)
	}
	input := make([]*schema.Message, 0, len(window.messages)+1)
	insertAt := 0
	if window.summary != nil && len(window.messages) > 0 {
		insertAt = 1
	}
	input = append(input, window.messages[:insertAt]...)
	input = append(input, memory)
	input = append(input, window.messages[insertAt:]...)
	return input
}

func (m *Manager) logCompressionFailure(
	userID int64,
	characterID string,
	trigger string,
	roundCount int,
	cutoffRoundID uint,
	startedAt time.Time,
	err error,
) {
	m.logger.Warn("session history compression failed",
		zap.Int64("user_id", userID),
		zap.String("character_id", characterID),
		zap.String("trigger", trigger),
		zap.Int("rounds", roundCount),
		zap.Uint("cutoff_round_id", cutoffRoundID),
		zap.Bool("history_preserved", true),
		zap.Duration("elapsed", time.Since(startedAt)),
		zap.Error(err),
	)
}

func (m *Manager) logCompressionThreshold(
	userID int64,
	characterID string,
	trigger string,
	roundCount int,
	maxRounds int,
	historyMessages int,
	window *historyWindow,
) {
	m.logger.Info("session history compression threshold reached",
		zap.Int64("user_id", userID),
		zap.String("character_id", characterID),
		zap.String("trigger", trigger),
		zap.Int("rounds", roundCount),
		zap.Int("max_rounds", maxRounds),
		zap.Int("history_messages", historyMessages),
		zap.Bool("has_summary", window.summary != nil),
		zap.Uint("previous_cutoff_round_id", window.cutoffRoundID()),
		zap.Uint("cutoff_round_id", window.lastRoundID()),
	)
}

func (m *Manager) renderCompressionInstruction(ctx context.Context, roundCount int, hasPreviousSummary bool) (*schema.Message, error) {
	messages, err := m.compression.Format(ctx, map[string]any{
		"RoundCount":         roundCount,
		"HasPreviousSummary": hasPreviousSummary,
	})
	if err != nil {
		return nil, err
	}
	if len(messages) != 1 {
		return nil, errs.ErrCompressionTemplateNoMessage
	}
	return messages[0], nil
}

func runCompressionAgent(ctx context.Context, agent adk.Agent, input []*schema.Message) (*schema.Message, error) {
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	events := runner.Run(ctx, input)
	var response *schema.Message
	for {
		event, ok := events.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			return nil, event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		message, err := event.Output.MessageOutput.GetMessage()
		if err != nil {
			return nil, err
		}
		if message == nil {
			continue
		}
		if message.Role == "" {
			message.Role = event.Output.MessageOutput.Role
		}
		if message.Role == schema.Assistant {
			response = message
		}
	}
	if response == nil {
		return nil, errs.ErrCompressionAgentNoMessage
	}
	return response, nil
}

func (m *Manager) loadHistoryWindow(ctx context.Context, userID int64, characterID string, excludedRoundID uint) (*historyWindow, error) {
	window := &historyWindow{}
	var summary model.SessionSummary
	err := m.db.WithContext(ctx).
		Where("user_id = ? AND character_id = ?", userID, characterID).
		Order("cutoff_round_id DESC").
		Order("id DESC").
		First(&summary).Error
	if err == nil {
		window.summary = &summary
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	roundsQuery := m.db.WithContext(ctx).
		Where("user_id = ? AND character_id = ?", userID, characterID)
	if window.summary != nil {
		roundsQuery = roundsQuery.Where("id > ?", window.summary.CutoffRoundID)
	}
	if excludedRoundID != 0 {
		roundsQuery = roundsQuery.Where("id <> ?", excludedRoundID)
	}
	if err = roundsQuery.Order("id ASC").Find(&window.rounds).Error; err != nil {
		return nil, err
	}
	window.messages, err = m.decodeHistoryWindow(ctx, userID, characterID, window.summary, window.rounds)
	if err != nil {
		return nil, err
	}
	return window, nil
}

func (m *Manager) decodeHistoryWindow(
	ctx context.Context,
	userID int64,
	characterID string,
	summary *model.SessionSummary,
	rounds []model.SessionRound,
) ([]*schema.Message, error) {
	messages := make([]*schema.Message, 0)
	if summary != nil {
		var message *schema.Message
		if err := json.Unmarshal([]byte(summary.Message), &message); err != nil {
			return nil, pkgErrors.Wrapf(err, "decode session summary %d", summary.ID)
		}
		messages = append(messages, message)
	}
	if len(rounds) == 0 {
		return messages, nil
	}
	roundIDs := make([]uint, 0, len(rounds))
	for _, round := range rounds {
		roundIDs = append(roundIDs, round.ID)
	}
	var records []model.SessionMessage
	if err := m.db.WithContext(ctx).
		Where("user_id = ? AND character_id = ? AND round_id IN ?", userID, characterID, roundIDs).
		Order("id ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	byRound := make(map[uint][]model.SessionMessage, len(rounds))
	for _, record := range records {
		byRound[record.RoundID] = append(byRound[record.RoundID], record)
	}
	for _, round := range rounds {
		formatted, err := m.formatRound(round, byRound[round.ID])
		if err != nil {
			return nil, err
		}
		messages = append(messages, formatted...)
	}
	return messages, nil
}

func (m *Manager) loadRound(ctx context.Context, userID int64, characterID string, roundID uint) ([]*schema.Message, error) {
	var round model.SessionRound
	if err := m.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND character_id = ?", roundID, userID, characterID).
		First(&round).Error; err != nil {
		return nil, err
	}
	var records []model.SessionMessage
	if err := m.db.WithContext(ctx).
		Where("round_id = ? AND user_id = ? AND character_id = ?", roundID, userID, characterID).
		Order("id ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	return m.formatRound(round, records)
}

func (m *Manager) formatRound(round model.SessionRound, records []model.SessionMessage) ([]*schema.Message, error) {
	messages := make([]*schema.Message, 0, len(records))
	previousWasMetadata := false
	for _, record := range records {
		message, err := decodeSessionMessage(record)
		if err != nil {
			return nil, err
		}
		if record.MessageMetadata {
			if message.Role != schema.System {
				return nil, errs.SessionMetadataNotSystem(record.ID)
			}
		} else if message.Role == schema.User && !previousWasMetadata {
			return nil, errs.SessionUserNoMetadata(record.ID)
		}
		messages = append(messages, message)
		previousWasMetadata = record.MessageMetadata
	}
	if len(messages) == 0 {
		return nil, errs.SessionRoundEmpty(round.ID)
	}
	return messages, nil
}

func (m *Manager) renderMessageMetadata(ctx context.Context, sentAt time.Time, interrupted bool) (*schema.Message, error) {
	messages, err := m.messageMetadata.Format(ctx, map[string]any{
		"Time":        sentAt.Format(time.RFC3339),
		"Interrupted": interrupted,
	})
	if err != nil {
		return nil, err
	}
	if len(messages) != 1 {
		return nil, errs.ErrMessageMetadataTemplateNoMessage
	}
	return messages[0], nil
}

func decodeSessionMessage(record model.SessionMessage) (*schema.Message, error) {
	var message *schema.Message
	if err := json.Unmarshal([]byte(record.Message), &message); err != nil {
		return nil, pkgErrors.Wrapf(err, "decode session message %d", record.ID)
	}
	compacted, _, _ := compactToolResults([]*schema.Message{message})
	return compacted[0], nil
}

func makeSessionMessage(
	userID int64,
	characterID string,
	roundID uint,
	interrupted bool,
	message *schema.Message,
) (model.SessionMessage, error) {
	persisted, _, _ := compactToolResults([]*schema.Message{message})
	data, err := marshalSessionJSON(persisted[0], "session message")
	if err != nil {
		return model.SessionMessage{}, err
	}
	return model.SessionMessage{
		UserID:      userID,
		CharacterID: characterID,
		RoundID:     roundID,
		Interrupted: interrupted,
		Message:     data,
	}, nil
}

func compactToolResults(messages []*schema.Message) ([]*schema.Message, int, int) {
	compacted := make([]*schema.Message, 0, len(messages))
	remaining := maxPersistedToolRoundRunes
	compactedMessages := 0
	removedRunes := 0
	for _, message := range messages {
		if message == nil || message.Role != schema.Tool {
			compacted = append(compacted, message)
			continue
		}

		content := []rune(message.Content)
		limit := min(maxPersistedToolResultRunes, remaining)
		remaining -= min(len(content), limit)
		if len(content) <= limit {
			compacted = append(compacted, message)
			continue
		}

		copy := *message
		copy.Content = compactToolResultContent(content, limit)
		compacted = append(compacted, &copy)
		compactedMessages++
		removedRunes += len(content) - limit
	}
	return compacted, compactedMessages, removedRunes
}

func compactToolResultContent(content []rune, limit int) string {
	if limit <= 0 {
		return "[tool result omitted from long-term history]"
	}
	omitted := []rune("\n...[tool result truncated for long-term history]...\n")
	if limit <= len(omitted) {
		return string(omitted[:limit])
	}
	tail := min(toolResultTailRunes, (limit-len(omitted))/4)
	head := limit - len(omitted) - tail
	return string(content[:head]) + string(omitted) + string(content[len(content)-tail:])
}

func makeSessionSummary(userID int64, characterID string, cutoffRoundID uint, message *schema.Message) (model.SessionSummary, error) {
	data, err := marshalSessionJSON(message, "session summary")
	if err != nil {
		return model.SessionSummary{}, err
	}
	return model.SessionSummary{
		UserID:        userID,
		CharacterID:   characterID,
		CutoffRoundID: cutoffRoundID,
		Message:       data,
	}, nil
}

func marshalSessionJSON(v any, what string) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", pkgErrors.Wrapf(err, "encode %s", what)
	}
	return string(data), nil
}

func normalizeMaxRounds(maxRounds int) int {
	if maxRounds <= 0 {
		return constants.DefaultMaxRounds
	}
	return maxRounds
}

func (m *Manager) lock(ctx context.Context, userID int64, characterID, operation string) (func(), error) {
	key := sessionKey{userID: userID, characterID: characterID}
	m.locksMu.Lock()
	lock := m.locks[key]
	if lock == nil {
		lock = &sessionLock{gate: make(chan struct{}, 1)}
		lock.gate <- struct{}{}
		m.locks[key] = lock
	}
	lock.refs++
	m.locksMu.Unlock()

	waitStartedAt := time.Now()
	select {
	case <-lock.gate:
	default:
		m.logger.Debug("waiting for session operation",
			zap.Int64("user_id", userID),
			zap.String("character_id", characterID),
			zap.String("operation", operation),
		)
		select {
		case <-lock.gate:
			m.logger.Debug("session operation lock acquired",
				zap.Int64("user_id", userID),
				zap.String("character_id", characterID),
				zap.String("operation", operation),
				zap.Duration("waited", time.Since(waitStartedAt)),
			)
		case <-ctx.Done():
			m.releaseLockRef(key, lock)
			return nil, ctx.Err()
		}
	}
	return func() {
		lock.gate <- struct{}{}
		m.releaseLockRef(key, lock)
	}, nil
}

func (m *Manager) releaseLockRef(key sessionKey, lock *sessionLock) {
	m.locksMu.Lock()
	lock.refs--
	if lock.refs == 0 {
		delete(m.locks, key)
	}
	m.locksMu.Unlock()
}

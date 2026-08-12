package session

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chhongzh/atri-bot/internal/msgops"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const DefaultMaxRounds = 36

const (
	maxPersistedToolResultRunes = 2_000
	maxPersistedToolRoundRunes  = 8_000
	toolResultTailRunes         = 400
)

//go:embed compression.j2
var compressionTemplate string

type CompressionOptions struct {
	MaxRounds    int
	SystemPrompt string
	Agent        adk.Agent
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
	summary  *summaryEntry
	rounds   []roundEntry
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
	db       *gorm.DB
	logger   *zap.Logger
	template *prompt.DefaultChatTemplate

	locksMu sync.Mutex
	locks   map[sessionKey]*sessionLock
}

func New(db *gorm.DB, logger *zap.Logger) *Manager {
	return &Manager{
		db:       db,
		logger:   logger,
		template: prompt.FromMessages(schema.Jinja2, schema.SystemMessage(compressionTemplate)),
		locks:    make(map[sessionKey]*sessionLock),
	}
}

func (m *Manager) Init() error {
	return m.db.AutoMigrate(&roundEntry{}, &summaryEntry{})
}

func (m *Manager) Load(ctx context.Context, userID int64, characterID string, opts CompressionOptions) ([]*schema.Message, error) {
	release, err := m.lock(ctx, userID, characterID, "load")
	if err != nil {
		return nil, err
	}
	defer release()

	window, err := m.loadHistoryWindow(ctx, userID, characterID)
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
	if len(window.rounds) < maxRounds {
		return window.messages, nil
	}
	m.logCompressionThreshold(userID, characterID, "load", len(window.rounds), maxRounds, len(window.messages), window)
	return m.compress(ctx, userID, characterID, "load", maxRounds, opts, window)
}

func (m *Manager) AppendRound(
	ctx context.Context,
	userID int64,
	characterID string,
	opts CompressionOptions,
	messages ...*schema.Message,
) error {
	if len(messages) == 0 {
		return nil
	}
	persisted, compactedMessages, removedRunes := compactToolResults(messages)
	if compactedMessages > 0 {
		m.logger.Info("compacted tool results for session history",
			zap.Int64("user_id", userID),
			zap.String("character_id", characterID),
			zap.Int("tool_messages", compactedMessages),
			zap.Int("removed_characters", removedRunes),
		)
	}
	record, err := makeRoundEntry(userID, characterID, persisted)
	if err != nil {
		return err
	}
	release, err := m.lock(ctx, userID, characterID, "append_round")
	if err != nil {
		return err
	}
	defer release()
	if err = m.db.WithContext(ctx).Create(&record).Error; err != nil {
		return err
	}
	m.logger.Debug("appended session round",
		zap.Int64("user_id", userID),
		zap.String("character_id", characterID),
		zap.Uint("round_id", record.ID),
		zap.Int("messages", len(messages)),
	)
	window, err := m.loadHistoryWindow(ctx, userID, characterID)
	if err != nil {
		return err
	}
	maxRounds := normalizeMaxRounds(opts.MaxRounds)
	if len(window.rounds) < maxRounds {
		return nil
	}
	m.logCompressionThreshold(userID, characterID, "append_round", len(window.rounds), maxRounds, len(window.messages), window)
	_, err = m.compress(ctx, userID, characterID, "append_round", maxRounds, opts, window)
	return err
}

func (m *Manager) Clear(ctx context.Context, userID int64, characterID string) error {
	release, err := m.lock(ctx, userID, characterID, "clear")
	if err != nil {
		return err
	}
	defer release()
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if txErr := tx.Where("user_id = ? AND character_id = ?", userID, characterID).Delete(&summaryEntry{}).Error; txErr != nil {
			return txErr
		}
		return tx.Where("user_id = ? AND character_id = ?", userID, characterID).Delete(&roundEntry{}).Error
	})
}

func (m *Manager) Wait(ctx context.Context, userID int64, characterID string) error {
	release, err := m.lock(ctx, userID, characterID, "new_message")
	if err != nil {
		return err
	}
	release()
	return nil
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
	instruction, err := m.renderCompressionInstruction(ctx, roundCount)
	if err != nil {
		m.logCompressionFailure(userID, characterID, trigger, roundCount, cutoffRoundID, startedAt, err)
		return nil, err
	}
	input := make([]*schema.Message, 0, len(window.messages)+2)
	input = append(input, schema.SystemMessage(opts.SystemPrompt))
	input = append(input, window.messages...)
	input = append(input, schema.SystemMessage(instruction))

	response, err := runCompressionAgent(ctx, opts.Agent, input)
	if err != nil {
		m.logCompressionFailure(userID, characterID, trigger, roundCount, cutoffRoundID, startedAt, err)
		return nil, fmt.Errorf("compress session history: %w", err)
	}
	compressed := strings.TrimSpace(msgops.AssistantText(response))
	if compressed == "" {
		err = errors.New("compression model returned empty history")
		m.logCompressionFailure(userID, characterID, trigger, roundCount, cutoffRoundID, startedAt, err)
		return nil, err
	}
	compressedMessage := schema.SystemMessage(compressed)
	record, err := makeSummaryEntry(userID, characterID, cutoffRoundID, compressedMessage)
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

func (m *Manager) renderCompressionInstruction(ctx context.Context, roundCount int) (string, error) {
	messages, err := m.template.Format(ctx, map[string]any{"RoundCount": roundCount})
	if err != nil {
		return "", err
	}
	if len(messages) != 1 {
		return "", errors.New("compression template returned no message")
	}
	return messages[0].Content, nil
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
		return nil, errors.New("compression agent returned no assistant message")
	}
	return response, nil
}

func (m *Manager) loadHistoryWindow(ctx context.Context, userID int64, characterID string) (*historyWindow, error) {
	window := &historyWindow{}
	var summary summaryEntry
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
	if err = roundsQuery.Order("id ASC").Find(&window.rounds).Error; err != nil {
		return nil, err
	}
	window.messages, err = decodeHistoryWindow(window.summary, window.rounds)
	if err != nil {
		return nil, err
	}
	return window, nil
}

func decodeHistoryWindow(summary *summaryEntry, rounds []roundEntry) ([]*schema.Message, error) {
	messages := make([]*schema.Message, 0)
	if summary != nil {
		var message *schema.Message
		if err := json.Unmarshal([]byte(summary.Message), &message); err != nil {
			return nil, fmt.Errorf("decode session summary %d: %w", summary.ID, err)
		}
		messages = append(messages, message)
	}
	for _, record := range rounds {
		var decoded []*schema.Message
		if err := json.Unmarshal([]byte(record.Messages), &decoded); err != nil {
			return nil, fmt.Errorf("decode session round %d: %w", record.ID, err)
		}
		compacted, _, _ := compactToolResults(decoded)
		messages = append(messages, compacted...)
	}
	return messages, nil
}

func makeRoundEntry(userID int64, characterID string, messages []*schema.Message) (roundEntry, error) {
	persisted, _, _ := compactToolResults(messages)
	data, err := json.Marshal(persisted)
	if err != nil {
		return roundEntry{}, fmt.Errorf("encode session round: %w", err)
	}
	return roundEntry{UserID: userID, CharacterID: characterID, Messages: string(data)}, nil
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

func makeSummaryEntry(userID int64, characterID string, cutoffRoundID uint, message *schema.Message) (summaryEntry, error) {
	data, err := json.Marshal(message)
	if err != nil {
		return summaryEntry{}, fmt.Errorf("encode session summary: %w", err)
	}
	return summaryEntry{
		UserID:        userID,
		CharacterID:   characterID,
		CutoffRoundID: cutoffRoundID,
		Message:       string(data),
	}, nil
}

func normalizeMaxRounds(maxRounds int) int {
	if maxRounds <= 0 {
		return DefaultMaxRounds
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

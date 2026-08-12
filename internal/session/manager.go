package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

const DefaultMaxRounds = 36

type Manager struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Manager {
	return &Manager{db: db}
}

func (m *Manager) Init() error {
	return m.db.AutoMigrate(&model.Record{})
}

func (m *Manager) Load(ctx context.Context, userID int64, characterID string, maxRounds int) ([]*schema.Message, error) {
	var records []model.Record
	err := m.db.WithContext(ctx).
		Where("user_id = ? AND character_id = ?", userID, characterID).
		Order("created_at ASC").
		Order("id ASC").
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	messages := make([]*schema.Message, 0, len(records))
	for _, record := range records {
		var message *schema.Message
		if err = json.Unmarshal([]byte(record.Message), &message); err != nil {
			return nil, fmt.Errorf("decode session record %d: %w", record.ID, err)
		}
		messages = append(messages, message)
	}
	return trimRounds(messages, normalizeMaxRounds(maxRounds)), nil
}

func (m *Manager) Append(ctx context.Context, userID int64, characterID string, maxRounds int, messages ...*schema.Message) error {
	records, err := makeRecords(userID, characterID, messages)
	if err != nil {
		return err
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err = tx.Create(&records).Error; err != nil {
			return err
		}
		return trimStoredRounds(tx, userID, characterID, normalizeMaxRounds(maxRounds))
	})
}

func (m *Manager) Save(ctx context.Context, userID int64, characterID string, maxRounds int, messages []*schema.Message) error {
	messages = withoutLeadingSystem(messages)
	messages = trimRounds(messages, normalizeMaxRounds(maxRounds))
	records, err := makeRecords(userID, characterID, messages)
	if err != nil {
		return err
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err = tx.Where("user_id = ? AND character_id = ?", userID, characterID).Delete(&model.Record{}).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		return tx.Create(&records).Error
	})
}

func normalizeMaxRounds(maxRounds int) int {
	if maxRounds <= 0 {
		return DefaultMaxRounds
	}
	return maxRounds
}

func (m *Manager) Clear(ctx context.Context, userID int64, characterID string) error {
	return m.db.WithContext(ctx).
		Where("user_id = ? AND character_id = ?", userID, characterID).
		Delete(&model.Record{}).Error
}

func makeRecords(userID int64, characterID string, messages []*schema.Message) ([]model.Record, error) {
	records := make([]model.Record, 0, len(messages))
	for index, message := range messages {
		data, err := json.Marshal(message)
		if err != nil {
			return nil, fmt.Errorf("encode message %d: %w", index, err)
		}
		records = append(records, model.Record{
			UserID:      userID,
			CharacterID: characterID,
			Role:        string(message.Role),
			Message:     string(data),
		})
	}
	return records, nil
}

func trimStoredRounds(tx *gorm.DB, userID int64, characterID string, maxRounds int) error {
	if maxRounds <= 0 {
		return nil
	}
	var cutoff model.Record
	err := tx.Select("id", "created_at").
		Where("user_id = ? AND character_id = ? AND role = ?", userID, characterID, schema.User).
		Order("created_at DESC").
		Order("id DESC").
		Offset(maxRounds - 1).
		First(&cutoff).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return tx.Where("user_id = ? AND character_id = ?", userID, characterID).
		Where("created_at < ? OR (created_at = ? AND id < ?)", cutoff.CreatedAt, cutoff.CreatedAt, cutoff.ID).
		Delete(&model.Record{}).Error
}

func withoutLeadingSystem(messages []*schema.Message) []*schema.Message {
	if len(messages) > 0 && messages[0].Role == schema.System {
		return messages[1:]
	}
	return messages
}

func trimRounds(messages []*schema.Message, maxRounds int) []*schema.Message {
	if maxRounds <= 0 {
		return messages
	}
	userCount := 0
	start := 0
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != schema.User {
			continue
		}
		userCount++
		if userCount == maxRounds {
			start = index
			break
		}
	}
	if userCount < maxRounds {
		return messages
	}
	return messages[start:]
}

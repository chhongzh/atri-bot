package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrNotFound = errors.New("configuration not found")

type Manager struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Manager {
	return &Manager{db: db}
}

func (m *Manager) Init() error {
	return m.db.AutoMigrate(&Record{})
}

// Query decodes a global configuration value directly from its JSON record.
func (m *Manager) Query[T any](ctx context.Context, key string) (T, error) {
	return query[T](ctx, m, globalScope, 0, key)
}

// QueryUser decodes a user-specific configuration value directly from JSON.
func (m *Manager) QueryUser[T any](ctx context.Context, userID int64, key string) (T, error) {
	return query[T](ctx, m, userScope, userID, key)
}

func (m *Manager) Set[T any](ctx context.Context, key string, value T) error {
	return set(ctx, m, globalScope, 0, key, value)
}

func (m *Manager) SetUser[T any](ctx context.Context, userID int64, key string, value T) error {
	return set(ctx, m, userScope, userID, key, value)
}

func query[T any](ctx context.Context, manager *Manager, scope string, userID int64, key string) (T, error) {
	var zero T
	key = strings.TrimSpace(key)
	if key == "" {
		return zero, errors.New("configuration key is required")
	}
	var record Record
	err := manager.db.WithContext(ctx).First(&record, "scope = ? AND user_id = ? AND key = ?", scope, userID, key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return zero, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if err != nil {
		return zero, err
	}
	if err = json.Unmarshal([]byte(record.Value), &zero); err != nil {
		return zero, fmt.Errorf("decode configuration %q: %w", key, err)
	}
	return zero, nil
}

func set[T any](ctx context.Context, manager *Manager, scope string, userID int64, key string, value T) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("configuration key is required")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode configuration %q: %w", key, err)
	}
	record := Record{Scope: scope, UserID: userID, Key: key, Value: string(data)}
	return manager.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "scope"},
			{Name: "user_id"},
			{Name: "key"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&record).Error
}

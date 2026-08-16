// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/model"
	pkgErrors "github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Manager struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Manager {
	return &Manager{db: db}
}

func (m *Manager) Init() error {
	return m.db.AutoMigrate(&model.Config{})
}

// Query decodes a global configuration value directly from its JSON record.
func (m *Manager) Query[T any](ctx context.Context, key string) (T, error) {
	return query[T](ctx, m, model.GlobalScope, 0, key)
}

// QueryUser decodes a user-specific configuration value directly from JSON.
func (m *Manager) QueryUser[T any](ctx context.Context, userID int64, key string) (T, error) {
	return query[T](ctx, m, model.UserScope, userID, key)
}

func (m *Manager) Set[T any](ctx context.Context, key string, value T) error {
	return set(ctx, m, model.GlobalScope, 0, key, value)
}

func (m *Manager) SetUser[T any](ctx context.Context, userID int64, key string, value T) error {
	return set(ctx, m, model.UserScope, userID, key, value)
}

func query[T any](ctx context.Context, manager *Manager, scope string, userID int64, key string) (T, error) {
	var zero T
	key, err := normalizeKey(key)
	if err != nil {
		return zero, err
	}
	var record model.Config
	err = manager.db.WithContext(ctx).First(&record, "scope = ? AND user_id = ? AND key = ?", scope, userID, key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return zero, errs.ConfigNotFound(key)
	}
	if err != nil {
		return zero, err
	}
	if err = json.Unmarshal([]byte(record.Value), &zero); err != nil {
		return zero, pkgErrors.Wrapf(err, "decode configuration %q", key)
	}
	return zero, nil
}

func set[T any](ctx context.Context, manager *Manager, scope string, userID int64, key string, value T) error {
	key, err := normalizeKey(key)
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return pkgErrors.Wrapf(err, "encode configuration %q", key)
	}
	record := model.Config{Scope: scope, UserID: userID, Key: key, Value: string(data)}
	return manager.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "scope"},
			{Name: "user_id"},
			{Name: "key"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&record).Error
}

func normalizeKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errs.ErrConfigKeyRequired
	}
	return key, nil
}

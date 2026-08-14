// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package account

import (
	"context"
	"errors"
	"fmt"
	"sync"

	configmanager "github.com/chhongzh/atri-bot/internal/config"
	"github.com/chhongzh/atri-bot/internal/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrUserNotFound     = errors.New("user not found")
	ErrPermissionDenied = errors.New("permission denied")
	ErrLastAdmin        = errors.New("the last administrator cannot be removed")
	ErrSelfDemotion     = errors.New("administrators cannot demote themselves")
)

type Manager struct {
	db               *gorm.DB
	logger           *zap.Logger
	configs          *configmanager.Manager
	defaultMaxRounds int

	createMu sync.Mutex
}

func New(db *gorm.DB, logger *zap.Logger, configs *configmanager.Manager, defaultMaxRounds int) *Manager {
	if defaultMaxRounds <= 0 {
		defaultMaxRounds = 36
	}
	return &Manager{db: db, logger: logger, configs: configs, defaultMaxRounds: defaultMaxRounds}
}

func (m *Manager) Init() error {
	if err := m.db.AutoMigrate(&model.User{}); err != nil {
		return err
	}
	if err := m.configs.Init(); err != nil {
		return err
	}
	return nil
}

func (m *Manager) EnsureUser(ctx context.Context, id int64, username, displayName string) (*model.User, bool, error) {
	m.createMu.Lock()
	defer m.createMu.Unlock()

	var user model.User
	err := m.db.WithContext(ctx).First(&user, "telegram_id = ?", id).Error
	if err == nil {
		updates := map[string]any{}
		if user.Username != username {
			updates["username"] = username
			user.Username = username
		}
		if user.DisplayName != displayName {
			updates["display_name"] = displayName
			user.DisplayName = displayName
		}
		if len(updates) > 0 {
			if err = m.db.WithContext(ctx).Model(&user).Updates(updates).Error; err != nil {
				return nil, false, err
			}
		}
		return &user, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if txErr := tx.Model(&model.User{}).Count(&count).Error; txErr != nil {
			return txErr
		}
		role := model.RoleUser
		if count == 0 {
			role = model.RoleAdmin
		}
		user = model.User{
			TelegramID:  id,
			Username:    username,
			DisplayName: displayName,
			Role:        role,
		}
		return tx.Create(&user).Error
	})
	if err != nil {
		return nil, false, err
	}
	if err = m.configs.SetUser(ctx, id, configmanager.UserSettingsKey, configmanager.UserSettings{AIMaxRounds: m.defaultMaxRounds}); err != nil {
		return nil, false, err
	}

	m.logger.Info("created telegram user",
		zap.Int64("user_id", id),
		zap.String("role", string(user.Role)),
	)
	return &user, true, nil
}

func (m *Manager) Get(ctx context.Context, id int64) (*model.User, error) {
	var user model.User
	err := m.db.WithContext(ctx).First(&user, "telegram_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	return &user, err
}

func (m *Manager) Settings(ctx context.Context, id int64) (configmanager.UserSettings, error) {
	settings, err := m.configs.QueryUser[configmanager.UserSettings](ctx, id, configmanager.UserSettingsKey)
	if errors.Is(err, configmanager.ErrNotFound) {
		return configmanager.UserSettings{AIMaxRounds: m.defaultMaxRounds}, nil
	}
	return settings, err
}

func (m *Manager) SetSettings(ctx context.Context, id int64, settings configmanager.UserSettings) error {
	if settings.AIMaxRounds <= 0 {
		return errors.New("AI max rounds must be positive")
	}
	if _, err := m.Get(ctx, id); err != nil {
		return err
	}
	return m.configs.SetUser(ctx, id, configmanager.UserSettingsKey, settings)
}

func (m *Manager) IsAdmin(ctx context.Context, id int64) (bool, error) {
	user, err := m.Get(ctx, id)
	if err != nil {
		return false, err
	}
	return user.Role == model.RoleAdmin && !user.Banned, nil
}

func (m *Manager) SetRole(ctx context.Context, actorID, targetID int64, role model.Role) error {
	if role != model.RoleUser && role != model.RoleAdmin {
		return fmt.Errorf("invalid role %q", role)
	}
	if actorID == targetID && role != model.RoleAdmin {
		return ErrSelfDemotion
	}

	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireAdmin(tx, actorID); err != nil {
			return err
		}
		target, err := loadTargetUser(tx, targetID)
		if err != nil {
			return err
		}
		if target.Role == model.RoleAdmin && role != model.RoleAdmin {
			if err := ensureAnotherAdmin(tx, targetID); err != nil {
				return err
			}
		}
		return tx.Model(target).Update("role", role).Error
	})
}

func (m *Manager) SetBanned(ctx context.Context, actorID, targetID int64, banned bool) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireAdmin(tx, actorID); err != nil {
			return err
		}
		target, err := loadTargetUser(tx, targetID)
		if err != nil {
			return err
		}
		if banned && target.Role == model.RoleAdmin {
			if err := ensureAnotherAdmin(tx, targetID); err != nil {
				return err
			}
		}
		return tx.Model(target).Update("banned", banned).Error
	})
}

func (m *Manager) Delete(ctx context.Context, actorID, targetID int64) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireAdmin(tx, actorID); err != nil {
			return err
		}
		target, err := loadTargetUser(tx, targetID)
		if err != nil {
			return err
		}
		if target.Role == model.RoleAdmin {
			if err := ensureAnotherAdmin(tx, targetID); err != nil {
				return err
			}
		}
		return tx.Delete(target).Error
	})
}

func (m *Manager) SetCharacter(ctx context.Context, id int64, characterID string) error {
	return m.updateSettings(ctx, id, func(settings *configmanager.UserSettings) {
		settings.CharacterID = characterID
	})
}

func (m *Manager) SetAIBaseURL(ctx context.Context, id int64, value string) error {
	return m.updateSettings(ctx, id, func(settings *configmanager.UserSettings) {
		settings.AIBaseURL = value
	})
}

func (m *Manager) SetAIAPIKey(ctx context.Context, id int64, value string) error {
	return m.updateSettings(ctx, id, func(settings *configmanager.UserSettings) {
		settings.AIAPIKey = value
	})
}

func (m *Manager) SetAIModel(ctx context.Context, id int64, value string) error {
	return m.updateSettings(ctx, id, func(settings *configmanager.UserSettings) {
		settings.AIModel = value
	})
}

func (m *Manager) SetAIMaxRounds(ctx context.Context, id int64, value int) error {
	return m.updateSettings(ctx, id, func(settings *configmanager.UserSettings) {
		settings.AIMaxRounds = value
	})
}

func (m *Manager) updateSettings(ctx context.Context, id int64, mutate func(*configmanager.UserSettings)) error {
	settings, err := m.Settings(ctx, id)
	if err != nil {
		return err
	}
	mutate(&settings)
	return m.SetSettings(ctx, id, settings)
}

// SetMCPMaxTools sets a per-user override for the MCP provider limit.
// A value of 0 restores the global default.
func (m *Manager) SetMCPMaxTools(ctx context.Context, actorID, targetID int64, value int) error {
	if value < 0 {
		return errors.New("MCP max tools cannot be negative")
	}
	if err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireAdmin(tx, actorID); err != nil {
			return err
		}
		_, err := loadTargetUser(tx, targetID)
		return err
	}); err != nil {
		return err
	}
	settings, err := m.Settings(ctx, targetID)
	if err != nil {
		return err
	}
	settings.MCPMaxTools = value
	if err = m.configs.SetUser(ctx, targetID, configmanager.UserSettingsKey, settings); err != nil {
		return err
	}
	m.logger.Info("updated user mcp provider limit",
		zap.Int64("actor_id", actorID),
		zap.Int64("user_id", targetID),
		zap.Int("max_tools", value),
	)
	return nil
}

func (m *Manager) Admins(ctx context.Context) ([]model.User, error) {
	var users []model.User
	err := m.db.WithContext(ctx).
		Where("role = ? AND banned = ?", model.RoleAdmin, false).
		Order("telegram_id ASC").
		Find(&users).Error
	return users, err
}

// ListPage returns account identity and status fields for administrator views.
func (m *Manager) ListPage(ctx context.Context, filter model.UserListFilter, page, pageSize int) (*model.UserPage, error) {
	if page <= 0 {
		return nil, errors.New("page must be positive")
	}
	if pageSize <= 0 {
		return nil, errors.New("page size must be positive")
	}
	query := m.db.WithContext(ctx).
		Model(&model.User{})
	if filter.Role != nil {
		query = query.Where("role = ?", *filter.Role)
	}
	if filter.Banned != nil {
		query = query.Where("banned = ?", *filter.Banned)
	}
	result := &model.UserPage{Page: page}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	result.Pages = int((result.Total + int64(pageSize) - 1) / int64(pageSize))
	if result.Total == 0 || page > result.Pages {
		return result, nil
	}
	if err := query.
		Select("telegram_id", "username", "display_name", "role", "banned", "created_at", "updated_at").
		Order("telegram_id ASC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&result.Users).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) Stats(ctx context.Context) (*model.Stats, error) {
	stats := new(model.Stats)
	db := m.db.WithContext(ctx)
	if err := db.Model(&model.User{}).Count(&stats.Users).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.User{}).Where("role = ?", model.RoleAdmin).Count(&stats.Admins).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&model.User{}).Where("banned = ?", true).Count(&stats.Banned).Error; err != nil {
		return nil, err
	}
	return stats, nil
}

func requireAdmin(tx *gorm.DB, id int64) error {
	var actor model.User
	if err := tx.First(&actor, "telegram_id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	if actor.Role != model.RoleAdmin || actor.Banned {
		return ErrPermissionDenied
	}
	return nil
}

func loadTargetUser(tx *gorm.DB, targetID int64) (*model.User, error) {
	var target model.User
	if err := tx.First(&target, "telegram_id = ?", targetID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &target, nil
}

func ensureAnotherAdmin(tx *gorm.DB, excludedID int64) error {
	var count int64
	err := tx.Model(&model.User{}).
		Where("role = ? AND banned = ? AND telegram_id <> ?", model.RoleAdmin, false, excludedID).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrLastAdmin
	}
	return nil
}

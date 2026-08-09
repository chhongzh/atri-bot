package account

import (
	"context"
	"errors"
	"fmt"
	"sync"

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
	defaultMaxRounds int

	createMu sync.Mutex
}

func New(db *gorm.DB, logger *zap.Logger, defaultMaxRounds int) *Manager {
	if defaultMaxRounds <= 0 {
		defaultMaxRounds = 36
	}
	return &Manager{db: db, logger: logger, defaultMaxRounds: defaultMaxRounds}
}

func (m *Manager) Init() error {
	return m.db.AutoMigrate(&User{})
}

func (m *Manager) EnsureUser(ctx context.Context, id int64, username, displayName string) (*User, bool, error) {
	m.createMu.Lock()
	defer m.createMu.Unlock()

	var user User
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
		if user.AIMaxRounds <= 0 {
			updates["ai_max_rounds"] = m.defaultMaxRounds
			user.AIMaxRounds = m.defaultMaxRounds
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
		if txErr := tx.Model(&User{}).Count(&count).Error; txErr != nil {
			return txErr
		}
		role := RoleUser
		if count == 0 {
			role = RoleAdmin
		}
		user = User{
			TelegramID:  id,
			Username:    username,
			DisplayName: displayName,
			Role:        role,
			AIMaxRounds: m.defaultMaxRounds,
		}
		return tx.Create(&user).Error
	})
	if err != nil {
		return nil, false, err
	}

	m.logger.Info("created telegram user",
		zap.Int64("user_id", id),
		zap.String("role", string(user.Role)),
	)
	return &user, true, nil
}

func (m *Manager) Get(ctx context.Context, id int64) (*User, error) {
	var user User
	err := m.db.WithContext(ctx).First(&user, "telegram_id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	return &user, err
}

func (m *Manager) IsAdmin(ctx context.Context, id int64) (bool, error) {
	user, err := m.Get(ctx, id)
	if err != nil {
		return false, err
	}
	return user.Role == RoleAdmin && !user.Banned, nil
}

func (m *Manager) SetRole(ctx context.Context, actorID, targetID int64, role Role) error {
	if role != RoleUser && role != RoleAdmin {
		return fmt.Errorf("invalid role %q", role)
	}
	if actorID == targetID && role != RoleAdmin {
		return ErrSelfDemotion
	}

	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireAdmin(tx, actorID); err != nil {
			return err
		}
		var target User
		if err := tx.First(&target, "telegram_id = ?", targetID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}
		if target.Role == RoleAdmin && role != RoleAdmin {
			if err := ensureAnotherAdmin(tx, targetID); err != nil {
				return err
			}
		}
		return tx.Model(&target).Update("role", role).Error
	})
}

func (m *Manager) SetBanned(ctx context.Context, actorID, targetID int64, banned bool) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireAdmin(tx, actorID); err != nil {
			return err
		}
		var target User
		if err := tx.First(&target, "telegram_id = ?", targetID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}
		if banned && target.Role == RoleAdmin {
			if err := ensureAnotherAdmin(tx, targetID); err != nil {
				return err
			}
		}
		return tx.Model(&target).Update("banned", banned).Error
	})
}

func (m *Manager) Delete(ctx context.Context, actorID, targetID int64) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireAdmin(tx, actorID); err != nil {
			return err
		}
		var target User
		if err := tx.First(&target, "telegram_id = ?", targetID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}
		if target.Role == RoleAdmin {
			if err := ensureAnotherAdmin(tx, targetID); err != nil {
				return err
			}
		}
		return tx.Delete(&target).Error
	})
}

func (m *Manager) SetCharacter(ctx context.Context, id int64, characterID string) error {
	result := m.db.WithContext(ctx).Model(&User{}).
		Where("telegram_id = ?", id).
		Update("character_id", characterID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (m *Manager) SetAIBaseURL(ctx context.Context, id int64, value string) error {
	return m.updateUserField(ctx, id, "ai_base_url", value)
}

func (m *Manager) SetAIAPIKey(ctx context.Context, id int64, value string) error {
	return m.updateUserField(ctx, id, "ai_api_key", value)
}

func (m *Manager) SetAIModel(ctx context.Context, id int64, value string) error {
	return m.updateUserField(ctx, id, "ai_model", value)
}

func (m *Manager) SetAIMaxRounds(ctx context.Context, id int64, value int) error {
	if value <= 0 {
		return errors.New("AI max rounds must be positive")
	}
	result := m.db.WithContext(ctx).Model(&User{}).Where("telegram_id = ?", id).Update("ai_max_rounds", value)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
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
		var target User
		if err := tx.First(&target, "telegram_id = ?", targetID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}
		return tx.Model(&target).Update("mcp_max_tools", value).Error
	}); err != nil {
		return err
	}
	m.logger.Info("updated user mcp provider limit",
		zap.Int64("actor_id", actorID),
		zap.Int64("user_id", targetID),
		zap.Int("max_tools", value),
	)
	return nil
}

// SetMCPBlockInternal sets a per-user override for the internal network guard.
// A nil value restores the global default.
func (m *Manager) SetMCPBlockInternal(ctx context.Context, actorID, targetID int64, value *bool) error {
	if err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireAdmin(tx, actorID); err != nil {
			return err
		}
		var target User
		if err := tx.First(&target, "telegram_id = ?", targetID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return err
		}
		return tx.Model(&target).Update("mcp_block_internal", value).Error
	}); err != nil {
		return err
	}
	fields := []zap.Field{zap.Int64("actor_id", actorID), zap.Int64("user_id", targetID)}
	if value == nil {
		fields = append(fields, zap.String("block_internal", "default"))
	} else {
		fields = append(fields, zap.Bool("block_internal", *value))
	}
	m.logger.Info("updated user mcp internal network guard", fields...)
	return nil
}

func (m *Manager) Admins(ctx context.Context) ([]User, error) {
	var users []User
	err := m.db.WithContext(ctx).
		Where("role = ? AND banned = ?", RoleAdmin, false).
		Order("telegram_id ASC").
		Find(&users).Error
	return users, err
}

func (m *Manager) Stats(ctx context.Context) (*Stats, error) {
	stats := new(Stats)
	db := m.db.WithContext(ctx)
	if err := db.Model(&User{}).Count(&stats.Users).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&User{}).Where("role = ?", RoleAdmin).Count(&stats.Admins).Error; err != nil {
		return nil, err
	}
	if err := db.Model(&User{}).Where("banned = ?", true).Count(&stats.Banned).Error; err != nil {
		return nil, err
	}
	return stats, nil
}

func (m *Manager) updateUserField(ctx context.Context, id int64, field, value string) error {
	result := m.db.WithContext(ctx).Model(&User{}).Where("telegram_id = ?", id).Update(field, value)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

func requireAdmin(tx *gorm.DB, id int64) error {
	var actor User
	if err := tx.First(&actor, "telegram_id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	if actor.Role != RoleAdmin || actor.Banned {
		return ErrPermissionDenied
	}
	return nil
}

func ensureAnotherAdmin(tx *gorm.DB, excludedID int64) error {
	var count int64
	err := tx.Model(&User{}).
		Where("role = ? AND banned = ? AND telegram_id <> ?", RoleAdmin, false, excludedID).
		Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrLastAdmin
	}
	return nil
}

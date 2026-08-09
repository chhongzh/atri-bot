package chat

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/cloudwego/eino/components/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func normalizeDefaultToolPermissions(
	logger *zap.Logger,
	tools *toolmanager.Manager,
	defaults map[string]bool,
) map[string]bool {
	normalized := make(map[string]bool, len(defaults))
	known := make(map[string]bool, len(tools.AllNames()))
	for _, name := range tools.AllNames() {
		known[name] = true
	}
	unknown := make([]string, 0)
	for name, allowed := range defaults {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		normalized[name] = allowed
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		logger.Warn("default tool permissions reference unknown tools",
			zap.Strings("tools", unknown),
		)
	}
	return normalized
}

// ToolPermission records an individual user's permission for one tool.
// When no record exists for a user, the configured default applies.
type ToolPermission struct {
	UserID   int64  `gorm:"primaryKey;autoIncrement:false"`
	ToolName string `gorm:"primaryKey;size:255"`
	Allowed  bool   `gorm:"not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ToolPermissionInfo describes the effective permission for one tool.
type ToolPermissionInfo struct {
	ToolName string
	Allowed  bool
	Default  bool
	Custom   bool
}

// Init migrates the tool permission table.
func (m *Manager) Init() error {
	return m.db.AutoMigrate(&ToolPermission{})
}

// SetToolPermission sets a per-user override for a tool.
func (m *Manager) SetToolPermission(ctx context.Context, userID int64, toolName string, allowed bool) error {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return errors.New("tool name is required")
	}
	if !m.tools.Has(toolName) {
		return fmt.Errorf("%w: %s", toolmanager.ErrToolNotFound, toolName)
	}
	record := ToolPermission{UserID: userID, ToolName: toolName, Allowed: allowed}
	if err := m.db.WithContext(ctx).Save(&record).Error; err != nil {
		return err
	}
	m.logger.Info("updated tool permission",
		zap.Int64("user_id", userID),
		zap.String("tool", toolName),
		zap.Bool("allowed", allowed),
	)
	return nil
}

// ResetToolPermission removes a per-user override so the default applies again.
func (m *Manager) ResetToolPermission(ctx context.Context, userID int64, toolName string) error {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return errors.New("tool name is required")
	}
	if !m.tools.Has(toolName) {
		return fmt.Errorf("%w: %s", toolmanager.ErrToolNotFound, toolName)
	}
	if err := m.db.WithContext(ctx).
		Where("user_id = ? AND tool_name = ?", userID, toolName).
		Delete(&ToolPermission{}).Error; err != nil {
		return err
	}
	m.logger.Info("reset tool permission",
		zap.Int64("user_id", userID),
		zap.String("tool", toolName),
	)
	return nil
}

// ToolPermissionInfo returns the effective permission for a tool:
// the per-user override wins, otherwise the configured default applies.
func (m *Manager) ToolPermissionInfo(ctx context.Context, userID int64, toolName string) (*ToolPermissionInfo, error) {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return nil, errors.New("tool name is required")
	}
	defaultAllowed, hasDefault := m.defaultToolPermissions[toolName]
	if !hasDefault {
		defaultAllowed = true
	}
	var record ToolPermission
	err := m.db.WithContext(ctx).
		Where("user_id = ? AND tool_name = ?", userID, toolName).
		First(&record).Error
	if err == nil {
		return &ToolPermissionInfo{
			ToolName: toolName,
			Allowed:  record.Allowed,
			Default:  defaultAllowed,
			Custom:   true,
		}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return &ToolPermissionInfo{
		ToolName: toolName,
		Allowed:  defaultAllowed,
		Default:  defaultAllowed,
		Custom:   false,
	}, nil
}

// ToolAllowed reports whether a user is allowed to use a tool.
func (m *Manager) ToolAllowed(ctx context.Context, userID int64, toolName string) (bool, error) {
	info, err := m.ToolPermissionInfo(ctx, userID, toolName)
	if err != nil {
		return false, err
	}
	return info.Allowed, nil
}

// DefaultToolPermission returns the configured default for a tool name.
func (m *Manager) DefaultToolPermission(toolName string) (bool, bool) {
	allowed, ok := m.defaultToolPermissions[strings.TrimSpace(toolName)]
	return allowed, ok
}

// allowedTools returns the tools visible to a user, honoring permissions.
func (m *Manager) allowedTools(ctx context.Context, userID int64) ([]tool.BaseTool, error) {
	all := m.tools.Tools()
	allowed := make([]tool.BaseTool, 0, len(all))
	for _, t := range all {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read tool info: %w", err)
		}
		name := ""
		if info != nil {
			name = info.Name
		}
		ok, err := m.ToolAllowed(ctx, userID, name)
		if err != nil {
			return nil, err
		}
		if ok {
			allowed = append(allowed, t)
		}
	}
	return allowed, nil
}

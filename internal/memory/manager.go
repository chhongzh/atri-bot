package memory

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	MaxMemories      = 32
	MaxContentRunes  = 512
	memoryDateFormat = time.RFC3339
)

//go:embed memory.j2
var memoryTemplate string

type templateMemory struct {
	ID      uint
	Content string
	Date    string
}

// Manager stores and renders user-owned long-term memories.
type Manager struct {
	db       *gorm.DB
	logger   *zap.Logger
	template *prompt.DefaultChatTemplate

	mutationMu sync.Mutex
}

func New(db *gorm.DB, logger *zap.Logger) *Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{
		db:       db,
		logger:   logger,
		template: prompt.FromMessages(schema.Jinja2, schema.SystemMessage(memoryTemplate)),
	}
}

func (m *Manager) Init() error {
	return m.db.AutoMigrate(&model.Memory{})
}

// List returns the current user's memories in stable id order.
func (m *Manager) List(ctx context.Context, userID int64) ([]model.Memory, error) {
	var memories []model.Memory
	err := m.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id ASC").
		Limit(MaxMemories).
		Find(&memories).Error
	return memories, err
}

// Add stores one memory. The generated id is intentionally not returned to
// callers; ids are rendered in the next dynamic memory block instead.
func (m *Manager) Add(ctx context.Context, userID int64, content string) error {
	content, err := normalizeContent(content)
	if err != nil {
		return err
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()

	var count int64
	if err = m.db.WithContext(ctx).Model(&model.Memory{}).
		Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return err
	}
	if count >= MaxMemories {
		return errs.ErrMemoryLimitReached
	}
	if err = m.db.WithContext(ctx).Create(&model.Memory{UserID: userID, Content: content}).Error; err != nil {
		return err
	}
	m.logger.Debug("added user memory", zap.Int64("user_id", userID))
	return nil
}

// Update changes a memory owned by the current user.
func (m *Manager) Update(ctx context.Context, userID int64, id uint, content string) error {
	content, err := normalizeContent(content)
	if err != nil {
		return err
	}
	if id == 0 {
		return errs.ErrMemoryIDRequired
	}
	var memory model.Memory
	if err = m.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		First(&memory).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errs.MemoryNotFound(id)
		}
		return err
	}
	if err = m.db.WithContext(ctx).Model(&memory).
		Updates(map[string]any{"content": content, "updated_at": time.Now()}).Error; err != nil {
		return err
	}
	m.logger.Debug("updated user memory", zap.Int64("user_id", userID), zap.Uint("memory_id", id))
	return nil
}

// Delete removes a memory owned by the current user.
func (m *Manager) Delete(ctx context.Context, userID int64, id uint) error {
	if id == 0 {
		return errs.ErrMemoryIDRequired
	}
	result := m.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.Memory{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errs.MemoryNotFound(id)
	}
	m.logger.Debug("deleted user memory", zap.Int64("user_id", userID), zap.Uint("memory_id", id))
	return nil
}

// Render returns the dynamic system block for the current user's memories.
func (m *Manager) Render(ctx context.Context, userID int64) (*schema.Message, error) {
	memories, err := m.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	items := make([]templateMemory, 0, len(memories))
	for _, memory := range memories {
		items = append(items, templateMemory{
			ID:      memory.ID,
			Content: memory.Content,
			Date:    memory.CreatedAt.Format(memoryDateFormat),
		})
	}
	messages, err := m.template.Format(ctx, map[string]any{
		"Memories":    items,
		"HasMemories": len(items) > 0,
	})
	if err != nil {
		return nil, err
	}
	if len(messages) != 1 {
		return nil, errs.ErrMemoryTemplateNoMessage
	}
	return messages[0], nil
}

// RenderSystemPrompt is an explicit alias for Render when the block is used
// as a system-level prompt.
func (m *Manager) RenderSystemPrompt(ctx context.Context, userID int64) (*schema.Message, error) {
	return m.Render(ctx, userID)
}

func normalizeContent(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", errs.ErrMemoryContentRequired
	}
	if len([]rune(content)) > MaxContentRunes {
		return "", errs.ErrMemoryContentTooLong
	}
	return content, nil
}

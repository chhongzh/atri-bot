package model

import "time"

// Memory is a user-owned long-term memory entry.
type Memory struct {
	ID      uint   `gorm:"primaryKey"`
	UserID  int64  `gorm:"not null;index:idx_memories_user_id"`
	Content string `gorm:"type:text;not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (Memory) TableName() string {
	return "memories"
}

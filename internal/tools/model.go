package tools

import (
	"time"
)

type ConfigRecord struct {
	UserID   int64  `gorm:"primaryKey;autoIncrement:false"`
	ToolName string `gorm:"primaryKey;size:255"`
	Config   string `gorm:"type:json;not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

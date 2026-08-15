package model

import "time"

// ProviderFile is an opaque reference to a file held by an AI provider.
// Media bytes never enter the database or session history.
type ProviderFile struct {
	ID               string    `gorm:"primaryKey;size:36"`
	CreatedAt        time.Time `gorm:"not null"`
	UserID           int64     `gorm:"not null;index"`
	CharacterID      string    `gorm:"size:255;not null;index"`
	AIConfigRevision uint64    `gorm:"not null;index"`
	ProviderFileID   string    `gorm:"size:255;not null"`
	Kind             string    `gorm:"size:16;not null"`
}

func (ProviderFile) TableName() string { return "provider_files" }

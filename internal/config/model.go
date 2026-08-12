package config

import "time"

// Record stores a JSON value. UserID is zero for global configuration.
type Record struct {
	Scope     string `gorm:"primaryKey;size:16"`
	UserID    int64  `gorm:"primaryKey;autoIncrement:false"`
	Key       string `gorm:"primaryKey;size:128"`
	Value     string `gorm:"type:json;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

const globalScope = "global"
const userScope = "user"

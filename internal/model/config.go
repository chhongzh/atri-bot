// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package model

import "time"

// Config stores a JSON value. UserID is zero for global configuration.
type Config struct {
	Scope     string `gorm:"primaryKey;size:16"`
	UserID    int64  `gorm:"primaryKey;autoIncrement:false"`
	Key       string `gorm:"primaryKey;size:128"`
	Value     string `gorm:"type:json;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName keeps the table name stable at config_records.
func (Config) TableName() string {
	return "config_records"
}

const GlobalScope = "global"
const UserScope = "user"

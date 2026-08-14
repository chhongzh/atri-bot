// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package model

import "time"

type ToolConfig struct {
	UserID   int64  `gorm:"primaryKey;autoIncrement:false"`
	ToolName string `gorm:"primaryKey;size:255"`
	Config   string `gorm:"type:json;not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName keeps the table name stable at tool_config_records.
func (ToolConfig) TableName() string {
	return "tool_config_records"
}

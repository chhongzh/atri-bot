// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package model

import "time"

// ToolPermission records an individual user's permission for one tool.
// When no record exists for a user, the configured default applies.
type ToolPermission struct {
	UserID   int64  `gorm:"primaryKey;autoIncrement:false"`
	ToolName string `gorm:"primaryKey;size:255"`
	Allowed  bool   `gorm:"not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

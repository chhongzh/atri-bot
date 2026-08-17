// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package model

import "time"

// MCPProvider records one external MCP server configured by a user.
// Meta and Headers are stored as JSON strings; GORM keeps them in a JSON
// column, and the management tool accesses individual keys through gjson/sjson.
type MCPProvider struct {
	ID     int64  `gorm:"primaryKey;autoIncrement"`
	UserID int64  `gorm:"not null;uniqueIndex:idx_mcp_provider_user_name"`
	Name   string `gorm:"size:255;not null;uniqueIndex:idx_mcp_provider_user_name"`
	URL    string `gorm:"size:2048;not null"`
	Meta   string `gorm:"type:json;not null"`
	Header string `gorm:"type:json;not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName keeps the table name stable at mcp_providers.
func (MCPProvider) TableName() string {
	return "mcp_providers"
}

// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package config

const (
	UserSettingsKey     = "settings"
	RuntimeSettingsKey  = "runtime"
	DefaultImageMaxEdge = 1024
	MaxImageMaxEdge     = 2048
)

type UserSettings struct {
	CharacterID    string `json:"character_id"`
	AIBaseURL      string `json:"ai_base_url"`
	AIAPIKey       string `json:"ai_api_key"`
	AIModel        string `json:"ai_model"`
	AIMaxRounds    int    `json:"ai_max_rounds"`
	AIImageMaxEdge int    `json:"ai_image_max_edge"`
	MCPMaxTools    int    `json:"mcp_max_tools"`
}

type RuntimeSettings struct {
	DefaultMaxRounds       int             `json:"default_max_rounds"`
	DefaultImageMaxEdge    int             `json:"default_image_max_edge"`
	DefaultToolPermissions map[string]bool `json:"default_tool_permissions"`
	MCPDefaultMaxTools     int             `json:"mcp_default_max_tools"`
}

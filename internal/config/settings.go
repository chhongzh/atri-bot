package config

const (
	UserSettingsKey    = "settings"
	RuntimeSettingsKey = "runtime"
)

type UserSettings struct {
	CharacterID      string `json:"character_id"`
	AIBaseURL        string `json:"ai_base_url"`
	AIAPIKey         string `json:"ai_api_key"`
	AIModel          string `json:"ai_model"`
	AIMaxRounds      int    `json:"ai_max_rounds"`
	MCPMaxTools      int    `json:"mcp_max_tools"`
	MCPBlockInternal *bool  `json:"mcp_block_internal"`
}

type RuntimeSettings struct {
	DefaultMaxRounds       int             `json:"default_max_rounds"`
	DefaultToolPermissions map[string]bool `json:"default_tool_permissions"`
	MCPDefaultMaxTools     int             `json:"mcp_default_max_tools"`
	MCPBlockInternal       bool            `json:"mcp_block_internal"`
}

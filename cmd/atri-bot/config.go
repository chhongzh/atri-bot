package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chhongzh/atri-bot/internal/session"
	"github.com/spf13/viper"
)

const (
	defaultMCPMaxTools = 128
	defaultSQLitePath  = "atri-bot.db"
)

// Config 是对应 config.yaml 的语义化运行配置。
type Config struct {
	Telegram TelegramConfig `mapstructure:"telegram"`
	Default  DefaultConfig  `mapstructure:"default"`
	Security SecurityConfig `mapstructure:"security"`
	Database DatabaseConfig `mapstructure:"database"`
	External ExternalConfig `mapstructure:"external"`
	CWD      string         `mapstructure:"atri_cwd"`
}

type TelegramConfig struct {
	BotToken string `mapstructure:"bot_token"`
}

type DefaultConfig struct {
	MaxRounds       int             `mapstructure:"max_rounds"`
	MCPMaxTools     int             `mapstructure:"mcp_max_tools"`
	ToolPermissions map[string]bool `mapstructure:"tool_permissions"`
}

type SecurityConfig struct {
	AllowPrivateIP bool `mapstructure:"allow_private_ip"`
}

type DatabaseConfig struct {
	Type string `mapstructure:"type"`
	DSN  string `mapstructure:"dsn"`
	Path string `mapstructure:"path"`
}

type ExternalConfig struct {
	BrowserURL string `mapstructure:"browser_url"`
}

func getConfig() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")

	v.SetDefault("telegram.bot_token", "")
	v.SetDefault("default.max_rounds", session.DefaultMaxRounds)
	v.SetDefault("default.mcp_max_tools", defaultMCPMaxTools)
	v.SetDefault("default.tool_permissions", map[string]bool{})
	v.SetDefault("security.allow_private_ip", false)
	v.SetDefault("database.type", "sqlite")
	v.SetDefault("database.path", defaultSQLitePath)
	v.SetDefault("external.browser_url", "")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parse configuration: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	c.Telegram.BotToken = strings.TrimSpace(c.Telegram.BotToken)
	if c.Telegram.BotToken == "" {
		return errors.New("required configuration telegram.bot_token is missing")
	}

	if c.Default.MaxRounds <= 0 {
		return errors.New("configuration default.max_rounds must be positive")
	}
	if c.Default.MCPMaxTools <= 0 {
		return errors.New("configuration default.mcp_max_tools must be positive")
	}

	c.Database.DSN = strings.TrimSpace(c.Database.DSN)
	switch c.Database.Type {
	case "sqlite":
	case "mysql":
		if c.Database.DSN == "" {
			return errors.New("configuration database.dsn is required for database.type=mysql")
		}
	default:
		return fmt.Errorf("unsupported database.type %q (supported: sqlite, mysql)", c.Database.Type)
	}

	c.External.BrowserURL = strings.TrimSpace(c.External.BrowserURL)
	return nil
}

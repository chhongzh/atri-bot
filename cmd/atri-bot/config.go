// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"time"

	"github.com/chhongzh/atri-bot/internal/constants"
	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/pkg/errors"
	"github.com/spf13/viper"
)

// Config 是对应 config.yaml 的语义化运行配置。
type Config struct {
	Telegram TelegramConfig `mapstructure:"telegram"`
	Default  DefaultConfig  `mapstructure:"default"`
	Security SecurityConfig `mapstructure:"security"`
	Database DatabaseConfig `mapstructure:"database"`
	External ExternalConfig `mapstructure:"external"`
	Files    FilesConfig    `mapstructure:"files"`
	CWD      string         `mapstructure:"atri_cwd"`
}

type TelegramConfig struct {
	BotToken string `mapstructure:"bot_token"`
}

type DefaultConfig struct {
	MaxRounds       int             `mapstructure:"max_rounds"`
	ImageMaxEdge    int             `mapstructure:"image_max_edge"`
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

type FilesConfig struct {
	MaxStorageMB int    `mapstructure:"max_storage_mb"`
	CleanupAfter string `mapstructure:"cleanup_after"`
	cleanupAfter time.Duration
}

func getConfig() (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")

	v.SetDefault("telegram.bot_token", "")
	v.SetDefault("default.max_rounds", constants.DefaultMaxRounds)
	v.SetDefault("default.image_max_edge", constants.DefaultImageMaxEdge)
	v.SetDefault("default.mcp_max_tools", constants.DefaultMCPMaxTools)
	v.SetDefault("default.tool_permissions", map[string]bool{})
	v.SetDefault("security.allow_private_ip", false)
	v.SetDefault("database.type", "sqlite")
	v.SetDefault("database.path", constants.DefaultSQLitePath)
	v.SetDefault("external.browser_url", "")
	v.SetDefault("files.max_storage_mb", constants.DefaultFilesMaxStorageMB)
	v.SetDefault("files.cleanup_after", constants.DefaultFilesCleanupAfter)

	if err := v.ReadInConfig(); err != nil {
		return nil, errors.Wrap(err, "read configuration")
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, errors.Wrap(err, "parse configuration")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	c.Telegram.BotToken = strings.TrimSpace(c.Telegram.BotToken)
	if c.Telegram.BotToken == "" {
		return errs.ErrConfigBotTokenMissing
	}

	if c.Default.MaxRounds <= 0 {
		return errs.ErrConfigMaxRoundsInvalid
	}
	if c.Default.ImageMaxEdge <= 0 || c.Default.ImageMaxEdge > constants.MaxImageMaxEdge {
		return errs.ConfigImageMaxEdgeInvalid(constants.MaxImageMaxEdge)
	}
	if c.Default.MCPMaxTools <= 0 {
		return errs.ErrConfigMCPMaxToolsInvalid
	}
	if c.Files.MaxStorageMB <= 0 {
		return errs.ErrConfigFilesMaxStorageInvalid
	}
	cleanupAfter, err := parseDuration(c.Files.CleanupAfter)
	if err != nil {
		return errors.Wrap(err, "configuration files.cleanup_after must be a positive duration")
	}
	if cleanupAfter <= 0 {
		return errs.ErrConfigFilesCleanupAfterInvalid
	}
	c.Files.cleanupAfter = cleanupAfter

	c.Database.DSN = strings.TrimSpace(c.Database.DSN)
	switch c.Database.Type {
	case "sqlite":
	case "mysql":
		if c.Database.DSN == "" {
			return errs.ErrConfigMySQLDSNRequired
		}
	default:
		return errs.ConfigDatabaseTypeUnsupported(c.Database.Type)
	}

	c.External.BrowserURL = strings.TrimSpace(c.External.BrowserURL)
	return nil
}

func parseDuration(value string) (time.Duration, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasSuffix(value, "d") {
		hours, err := time.ParseDuration(strings.TrimSuffix(value, "d") + "h")
		if err != nil {
			return 0, err
		}
		return hours * 24, nil
	}
	return time.ParseDuration(value)
}

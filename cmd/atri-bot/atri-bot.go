// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/chhongzh/atri-bot/internal/constants"
	"github.com/chhongzh/atri-bot/internal/runner"
	"github.com/chhongzh/atri-bot/internal/security"
	"github.com/chhongzh/atri-bot/internal/tools"
	"github.com/chhongzh/atri-bot/internal/tools/email"
	"github.com/chhongzh/atri-bot/internal/tools/hotspot"
	"github.com/chhongzh/atri-bot/internal/tools/loadimage"
	"github.com/chhongzh/atri-bot/internal/tools/sendimage"
	"github.com/chhongzh/atri-bot/internal/tools/webread"
	"github.com/chhongzh/atri-bot/internal/tools/websearch"
	"github.com/chhongzh/atri-bot/internal/utils"
	"github.com/glebarez/sqlite"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"resty.dev/v3"
)

func main() {
	app := fx.New(
		fx.Provide(getConfig),
		fx.Provide(getLogger),
		fx.Provide(getDB),
		fx.Provide(getRestyClient),
		fx.Provide(getRod),
		fx.Provide(getRunner),

		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: log}
		}),

		fx.Invoke(run),
	)

	app.Run()
}

func getLogger() (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)

	return cfg.Build()
}

func getDB(cfg *Config, log *zap.Logger) (*gorm.DB, error) {
	gormConfig := &gorm.Config{
		Logger:         utils.NewGormLogger(log),
		TranslateError: true,
	}
	switch cfg.Database.Type {
	case "mysql":
		return gorm.Open(mysql.Open(cfg.Database.DSN), gormConfig)
	default: // sqlite
		root := cfg.CWD
		if root == "" {
			root = "."
		}
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, err
		}

		path := cfg.Database.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		return gorm.Open(sqlite.Open(path), gormConfig)
	}
}

func getRunner(logger *zap.Logger, restyClient *resty.Client, browser *rod.Browser, cfg *Config, db *gorm.DB) *runner.Runner {
	registrars := []tools.Registrar{
		email.BindedRegister(cfg.Security.AllowPrivateIP),
		hotspot.BindedRegister(logger, restyClient),
		loadimage.Register,
		sendimage.Register,
	}
	if browser != nil {
		registrars = append(registrars,
			webread.BindedRegister(logger, browser),
			websearch.BindedRegister(logger, browser))
	}

	return runner.New(logger, &runner.Config{
		BotToken:               cfg.Telegram.BotToken,
		CWD:                    cfg.CWD,
		CharacterRepositoryURL: constants.DefaultCharacterRepositoryURL,
		CharacterBranch:        constants.DefaultCharacterBranch,
		DefaultMaxRounds:       cfg.Default.MaxRounds,
		DefaultImageMaxEdge:    cfg.Default.ImageMaxEdge,
		DefaultToolPermissions: cfg.Default.ToolPermissions,
		MCPDefaultMaxTools:     cfg.Default.MCPMaxTools,
		FilesMaxStorageBytes:   int64(cfg.Files.MaxStorageMB) << 20,
		FilesCleanupAfter:      cfg.Files.cleanupAfter,
		AllowPrivateIP:         cfg.Security.AllowPrivateIP,
		ToolRegistrars:         registrars,
	}, db)
}

func getRestyClient(cfg *Config) *resty.Client {
	client := resty.New().
		SetTransport(security.DefaultSafeHTTPTransport(cfg.Security.AllowPrivateIP)).
		SetHeader("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36")
	return client
}

func getRod(cfg *Config) (*rod.Browser, error) {
	if cfg.External.BrowserURL == "" {
		return nil, nil
	}

	resolvedURL, err := launcher.ResolveURL(cfg.External.BrowserURL)
	if err != nil {
		return nil, err
	}

	b := rod.New().ControlURL(resolvedURL)

	if err := b.Connect(); err != nil {
		return nil, err
	}

	return b, nil
}

func run(r *runner.Runner, lc fx.Lifecycle) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			err := r.Init(ctx)
			if err != nil {
				return err
			}

			go r.Start()

			return nil
		},
		OnStop: func(ctx context.Context) error {
			r.Stop()
			return nil
		},
	})
}

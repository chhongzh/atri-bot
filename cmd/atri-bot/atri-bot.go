package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chhongzh/atri-bot/internal/runner"
	"github.com/chhongzh/atri-bot/internal/session"
	"github.com/chhongzh/atri-bot/internal/tools"
	"github.com/chhongzh/atri-bot/internal/tools/email"
	"github.com/glebarez/sqlite"
	"github.com/spf13/viper"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func main() {
	app := fx.New(
		fx.Provide(getConfig),
		fx.Provide(getLogger),
		fx.Provide(getDB),
		fx.Provide(getRunner),

		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: log}
		}),

		fx.Invoke(run),
	)

	app.Run()
}

type config struct {
	botToken            string
	characterRepoURL    string
	characterRepoBranch string
	cwd                 string
	defaultMaxRounds    int
}

func getLogger() (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)

	return cfg.Build()
}

func getConfig() (*config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}

	defaultMaxRounds := session.DefaultMaxRounds
	if v.IsSet("bot.max_rounds") {
		defaultMaxRounds = v.GetInt("bot.max_rounds")
		if defaultMaxRounds <= 0 {
			return nil, fmt.Errorf("configuration bot.max_rounds must be positive")
		}
	}

	cfg := &config{
		botToken:            v.GetString("telegram.bot_token"),
		characterRepoURL:    v.GetString("character_repository_url"),
		characterRepoBranch: v.GetString("character_repository_branch"),
		cwd:                 v.GetString("atri_cwd"),
		defaultMaxRounds:    defaultMaxRounds,
	}
	if cfg.botToken == "" {
		return nil, fmt.Errorf("required configuration telegram.bot_token is missing")
	}

	return cfg, nil
}

func getDB(cfg *config) (*gorm.DB, error) {
	root := cfg.cwd
	if root == "" {
		root = "."
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return gorm.Open(sqlite.Open(filepath.Join(root, "atri-bot.db")))
}

func getRunner(logger *zap.Logger, cfg *config, db *gorm.DB) *runner.Runner {
	return runner.New(logger, &runner.Config{
		BotToken:               cfg.botToken,
		CWD:                    cfg.cwd,
		CharacterRepositoryURL: cfg.characterRepoURL,
		CharacterBranch:        cfg.characterRepoBranch,
		DefaultMaxRounds:       cfg.defaultMaxRounds,

		ToolRegistrars: []tools.Registrar{
			email.Register,
		},
	}, db)
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

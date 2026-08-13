package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/chhongzh/atri-bot/internal/runner"
	"github.com/chhongzh/atri-bot/internal/security"
	"github.com/chhongzh/atri-bot/internal/session"
	"github.com/chhongzh/atri-bot/internal/tools"
	"github.com/chhongzh/atri-bot/internal/tools/email"
	"github.com/chhongzh/atri-bot/internal/tools/hotspot"
	"github.com/chhongzh/atri-bot/internal/tools/webread"
	"github.com/glebarez/sqlite"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/spf13/viper"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
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

type config struct {
	botToken               string
	characterRepoURL       string
	characterRepoBranch    string
	cwd                    string
	defaultMaxRounds       int
	defaultToolPermissions map[string]bool
	allowPrivateIP         bool
	mcpDefaultMaxTools     int
	webreadBrowserURL      string
}

type toolsConfig struct {
	DefaultPermissions map[string]bool `mapstructure:"default_permissions"`
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
	var toolsCfg toolsConfig
	if err := v.UnmarshalKey("tools", &toolsCfg); err != nil {
		return nil, fmt.Errorf("configuration tools: %w", err)
	}

	mcpDefaultMaxTools := 128
	if v.IsSet("mcp.max_tools") {
		mcpDefaultMaxTools = v.GetInt("mcp.max_tools")
		if mcpDefaultMaxTools <= 0 {
			return nil, fmt.Errorf("configuration mcp.max_tools must be positive")
		}
	}
	allowPrivateIP := v.GetBool("network.allow_private_ip")

	webreadBrowserURL := ""
	if v.IsSet("webread.browser_url") {
		webreadBrowserURL = strings.TrimSpace(v.GetString("webread.browser_url"))
	}

	cfg := &config{
		botToken:               v.GetString("telegram.bot_token"),
		characterRepoURL:       v.GetString("character_repository_url"),
		characterRepoBranch:    v.GetString("character_repository_branch"),
		cwd:                    v.GetString("atri_cwd"),
		defaultMaxRounds:       defaultMaxRounds,
		defaultToolPermissions: toolsCfg.DefaultPermissions,
		allowPrivateIP:         allowPrivateIP,
		mcpDefaultMaxTools:     mcpDefaultMaxTools,
		webreadBrowserURL:      webreadBrowserURL,
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

func getRunner(logger *zap.Logger, restyClient *resty.Client, browser *rod.Browser, cfg *config, db *gorm.DB) *runner.Runner {
	registrars := []tools.Registrar{
		email.BindedRegister(cfg.allowPrivateIP),
		hotspot.BindedRegister(logger, restyClient),
	}
	if browser != nil {
		registrars = append(registrars, webread.BindedRegister(logger, browser))
	}

	return runner.New(logger, &runner.Config{
		BotToken:               cfg.botToken,
		CWD:                    cfg.cwd,
		CharacterRepositoryURL: cfg.characterRepoURL,
		CharacterBranch:        cfg.characterRepoBranch,
		DefaultMaxRounds:       cfg.defaultMaxRounds,
		DefaultToolPermissions: cfg.defaultToolPermissions,
		MCPDefaultMaxTools:     cfg.mcpDefaultMaxTools,
		AllowPrivateIP:         cfg.allowPrivateIP,
		ToolRegistrars:         registrars,
	}, db)
}

func getRestyClient(cfg *config) *resty.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	client := resty.New().
		SetTransport(security.NewSafeHTTPTransport(transport, cfg.allowPrivateIP)).
		SetHeader("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36")
	return client
}

func getRod(cfg *config) (*rod.Browser, error) {
	if cfg.webreadBrowserURL == "" {
		return nil, nil
	}

	resolvedURL, err := launcher.ResolveURL(cfg.webreadBrowserURL)
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

// Atri Bot
// 一个Telegram聊天机器人
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/chhongzh/atri-core"
	"github.com/glebarez/sqlite"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Config 结构体用于映射配置文件
type Config struct {
	Telegram struct {
		BotToken string `mapstructure:"bot_token"`
	} `mapstructure:"telegram"`
	OpenAI struct {
		BaseURL string `mapstructure:"base_url"`
		APIKey  string `mapstructure:"api_key"`
		Model   string `mapstructure:"model"`
	} `mapstructure:"openai"`
	Bot struct {
		SystemPrompt string `mapstructure:"system_prompt"`
		MaxRounds    int    `mapstructure:"max_rounds"`
	} `mapstructure:"bot"`
}

const configFileName = "config.yaml"

//go:embed Atri.md
var systemPrompt string

var errConfigTemplateCreated = errors.New("config template created")

// Build Info
var (
	BuildVersion = "dev"
	BuildCommit  = ""
	BuildDate    = ""
)

func main() {
	logger := initLogger()
	defer logger.Sync()

	if err := bootstrapConfig(logger); err != nil {
		if errors.Is(err, errConfigTemplateCreated) {
			logger.Info("配置模板已生成，请修改后重新运行", zap.String("file", configFileName))
			return
		}
		logger.Fatal("配置引导失败", zap.Error(err))
	}

	var cfg Config
	if err := loadConfig(logger, &cfg); err != nil {
		logger.Fatal("加载配置失败", zap.Error(err))
	}

	db, err := initDB()
	if err != nil {
		logger.Fatal("初始化数据库失败", zap.Error(err))
	}

	openaiClient := openai.NewClient(
		option.WithBaseURL(cfg.OpenAI.BaseURL),
		option.WithAPIKey(cfg.OpenAI.APIKey),
	)

	ctx := context.Background()
	atriCfg := atri.Config{
		Model:        cfg.OpenAI.Model,
		MaxRounds:    cfg.Bot.MaxRounds,
		SystemPrompt: systemPrompt,
	}

	core := atri.New(ctx, logger, &openaiClient, db, cfg.Telegram.BotToken, atriCfg)

	logger.Info(
		"正在启动 Atri Bot...",
		zap.String("version", BuildVersion),
		zap.String("commit", BuildCommit),
		zap.String("date", BuildDate),
	)
	ch, err := core.Start()
	if err != nil {
		logger.Fatal("启动 Atri 失败", zap.Error(err))
	}

	logger.Info("Atri Bot 已启动，按 Ctrl+C 停止")

	<-ch

}

func initLogger() *zap.Logger {
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zap.DebugLevel)

	logger, err := config.Build()
	if err != nil {
		panic(fmt.Sprintf("初始化 logger 失败: %v", err))
	}
	return logger
}

func bootstrapConfig(logger *zap.Logger) error {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			logger.Info("未找到配置文件，正在创建模板", zap.String("file", configFileName))
			if err := createDefaultConfig(logger); err != nil {
				return err
			}
			logger.Info("配置模板创建完成", zap.String("file", configFileName))
			return errConfigTemplateCreated
		}
		return err
	}
	return nil
}

func createDefaultConfig(logger *zap.Logger) error {
	viper.Set("telegram.bot_token", "YOUR_BOT_TOKEN")
	viper.Set("openai.base_url", "https://api.openai.com/v1")
	viper.Set("openai.api_key", "YOUR_API_KEY")
	viper.Set("openai.model", "gpt-4o-mini")
	viper.Set("bot.max_rounds", 16)

	if err := viper.SafeWriteConfigAs(configFileName); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

func loadConfig(logger *zap.Logger, cfg *Config) error {
	if err := viper.Unmarshal(cfg); err != nil {
		return err
	}
	logger.Info("配置加载成功")
	return nil
}

func initDB() (*gorm.DB, error) {
	dbPath := "atri-bot.db"
	db, err := gorm.Open(sqlite.Open(dbPath))
	if err != nil {
		return nil, err
	}

	return db, nil
}

// Atri Bot
// 一个Telegram聊天机器人
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chhongzh/atri-core"
	"github.com/glebarez/sqlite"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/spf13/viper"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm"
	"resty.dev/v3"
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
	ctx := context.Background()

	commonMain(ctx)
}

func commonMain(ctx context.Context) /* isSuccess */ bool {
	logger, err := initLogger()
	if err != nil {
		panic(fmt.Sprintf("初始化日志记录器失败: %v", err))
	}
	defer logger.Sync()

	needUpdate, err := shouldUpdate(logger)
	if err != nil {
		logger.Warn("版本更新检查失败, 请手动检查.", zap.Error(err))
	}
	if needUpdate {
		logger.Info("有新版本, 建议更新.")
	}

	if err := bootstrapConfig(logger); err != nil {
		if errors.Is(err, errConfigTemplateCreated) {
			logger.Info("配置模板已生成，请修改后重新运行", zap.String("file", configFileName))
			return false
		}
		exitWithError(logger, "配置引导失败", err)
		return false
	}

	var cfg Config
	if err := loadConfig(logger, &cfg); err != nil {
		exitWithError(logger, "加载配置失败", err)
		return false
	}

	db, err := initDB()
	if err != nil {
		exitWithError(logger, "初始化数据库失败", err)
		return false
	}

	openaiClient := openai.NewClient(
		option.WithBaseURL(cfg.OpenAI.BaseURL),
		option.WithAPIKey(cfg.OpenAI.APIKey),
	)

	atriCfg := atri.Config{
		Model:            cfg.OpenAI.Model,
		MaxRounds:        cfg.Bot.MaxRounds,
		SystemPrompt:     systemPrompt,
		CheckInitTimeout: time.Second * 114514, // 一个十分长的超时时间 防止报错
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
		exitWithError(logger, "启动 Atri 失败", err)
		return false
	}

	logger.Info("Atri Bot 已启动，按 Ctrl+C 停止")

	<-ch
	return true
}

func initLogger() (*zap.Logger, error) {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	consoleCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.AddSync(os.Stdout),
		zap.DebugLevel,
	)

	logFile, err := os.OpenFile("atri-bot.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	fileCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.AddSync(logFile),
		zap.DebugLevel,
	)

	core := zapcore.NewTee(consoleCore, fileCore)

	logger := zap.New(core)
	return logger, nil
}

func printFriendlyError(logger *zap.Logger, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		logger.Info("连接超时! Tips: 请检查代理配置, 并确认本机可以直接访问 Telegram.")
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "unauthorized") {
		logger.Info("Tips:Bot Token 看起来不正确, 请检查 telegram.bot_token 配置是否填写正确.")
	}
}

func bootstrapConfig(logger *zap.Logger) error {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			logger.Info("未找到配置文件，正在创建模板", zap.String("file", configFileName))
			if err := createDefaultConfig(); err != nil {
				return err
			}
			logger.Info("配置模板创建完成", zap.String("file", configFileName))
			return errConfigTemplateCreated
		}
		return err
	}
	return nil
}

func createDefaultConfig() error {
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

func shouldUpdate(logger *zap.Logger) (bool, error) {
	r := resty.New()
	resp, err := r.R().SetTimeout(time.Second * 10).Get("https://api.github.com/repos/chhongzh/atri-bot/releases/latest")
	if err != nil {
		return false, err
	}

	if !resp.IsSuccess() {
		return false, fmt.Errorf("api error: <%d>%s", resp.StatusCode(), resp.Status())
	}

	remoteVersion := gjson.GetBytes(resp.Bytes(), "tag_name").String()

	logger.Debug("版本检查", zap.String("Local", BuildVersion), zap.String("Remote", remoteVersion))

	return remoteVersion != BuildVersion, nil
}

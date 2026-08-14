package utils

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm/logger"
)

// gormSlowThreshold 默认的 GORM 慢查询阈值。
const gormSlowThreshold = 200 * time.Millisecond

// GormLogger 使用 zap 输出 GORM 日志。SQL 参数默认不展开，
// 避免将用户数据或密钥记录到日志中。
type GormLogger struct {
	logger *zap.Logger
	config logger.Config
}

// NewGormLogger 返回以 zap 为输出后端的 GORM logger。
func NewGormLogger(log *zap.Logger) logger.Interface {
	return &GormLogger{
		logger: log,
		config: logger.Config{
			SlowThreshold:             gormSlowThreshold,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
			LogLevel:                  logger.Warn,
		},
	}
}

// LogMode 返回带指定日志级别的新实例。
func (l *GormLogger) LogMode(level logger.LogLevel) logger.Interface {
	next := *l
	next.config.LogLevel = level
	return &next
}

// Info 输出 GORM 信息日志。
func (l *GormLogger) Info(_ context.Context, msg string, data ...interface{}) {
	if l.config.LogLevel < logger.Info {
		return
	}
	l.logger.Info("gorm: "+msg, gormFields(data...)...)
}

// Warn 输出 GORM 警告日志。
func (l *GormLogger) Warn(_ context.Context, msg string, data ...interface{}) {
	if l.config.LogLevel < logger.Warn {
		return
	}
	l.logger.Warn("gorm: "+msg, gormFields(data...)...)
}

// Error 输出 GORM 错误日志。
func (l *GormLogger) Error(_ context.Context, msg string, data ...interface{}) {
	if l.config.LogLevel < logger.Error {
		return
	}
	l.logger.Error("gorm: "+msg, gormFields(data...)...)
}

// ParamsFilter 在 ParameterizedQueries 开启时剥离 SQL 参数，防止敏感值进入日志。
func (l *GormLogger) ParamsFilter(_ context.Context, sql string, params ...interface{}) (string, []interface{}) {
	if l.config.ParameterizedQueries {
		return sql, nil
	}
	return sql, params
}

// Trace 输出 SQL 执行日志：失败为 error、慢查询为 warn、成功为 debug。
func (l *GormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.config.LogLevel <= logger.Silent {
		return
	}
	elapsed := time.Since(begin)
	sql, rows := fc()
	fields := []zap.Field{
		zap.String("sql", sql),
		zap.Int64("rows", rows),
		zap.Duration("elapsed", elapsed),
	}
	switch {
	case err != nil && l.config.LogLevel >= logger.Error && (!errors.Is(err, logger.ErrRecordNotFound) || !l.config.IgnoreRecordNotFoundError):
		l.logger.Error("gorm query failed", append(fields, zap.Error(err))...)
	case l.config.SlowThreshold != 0 && elapsed > l.config.SlowThreshold && l.config.LogLevel >= logger.Warn:
		l.logger.Warn("gorm slow query", fields...)
	case l.config.LogLevel == logger.Info:
		l.logger.Debug("gorm query", fields...)
	}
}

func gormFields(data ...interface{}) []zap.Field {
	if len(data) == 0 {
		return nil
	}
	fields := make([]zap.Field, 0, len(data)/2+1)
	for index := 0; index < len(data); index++ {
		if key, ok := data[index].(string); ok && index+1 < len(data) {
			fields = append(fields, zap.Any(key, data[index+1]))
			index++
			continue
		}
		fields = append(fields, zap.Any("value", data[index]))
	}
	return fields
}

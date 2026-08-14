package utils

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm/logger"
)

func TestGormLoggerLevelMapping(t *testing.T) {
	core, recorded := observer.New(zap.DebugLevel)
	gormLogger := NewGormLogger(zap.New(core)).(*GormLogger)
	gormLogger.config.LogLevel = logger.Info
	ctx := context.Background()

	gormLogger.Info(ctx, "migrated table")
	gormLogger.Warn(ctx, "duplicated index")
	gormLogger.Error(ctx, "migration failed")
	gormLogger.Trace(ctx, time.Now(), func() (string, int64) {
		return "SELECT * FROM users WHERE id = ?", 1
	}, nil)
	gormLogger.Trace(ctx, time.Now().Add(-time.Second), func() (string, int64) {
		return "SELECT * FROM users", 1
	}, nil)
	gormLogger.Trace(ctx, time.Now(), func() (string, int64) {
		return "UPDATE users SET name = ?", 1
	}, errors.New("update failed"))

	wantLevels := []zapcore.Level{zap.InfoLevel, zap.WarnLevel, zap.ErrorLevel, zap.DebugLevel, zap.WarnLevel, zap.ErrorLevel}
	if len(recorded.All()) != len(wantLevels) {
		t.Fatalf("log entries = %d, want %d: %#v", len(recorded.All()), len(wantLevels), recorded.All())
	}
	for index, entry := range recorded.All() {
		if entry.Level != wantLevels[index] {
			t.Fatalf("entry %d level = %v, want %v", index, entry.Level, wantLevels[index])
		}
	}
}

func TestGormLoggerStripsParams(t *testing.T) {
	gormLogger := NewGormLogger(zap.NewNop()).(*GormLogger)
	sql, params := gormLogger.ParamsFilter(context.Background(), "SELECT * FROM users WHERE id = ?", 42, "secret")
	if sql != "SELECT * FROM users WHERE id = ?" {
		t.Fatalf("sql = %q", sql)
	}
	if len(params) != 0 {
		t.Fatalf("params = %#v, want empty", params)
	}
}

package tools

import (
	"context"
	"reflect"
	"testing"

	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type databaseTestConfig struct {
	Value string `json:"value"`
}

func TestToolConfigUsesExplicitUpsert(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db, zap.NewNop())
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	manager.registered["database_test"] = &registeredTool{
		configType:    reflect.TypeOf(databaseTestConfig{}),
		defaultConfig: []byte(`{"value":"default"}`),
	}

	ctx := context.Background()
	data, err := manager.ConfigJSON(ctx, 7, "database_test")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"value":"default"}` {
		t.Fatalf("default config = %s", data)
	}
	if err = manager.SetConfig(ctx, 7, "database_test", []byte(`{"value":"custom"}`)); err != nil {
		t.Fatal(err)
	}
	data, err = manager.ConfigJSON(ctx, 7, "database_test")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"value":"custom"}` {
		t.Fatalf("updated config = %s", data)
	}

	var count int64
	if err = db.Model(&model.ToolConfig{}).
		Where("user_id = ? AND tool_name = ?", 7, "database_test").
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tool config rows = %d, want 1", count)
	}
}

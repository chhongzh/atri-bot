package chat

import (
	"context"
	"testing"

	"github.com/chhongzh/atri-bot/internal/model"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestToolPermissionUsesExplicitUpsert(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.ToolPermission{}); err != nil {
		t.Fatal(err)
	}
	tools := toolmanager.New(db, zap.NewNop())
	if err = tools.RegisterPermission("mcp"); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{db: db, logger: zap.NewNop(), tools: tools}
	ctx := context.Background()

	if err = manager.SetToolPermission(ctx, 7, "mcp", false); err != nil {
		t.Fatal(err)
	}
	if err = manager.SetToolPermission(ctx, 7, "mcp", true); err != nil {
		t.Fatal(err)
	}
	var records []model.ToolPermission
	if err = db.Where("user_id = ? AND tool_name = ?", 7, "mcp").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || !records[0].Allowed {
		t.Fatalf("tool permission rows = %#v", records)
	}
}

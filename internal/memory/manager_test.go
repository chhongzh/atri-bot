package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestManagerMutationsAndDynamicRender(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db, zap.NewNop())
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = manager.Add(ctx, 7, "用户喜欢喝茶"); err != nil {
		t.Fatal(err)
	}
	if err = manager.Add(ctx, 7, "用户住在上海"); err != nil {
		t.Fatal(err)
	}
	if err = manager.Add(ctx, 8, "用户属于另一个账户"); err != nil {
		t.Fatal(err)
	}

	memories, err := manager.List(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 2 || memories[0].ID == 0 || memories[1].ID <= memories[0].ID {
		t.Fatalf("memories = %#v", memories)
	}
	firstID := memories[0].ID
	if err = manager.Update(ctx, 7, firstID, "用户喜欢喝咖啡"); err != nil {
		t.Fatal(err)
	}
	if err = manager.Delete(ctx, 7, memories[1].ID); err != nil {
		t.Fatal(err)
	}
	if err = manager.Delete(ctx, 7, memories[1].ID); !errors.Is(err, errs.ErrMemoryNotFound) {
		t.Fatalf("second delete error = %v", err)
	}
	if err = manager.Update(ctx, 7, 3, "用户不应越权"); !errors.Is(err, errs.ErrMemoryNotFound) {
		t.Fatalf("missing update error = %v", err)
	}

	rendered, err := manager.Render(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.Role != "system" || !strings.Contains(rendered.Content, "<memory>") ||
		!strings.Contains(rendered.Content, "用户喜欢喝咖啡") || strings.Contains(rendered.Content, "用户喜欢喝茶") {
		t.Fatalf("rendered memory = %q", rendered.Content)
	}
	if !strings.Contains(rendered.Content, "1.") {
		t.Fatalf("rendered memory id missing = %q", rendered.Content)
	}
}

func TestManagerEnforcesMemoryLimit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db, zap.NewNop())
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < MaxMemories; index++ {
		if err = manager.Add(context.Background(), 7, "用户记忆"); err != nil {
			t.Fatalf("add memory %d: %v", index, err)
		}
	}
	if err = manager.Add(context.Background(), 7, "用户超出限制"); !errors.Is(err, errs.ErrMemoryLimitReached) {
		t.Fatalf("limit error = %v", err)
	}
	var count int64
	if err = db.Model(&model.Memory{}).Where("user_id = ?", 7).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != MaxMemories {
		t.Fatalf("memory rows = %d", count)
	}
}

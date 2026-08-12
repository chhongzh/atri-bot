package account

import (
	"context"
	"testing"

	configmanager "github.com/chhongzh/atri-bot/internal/config"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestUserSettingsAreStoredSeparatelyFromAccounts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	configs := configmanager.New(db)
	manager := New(db, zap.NewNop(), configs, 36)
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err = manager.EnsureUser(ctx, 1, "one", "One"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = manager.EnsureUser(ctx, 2, "two", "Two"); err != nil {
		t.Fatal(err)
	}
	if err = manager.SetAIModel(ctx, 1, "model-one"); err != nil {
		t.Fatal(err)
	}

	first, err := manager.Settings(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Settings(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.AIModel != "model-one" {
		t.Fatalf("first user model = %q", first.AIModel)
	}
	if second.AIModel != "" || second.AIMaxRounds != 36 {
		t.Fatalf("second user settings = %#v", second)
	}
}

package character

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestInitUpsertsLocalProviderFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	manager := New(db, zap.NewNop(), Config{CWD: root})
	ctx := context.Background()
	if err = manager.Init(ctx); err != nil {
		t.Fatal(err)
	}

	if err = db.Model(&model.Provider{}).Where("id = ?", localProviderID).Updates(map[string]any{
		"kind":     model.ProviderRemote,
		"url":      "https://invalid.example/repository",
		"branch":   "invalid",
		"path":     "invalid",
		"built_in": false,
		"enabled":  false,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err = manager.Init(ctx); err != nil {
		t.Fatal(err)
	}

	var provider model.Provider
	if err = db.Where("id = ?", localProviderID).Take(&provider).Error; err != nil {
		t.Fatal(err)
	}
	if provider.Kind != model.ProviderLocal || provider.URL != "" || provider.Branch != "" ||
		provider.Path != filepath.Join(root, "chardefs") || !provider.BuiltIn || !provider.Enabled {
		t.Fatalf("local provider = %#v", provider)
	}
}

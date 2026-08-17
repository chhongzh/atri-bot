// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package account

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	configmanager "github.com/chhongzh/atri-bot/internal/config"
	"github.com/chhongzh/atri-bot/internal/constants"
	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func newTestManager(t *testing.T) (*Manager, *gorm.DB) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	configs := configmanager.New(db)
	manager := New(db, zap.NewNop(), configs, constants.DefaultMaxRounds, constants.DefaultImageMaxEdge)
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	return manager, db
}

func TestUserSettingsAreStoredSeparatelyFromAccounts(t *testing.T) {
	manager, _ := newTestManager(t)
	ctx := context.Background()
	if _, _, err := manager.EnsureUser(ctx, 1, "one", "One"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.EnsureUser(ctx, 2, "two", "Two"); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetAIModel(ctx, 1, "model-one"); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetAIImageMaxEdge(ctx, 1, 1536); err != nil {
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
	if first.AIModel != "model-one" || first.AIImageMaxEdge != 1536 {
		t.Fatalf("first user settings = %#v", first)
	}
	if second.AIModel != "" || second.AIMaxRounds != constants.DefaultMaxRounds || second.AIImageMaxEdge != constants.DefaultImageMaxEdge {
		t.Fatalf("second user settings = %#v", second)
	}
}

func TestConcurrentAdminDemotionsKeepOneActiveAdmin(t *testing.T) {
	manager, db := newTestManager(t)
	ctx := context.Background()
	if _, _, err := manager.EnsureUser(ctx, 1, "one", "One"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.EnsureUser(ctx, 2, "two", "Two"); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetRole(ctx, 1, 2, model.RoleAdmin); err != nil {
		t.Fatal(err)
	}

	errors := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(2)
	for _, change := range []struct {
		actorID  int64
		targetID int64
	}{{actorID: 1, targetID: 2}, {actorID: 2, targetID: 1}} {
		go func() {
			start.Done()
			start.Wait()
			errors <- manager.SetRole(ctx, change.actorID, change.targetID, model.RoleUser)
		}()
	}
	for range 2 {
		<-errors
	}

	var activeAdmins int64
	if err := db.Model(&model.User{}).
		Where("role = ? AND banned = ?", model.RoleAdmin, false).
		Count(&activeAdmins).Error; err != nil {
		t.Fatal(err)
	}
	if activeAdmins != 1 {
		t.Fatalf("active admins = %d, want 1", activeAdmins)
	}
}

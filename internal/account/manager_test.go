package account

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestFirstUserAndAdminSafetyRules(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db, zap.NewNop(), 24)
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, _, err := manager.EnsureUser(ctx, 1, "first", "First")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := manager.EnsureUser(ctx, 2, "second", "Second")
	if err != nil {
		t.Fatal(err)
	}
	if first.Role != RoleAdmin || second.Role != RoleUser {
		t.Fatalf("unexpected roles: first=%s second=%s", first.Role, second.Role)
	}
	if err = manager.SetRole(ctx, 1, 1, RoleUser); !errors.Is(err, ErrSelfDemotion) {
		t.Fatalf("self demotion error = %v", err)
	}
	if err = manager.SetBanned(ctx, 1, 1, true); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin ban error = %v", err)
	}
	if err = manager.Delete(ctx, 1, 1); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin delete error = %v", err)
	}
	if err = manager.SetRole(ctx, 1, 2, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err = manager.SetRole(ctx, 2, 1, RoleUser); err != nil {
		t.Fatalf("demoting an admin with another active admin should work: %v", err)
	}
}

func TestAIConfigIsStoredPerUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db, zap.NewNop(), 24)
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err = manager.EnsureUser(ctx, 1, "first", "First"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = manager.EnsureUser(ctx, 2, "second", "Second"); err != nil {
		t.Fatal(err)
	}
	if err = manager.SetAIBaseURL(ctx, 1, "https://one.example/v1"); err != nil {
		t.Fatal(err)
	}
	if err = manager.SetAIAPIKey(ctx, 1, "key-one"); err != nil {
		t.Fatal(err)
	}
	if err = manager.SetAIModel(ctx, 1, "model-one"); err != nil {
		t.Fatal(err)
	}
	first, err := manager.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Get(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.AIBaseURL != "https://one.example/v1" || first.AIAPIKey != "key-one" || first.AIModel != "model-one" {
		t.Fatalf("first user AI config = %#v", first)
	}
	if second.AIBaseURL != "" || second.AIAPIKey != "" || second.AIModel != "" {
		t.Fatalf("second user unexpectedly inherited AI config: %#v", second)
	}
	if first.AIMaxRounds != 24 || second.AIMaxRounds != 24 {
		t.Fatalf("default AI max rounds = (%d, %d), want 24", first.AIMaxRounds, second.AIMaxRounds)
	}
	if err = manager.SetAIMaxRounds(ctx, 1, 12); err != nil {
		t.Fatal(err)
	}
	first, err = manager.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err = manager.Get(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.AIMaxRounds != 12 || second.AIMaxRounds != 24 {
		t.Fatalf("per-user AI max rounds = (%d, %d), want (12, 24)", first.AIMaxRounds, second.AIMaxRounds)
	}
}

func TestVerboseIsStoredPerUserAndDefaultsOff(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db, zap.NewNop(), 24)
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err = manager.EnsureUser(ctx, 1, "first", "First"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = manager.EnsureUser(ctx, 2, "second", "Second"); err != nil {
		t.Fatal(err)
	}
	first, err := manager.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Verbose {
		t.Fatal("verbose must default to off")
	}
	if err = manager.SetVerbose(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	first, err = manager.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Get(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Verbose {
		t.Fatal("verbose should be enabled for user 1")
	}
	if second.Verbose {
		t.Fatal("verbose must not leak to user 2")
	}
	if err = manager.SetVerbose(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	first, err = manager.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Verbose {
		t.Fatal("verbose should be disabled again")
	}
}

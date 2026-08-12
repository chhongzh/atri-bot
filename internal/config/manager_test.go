package config

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db)
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestQueryRoundTripsJSONValues(t *testing.T) {
	ctx := context.Background()
	manager := newTestManager(t)

	if err := manager.Set(ctx, "string", "123"); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetUser(ctx, 7, "strings", []string{"1", "2", "3"}); err != nil {
		t.Fatal(err)
	}

	value, err := manager.Query[string](ctx, "string")
	if err != nil || value != "123" {
		t.Fatalf("global string = %q, %v", value, err)
	}
	values, err := manager.QueryUser[[]string](ctx, 7, "strings")
	if err != nil || !reflect.DeepEqual(values, []string{"1", "2", "3"}) {
		t.Fatalf("user strings = %#v, %v", values, err)
	}
	if _, err = manager.QueryUser[string](ctx, 8, "strings"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("isolated query error = %v, want ErrNotFound", err)
	}
}

func TestSetOverwritesExistingValues(t *testing.T) {
	ctx := context.Background()
	manager := newTestManager(t)

	if err := manager.Set(ctx, "runtime", "first"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Set(ctx, "runtime", "second"); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetUser(ctx, 7, "model", "first"); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetUser(ctx, 7, "model", "second"); err != nil {
		t.Fatal(err)
	}

	global, err := manager.Query[string](ctx, "runtime")
	if err != nil || global != "second" {
		t.Fatalf("global value = %q, %v", global, err)
	}
	user, err := manager.QueryUser[string](ctx, 7, "model")
	if err != nil || user != "second" {
		t.Fatalf("user value = %q, %v", user, err)
	}
}

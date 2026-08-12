package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chhongzh/atri-bot/internal/account"
	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func newTestManager(t *testing.T, cfg Config) (*Manager, *account.Manager) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	accounts := account.New(db, zap.NewNop(), 36)
	if err = accounts.Init(); err != nil {
		t.Fatal(err)
	}
	manager := New(context.Background(), zap.NewNop(), db, accounts, cfg)
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	ensureUsers(t, accounts, 1, 2, 7, 42)
	return manager, accounts
}

func ensureUsers(t *testing.T, accounts *account.Manager, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if _, _, err := accounts.EnsureUser(context.Background(), id, "", ""); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAddListGetRemoveWithUserIsolation(t *testing.T) {
	manager, _ := newTestManager(t, Config{BlockInternal: false})
	ctx := context.Background()

	added, err := manager.Add(ctx, 1, "weather", "https://weather.example/sse", `{"timeout":30}`, `{"Authorization":"Bearer secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	if added.URL != "https://weather.example/sse" {
		t.Fatalf("url = %q", added.URL)
	}
	if _, err = manager.Add(ctx, 1, "weather", "https://other.example/sse", "", ""); !errors.Is(err, errs.ErrProviderExists) {
		t.Fatalf("duplicate add error = %v, want ErrProviderExists", err)
	}
	if _, err = manager.Add(ctx, 2, "weather", "https://other.example/sse", "", ""); err != nil {
		t.Fatalf("same name for another user should be allowed: %v", err)
	}

	providers, err := manager.List(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Name != "weather" {
		t.Fatalf("user 1 providers = %#v", providers)
	}
	got, err := manager.Get(ctx, 1, "weather")
	if err != nil {
		t.Fatal(err)
	}
	if got.Header != `{"Authorization":"Bearer secret"}` {
		t.Fatalf("header = %q", got.Header)
	}
	if err = manager.Remove(ctx, 1, "weather"); err != nil {
		t.Fatal(err)
	}
	if err = manager.Remove(ctx, 1, "weather"); !errors.Is(err, errs.ErrProviderNotFound) {
		t.Fatalf("second remove error = %v, want ErrProviderNotFound", err)
	}
}

func TestAddValidatesURLAndJSON(t *testing.T) {
	manager, _ := newTestManager(t, Config{BlockInternal: true})
	ctx := context.Background()

	tests := []struct {
		name    string
		url     string
		meta    string
		header  string
		wantErr error
	}{
		{name: "internal suffix", url: "http://service.internal/sse", wantErr: errs.ErrInternalHostBlocked},
		{name: "localhost", url: "http://localhost:8080/sse", wantErr: errs.ErrInternalHostBlocked},
		{name: "loopback ip", url: "http://127.0.0.1:12345/sse", wantErr: errs.ErrInternalHostBlocked},
		{name: "private ip", url: "http://192.168.1.10/sse", wantErr: errs.ErrInternalHostBlocked},
		{name: "non-http scheme", url: "ftp://example.com/sse", wantErr: errs.ErrInvalidScheme},
		{name: "bad json meta", url: "https://example.com/sse", meta: "not-json", wantErr: errs.ErrInvalidJSON},
		{name: "array header", url: "https://example.com/sse", header: `["a"]`, wantErr: errs.ErrInvalidJSON},
		{name: "header injection", url: "https://example.com/sse", header: "{\"Authorization\":\"secret\\r\\nInjected: true\"}", wantErr: errs.ErrInvalidJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.Add(ctx, 1, "p", tt.url, tt.meta, tt.header)
			if tt.wantErr == nil {
				if err == nil {
					return
				}
				t.Fatalf("Add() error = %v, want success", err)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Add() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestAddEnforcesProviderLimit(t *testing.T) {
	manager, accounts := newTestManager(t, Config{DefaultMaxTools: 2, BlockInternal: false})
	ctx := context.Background()
	if _, err := manager.Add(ctx, 1, "a", "https://a.example/sse", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Add(ctx, 1, "b", "https://b.example/sse", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Add(ctx, 1, "c", "https://c.example/sse", "", ""); !errors.Is(err, errs.ErrProviderLimit) {
		t.Fatalf("third add error = %v, want ErrProviderLimit", err)
	}

	if err := accounts.SetMCPMaxTools(ctx, 1, 1, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Add(ctx, 1, "c", "https://c.example/sse", "", ""); err != nil {
		t.Fatalf("add after per-user raise failed: %v", err)
	}
}

func TestValueAndSetValueViaJSONPath(t *testing.T) {
	manager, _ := newTestManager(t, Config{BlockInternal: false})
	ctx := context.Background()
	if _, err := manager.Add(ctx, 7, "git", "https://git.example/sse",
		`{"timeout":30}`, `{"Authorization":"Bearer old"}`); err != nil {
		t.Fatal(err)
	}

	value, err := manager.Value(ctx, 7, "git", "header.Authorization")
	if err != nil {
		t.Fatal(err)
	}
	if value != "Bearer old" {
		t.Fatalf("header value = %#v", value)
	}

	updated, err := manager.SetValue(ctx, 7, "git", "header.Authorization", "Bearer new")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "Bearer new" {
		t.Fatalf("updated value = %#v", updated)
	}
	value, err = manager.Value(ctx, 7, "git", "header.Authorization")
	if err != nil {
		t.Fatal(err)
	}
	if value != "Bearer new" {
		t.Fatalf("header after update = %#v", value)
	}

	if _, err = manager.SetValue(ctx, 7, "git", "header.Authorization", 42); !errors.Is(err, errs.ErrInvalidJSON) {
		t.Fatalf("non-string header error = %v, want ErrInvalidJSON", err)
	}
	if _, err = manager.SetValue(ctx, 7, "git", "name", "renamed"); !errors.Is(err, errs.ErrPathForbidden) {
		t.Fatalf("name update error = %v, want ErrPathForbidden", err)
	}
	if value, err = manager.SetValue(ctx, 7, "git", "meta.missing", 1); err != nil || value != float64(1) {
		t.Fatalf("add nested meta value = %#v, %v", value, err)
	}
	if _, err = manager.SetValue(ctx, 7, "git", "meta.timeout", 60); err != nil {
		t.Fatalf("meta update failed: %v", err)
	}
	value, err = manager.Value(ctx, 7, "git", "meta.timeout")
	if err != nil {
		t.Fatal(err)
	}
	if value != float64(60) {
		t.Fatalf("meta.timeout = %#v", value)
	}
}

func TestSetValueRevalidatesURL(t *testing.T) {
	manager, _ := newTestManager(t, Config{BlockInternal: true})
	ctx := context.Background()
	if _, err := manager.Add(ctx, 7, "git", "https://example.com/sse", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SetValue(ctx, 7, "git", "url", "http://localhost:8080/sse"); !errors.Is(err, errs.ErrInternalHostBlocked) {
		t.Fatalf("internal url update error = %v, want ErrInternalHostBlocked", err)
	}
	value, err := manager.Value(ctx, 7, "git", "url")
	if err != nil {
		t.Fatal(err)
	}
	if value != "https://example.com/sse" {
		t.Fatalf("url changed after rejected update: %#v", value)
	}
}

func TestSetValueCanAddNestedHeader(t *testing.T) {
	manager, _ := newTestManager(t, Config{BlockInternal: false})
	ctx := context.Background()
	if _, err := manager.Add(ctx, 7, "git", "https://example.com/sse", "", ""); err != nil {
		t.Fatal(err)
	}
	value, err := manager.SetValue(ctx, 7, "git", "header.Authorization", "Bearer token")
	if err != nil {
		t.Fatal(err)
	}
	if value != "Bearer token" {
		t.Fatalf("updated header = %#v", value)
	}
}

func TestLoadCanBeCanceledWithChatState(t *testing.T) {
	manager, _ := newTestManager(t, Config{BlockInternal: false})
	defer manager.Close()

	gateStarted := make(chan struct{})
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_, err := manager.Load(ctx, 7, func(ctx context.Context) (bool, error) {
			close(gateStarted)
			<-ctx.Done()
			return false, ctx.Err()
		})
		done <- err
	}()
	<-gateStarted
	cancel()
	select {
	case loadErr := <-done:
		if !errors.Is(loadErr, context.Canceled) {
			t.Fatalf("load error = %v, want context.Canceled", loadErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled load did not invoke callback")
	}
}

func TestLoadRejectsClosedLoader(t *testing.T) {
	manager, _ := newTestManager(t, Config{})
	manager.Close()
	if _, err := manager.Load(context.Background(), 7, nil); !errors.Is(err, errs.ErrLoaderClosed) {
		t.Fatalf("Load error = %v, want ErrLoaderClosed", err)
	}
}

func TestLoadHonoursGate(t *testing.T) {
	manager, _ := newTestManager(t, Config{BlockInternal: false})
	defer manager.Close()

	deniedResult, deniedErr := manager.Load(context.Background(), 7, func(context.Context) (bool, error) {
		return false, nil
	})
	loadedResult, loadedErr := manager.Load(context.Background(), 7, func(context.Context) (bool, error) {
		return true, nil
	})
	if deniedErr != nil {
		t.Fatalf("denied err = %v", deniedErr)
	}
	if loadedErr != nil {
		t.Fatalf("loaded err = %v", loadedErr)
	}
	defer deniedResult.Close()
	defer loadedResult.Close()
	if len(loadedResult.Tools) != 0 {
		t.Fatalf("loaded tools = %d, want 0", len(loadedResult.Tools))
	}
}

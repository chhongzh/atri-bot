package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chhongzh/atri-bot/internal/account"
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
	if _, err = manager.Add(ctx, 1, "weather", "https://other.example/sse", "", ""); !errors.Is(err, ErrProviderExists) {
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
	if err = manager.Remove(ctx, 1, "weather"); !errors.Is(err, ErrProviderNotFound) {
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
		{name: "internal suffix", url: "http://service.internal/sse", wantErr: ErrInternalHostBlocked},
		{name: "localhost", url: "http://localhost:8080/sse", wantErr: ErrInternalHostBlocked},
		{name: "loopback ip", url: "http://127.0.0.1:12345/sse", wantErr: ErrInternalHostBlocked},
		{name: "private ip", url: "http://192.168.1.10/sse", wantErr: ErrInternalHostBlocked},
		{name: "non-http scheme", url: "ftp://example.com/sse", wantErr: ErrInvalidScheme},
		{name: "bad json meta", url: "https://example.com/sse", meta: "not-json", wantErr: ErrInvalidJSON},
		{name: "array header", url: "https://example.com/sse", header: `["a"]`, wantErr: ErrInvalidJSON},
		{name: "header injection", url: "https://example.com/sse", header: "{\"Authorization\":\"secret\\r\\nInjected: true\"}", wantErr: ErrInvalidJSON},
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
	if _, err := manager.Add(ctx, 1, "c", "https://c.example/sse", "", ""); !errors.Is(err, ErrProviderLimit) {
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

	if _, err = manager.SetValue(ctx, 7, "git", "header.Authorization", 42); !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("non-string header error = %v, want ErrInvalidJSON", err)
	}
	if _, err = manager.SetValue(ctx, 7, "git", "name", "renamed"); !errors.Is(err, ErrPathForbidden) {
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
	if _, err := manager.SetValue(ctx, 7, "git", "url", "http://localhost:8080/sse"); !errors.Is(err, ErrInternalHostBlocked) {
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

func TestLoadAsyncCanBeCanceledWithChatState(t *testing.T) {
	manager, _ := newTestManager(t, Config{BlockInternal: false, Workers: 1})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	gateStarted := make(chan struct{})
	done := make(chan error, 1)
	cancel, err := manager.LoadAsync(7, func(ctx context.Context) (bool, error) {
		close(gateStarted)
		<-ctx.Done()
		return false, ctx.Err()
	}, func(_ *LoadResult, loadErr error) {
		done <- loadErr
	})
	if err != nil {
		t.Fatal(err)
	}
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

func TestLoadAsyncRequiresStartedLoader(t *testing.T) {
	manager, _ := newTestManager(t, Config{})
	if _, err := manager.LoadAsync(7, nil, func(*LoadResult, error) {}); !errors.Is(err, ErrLoaderNotStarted) {
		t.Fatalf("LoadAsync error = %v, want ErrLoaderNotStarted", err)
	}
}

func TestLoadAsyncHonoursGateAndRunsCallback(t *testing.T) {
	manager, _ := newTestManager(t, Config{BlockInternal: false})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	deniedDone := make(chan struct{})
	var deniedResult *LoadResult
	var deniedErr error
	if _, err := manager.LoadAsync(7, func(context.Context) (bool, error) {
		return false, nil
	}, func(result *LoadResult, err error) {
		deniedResult = result
		deniedErr = err
		close(deniedDone)
	}); err != nil {
		t.Fatal(err)
	}

	loadedDone := make(chan struct{})
	var loadedCount int
	var loadedErr error
	if _, err := manager.LoadAsync(7, func(context.Context) (bool, error) {
		return true, nil
	}, func(result *LoadResult, err error) {
		loadedErr = err
		if result != nil {
			loadedCount = len(result.Tools)
			result.Close()
		}
		close(loadedDone)
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-deniedDone:
	case <-time.After(2 * time.Second):
		t.Fatal("denied callback was not invoked")
	}
	if deniedResult != nil {
		t.Fatalf("denied result = %#v, want nil", deniedResult)
	}
	if deniedErr != nil {
		t.Fatalf("denied err = %v", deniedErr)
	}
	select {
	case <-loadedDone:
	case <-time.After(2 * time.Second):
		t.Fatal("loaded callback was not invoked")
	}
	if loadedErr != nil {
		t.Fatalf("loaded err = %v", loadedErr)
	}
	if loadedCount != 0 {
		t.Fatalf("loaded tools = %d, want 0", loadedCount)
	}
}

package chat

import (
	"context"
	"errors"
	"testing"

	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/cloudwego/eino/components/tool"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type emptyConfig struct{}
type emptyInput struct{}
type emptyOutput struct{}

func newTestChatManager(t *testing.T, db *gorm.DB, defaults map[string]bool) *Manager {
	t.Helper()
	toolManager := toolmanager.New(db, zap.NewNop())
	for _, name := range []string{"tool_a", "tool_b", "tool_c"} {
		err := toolmanager.Register(toolManager, name, "test tool "+name, emptyConfig{},
			func(_ context.Context, _ *toolmanager.RunningState, _ *emptyConfig, _ *emptyInput) (*emptyOutput, error) {
				return &emptyOutput{}, nil
			})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := toolManager.Init(); err != nil {
		t.Fatal(err)
	}
	manager := New(context.Background(), zap.NewNop(), db, nil, nil, nil, toolManager, Config{
		DefaultToolPermissions: defaults,
	})
	if err := manager.Init(); err != nil {
		t.Fatal(err)
	}
	return manager
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestToolPermissionFallsBackToDefaults(t *testing.T) {
	manager := newTestChatManager(t, openTestDB(t), map[string]bool{
		"tool_a": true,
		"tool_b": false,
	})
	ctx := context.Background()
	for _, test := range []struct {
		toolName string
		want     bool
		custom   bool
	}{
		{"tool_a", true, false},
		{"tool_b", false, false},
		{"tool_c", true, false},
	} {
		info, err := manager.ToolPermissionInfo(ctx, 1, test.toolName)
		if err != nil {
			t.Fatalf("ToolPermissionInfo(%s) error = %v", test.toolName, err)
		}
		if info.Allowed != test.want || info.Custom != test.custom {
			t.Errorf("ToolPermissionInfo(%s) = allowed:%v custom:%v, want allowed:%v custom:%v",
				test.toolName, info.Allowed, info.Custom, test.want, test.custom)
		}
		allowed, err := manager.ToolAllowed(ctx, 1, test.toolName)
		if err != nil {
			t.Fatalf("ToolAllowed(%s) error = %v", test.toolName, err)
		}
		if allowed != test.want {
			t.Errorf("ToolAllowed(%s) = %v, want %v", test.toolName, allowed, test.want)
		}
	}
}

func TestToolPermissionPerUserOverrideAndReset(t *testing.T) {
	manager := newTestChatManager(t, openTestDB(t), map[string]bool{"tool_a": false})
	ctx := context.Background()

	if err := manager.SetToolPermission(ctx, 1, "tool_a", true); err != nil {
		t.Fatal(err)
	}
	info, err := manager.ToolPermissionInfo(ctx, 1, "tool_a")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Allowed || !info.Custom || info.Default {
		t.Errorf("user 1 override = %+v, want allowed+custom", info)
	}
	other, err := manager.ToolAllowed(ctx, 2, "tool_a")
	if err != nil {
		t.Fatal(err)
	}
	if other {
		t.Errorf("user 2 should still fall back to default deny")
	}

	if err = manager.ResetToolPermission(ctx, 1, "tool_a"); err != nil {
		t.Fatal(err)
	}
	info, err = manager.ToolPermissionInfo(ctx, 1, "tool_a")
	if err != nil {
		t.Fatal(err)
	}
	if info.Allowed || info.Custom {
		t.Errorf("after reset = %+v, want default deny without custom", info)
	}
}

func TestToolPermissionRejectsUnknownTool(t *testing.T) {
	manager := newTestChatManager(t, openTestDB(t), nil)
	ctx := context.Background()
	if err := manager.SetToolPermission(ctx, 1, "missing_tool", true); !errors.Is(err, toolmanager.ErrToolNotFound) {
		t.Fatalf("SetToolPermission error = %v, want ErrToolNotFound", err)
	}
	if err := manager.ResetToolPermission(ctx, 1, "missing_tool"); !errors.Is(err, toolmanager.ErrToolNotFound) {
		t.Fatalf("ResetToolPermission error = %v, want ErrToolNotFound", err)
	}
}

func TestAllowedToolsFiltersByPermission(t *testing.T) {
	manager := newTestChatManager(t, openTestDB(t), map[string]bool{"tool_b": false})
	ctx := context.Background()
	tools, err := manager.allowedTools(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertToolNames(t, tools, "tool_a", "tool_c")

	if err = manager.SetToolPermission(ctx, 1, "tool_b", true); err != nil {
		t.Fatal(err)
	}
	tools, err = manager.allowedTools(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertToolNames(t, tools, "tool_a", "tool_b", "tool_c")

	tools, err = manager.allowedTools(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertToolNames(t, tools, "tool_a", "tool_c")
}

func assertToolNames(t *testing.T, tools []tool.BaseTool, want ...string) {
	t.Helper()
	got := make([]string, 0, len(tools))
	for _, item := range tools {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, info.Name)
	}
	if len(got) != len(want) {
		t.Fatalf("tool names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tool names = %v, want %v", got, want)
		}
	}
}

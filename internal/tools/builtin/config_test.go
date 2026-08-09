package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/cloudwego/eino/components/tool"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type testConfig struct {
	Endpoint string `json:"endpoint"`
	Retries  int    `json:"retries"`
}

type testInput struct{}

type testResult struct{}

func TestConfigToolsListGetAndUpdatePath(t *testing.T) {
	manager := newTestManager(t)
	ctx := toolmanager.WithRunningState(context.Background(), &toolmanager.RunningState{UserID: 42})

	listOutput := invoke(t, manager, ListConfigurableToolsName, ctx, `{}`)
	var listed listConfigurableToolsResult
	if err := json.Unmarshal([]byte(listOutput), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.ToolNames) != 1 || listed.ToolNames[0] != "sample" {
		t.Fatalf("listed tools = %#v, want [sample]", listed.ToolNames)
	}

	getOutput := invoke(t, manager, GetToolConfigName, ctx, `{"tool_name":"sample","path":"endpoint"}`)
	assertConfigValue(t, getOutput, "https://default.example")

	setOutput := invoke(t, manager, ConfigureToolName, ctx, `{"tool_name":"sample","path":"endpoint","value":"https://custom.example"}`)
	assertConfigValue(t, setOutput, "https://custom.example")

	getOutput = invoke(t, manager, GetToolConfigName, ctx, `{"tool_name":"sample","path":"endpoint"}`)
	assertConfigValue(t, getOutput, "https://custom.example")
}

func TestConfigureToolValidatesPathAndValue(t *testing.T) {
	manager := newTestManager(t)
	ctx := toolmanager.WithRunningState(context.Background(), &toolmanager.RunningState{UserID: 7})
	configure := invokableTool(t, manager, ConfigureToolName)

	tests := []struct {
		name      string
		arguments string
		wantError error
	}{
		{name: "missing value", arguments: `{"tool_name":"sample","path":"endpoint"}`},
		{name: "missing tool name", arguments: `{"path":"endpoint","value":"x"}`},
		{name: "unknown path", arguments: `{"tool_name":"sample","path":"unknown","value":"x"}`, wantError: toolmanager.ErrConfigPathNotFound},
		{name: "query expression", arguments: `{"tool_name":"sample","path":"endpoint.#","value":"x"}`},
		{name: "wrong type", arguments: `{"tool_name":"sample","path":"retries","value":"many"}`},
		{name: "unknown tool", arguments: `{"tool_name":"missing","path":"endpoint","value":"x"}`, wantError: toolmanager.ErrToolNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := configure.InvokableRun(ctx, tt.arguments)
			if err == nil {
				t.Fatal("configure_tool succeeded, want validation error")
			}
			if tt.wantError != nil && !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
		})
	}

	value, err := manager.ConfigValue(ctx, 7, "sample", "endpoint")
	if err != nil {
		t.Fatal(err)
	}
	if value != "https://default.example" {
		t.Fatalf("endpoint changed after invalid updates: %#v", value)
	}
}

func newTestManager(t *testing.T) *toolmanager.Manager {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	manager := toolmanager.New(db, zap.NewNop())
	err = toolmanager.Register(manager, "sample", "sample tool", testConfig{
		Endpoint: "https://default.example",
		Retries:  3,
	}, func(context.Context, *toolmanager.RunningState, *testConfig, *testInput) (*testResult, error) {
		return &testResult{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	if err = Register(manager); err != nil {
		t.Fatal(err)
	}
	return manager
}

func invoke(t *testing.T, manager *toolmanager.Manager, name string, ctx context.Context, arguments string) string {
	t.Helper()
	result, err := invokableTool(t, manager, name).InvokableRun(ctx, arguments)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func invokableTool(t *testing.T, manager *toolmanager.Manager, name string) tool.InvokableTool {
	t.Helper()
	for _, candidate := range manager.Tools() {
		info, err := candidate.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name == name {
			invokable, ok := candidate.(tool.InvokableTool)
			if !ok {
				t.Fatalf("tool %s is not invokable", name)
			}
			return invokable
		}
	}
	t.Fatalf("tool %s not found", name)
	return nil
}

func assertConfigValue(t *testing.T, output string, want any) {
	t.Helper()
	var result toolConfigValueResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result.Value != want {
		t.Fatalf("config value = %#v, want %#v", result.Value, want)
	}
}

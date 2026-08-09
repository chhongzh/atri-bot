package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type testConfig struct {
	Prefix string `json:"prefix"`
}

type testInput struct {
	Value string `json:"value"`
}

type testOutput struct {
	Value string `json:"value"`
}

func TestConfiguredToolLoadsPerUserConfig(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db, zap.NewNop())
	err = Register(manager, "prefix_value", "prefix a value", testConfig{Prefix: "default:"},
		func(_ context.Context, _ *RunningState, config *testConfig, input *testInput) (*testOutput, error) {
			return &testOutput{Value: config.Prefix + input.Value}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}

	registered, ok := manager.Tools()[0].(tool.InvokableTool)
	if !ok {
		t.Fatal("configured tool is not invokable")
	}
	ctx := WithRunningState(context.Background(), &RunningState{UserID: 7})
	result, err := registered.InvokableRun(ctx, `{"value":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	assertToolValue(t, result, "default:hello")
	var stored ConfigRecord
	if err = db.First(&stored, "user_id = ? AND tool_name = ?", 7, "prefix_value").Error; err != nil {
		t.Fatalf("default config was not persisted: %v", err)
	}
	if err = manager.SetConfig(ctx, 7, "prefix_value", []byte(`{"prefix":"custom:"}`)); err != nil {
		t.Fatal(err)
	}
	result, err = registered.InvokableRun(ctx, `{"value":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	assertToolValue(t, result, "custom:hello")
}

func TestSetConfigRejectsUnknownFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	manager := New(db, zap.NewNop())
	if err = Register(manager, "prefix_value", "prefix a value", testConfig{Prefix: "default:"},
		func(_ context.Context, _ *RunningState, _ *testConfig, _ *testInput) (*testOutput, error) {
			return &testOutput{}, nil
		}); err != nil {
		t.Fatal(err)
	}
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}

	err = manager.SetConfig(context.Background(), 1, "prefix_value", []byte(`{"prefix":"ok","unknown":true}`))
	if err == nil {
		t.Fatal("SetConfig accepted an unknown field")
	}
	var count int64
	if err = db.Model(&ConfigRecord{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid config was persisted, record count = %d", count)
	}
}

func assertToolValue(t *testing.T, data, expected string) {
	t.Helper()
	var output testOutput
	if err := json.Unmarshal([]byte(data), &output); err != nil {
		t.Fatal(err)
	}
	if output.Value != expected {
		t.Fatalf("tool output = %q, want %q", output.Value, expected)
	}
}

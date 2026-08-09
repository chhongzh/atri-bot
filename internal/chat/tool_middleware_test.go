package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

func TestSafeToolMiddlewarePassesThroughSuccess(t *testing.T) {
	middleware := &safeToolMiddleware{}
	wrapped, err := middleware.WrapInvokableToolCall(
		context.Background(),
		func(_ context.Context, _ string, _ ...tool.Option) (string, error) {
			return "ok", nil
		},
		&adk.ToolContext{},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := wrapped(context.Background(), "{}")
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatalf("result = %q, want ok", result)
	}
}

func TestSafeToolMiddlewareConvertsErrorToResult(t *testing.T) {
	middleware := &safeToolMiddleware{}
	wrapped, err := middleware.WrapInvokableToolCall(
		context.Background(),
		func(_ context.Context, _ string, _ ...tool.Option) (string, error) {
			return "", errors.New("smtp down")
		},
		&adk.ToolContext{},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := wrapped(context.Background(), "{}")
	if err != nil {
		t.Fatalf("tool error should be converted, got error %v", err)
	}
	if !strings.Contains(result, "[tool error]") || !strings.Contains(result, "smtp down") {
		t.Fatalf("result = %q, want [tool error] smtp down", result)
	}
}

func TestSafeToolMiddlewarePropagatesInterruptRerunError(t *testing.T) {
	middleware := &safeToolMiddleware{}
	want := compose.NewInterruptAndRerunErr("human approval needed")
	wrapped, err := middleware.WrapInvokableToolCall(
		context.Background(),
		func(_ context.Context, _ string, _ ...tool.Option) (string, error) {
			return "", want
		},
		&adk.ToolContext{},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = wrapped(context.Background(), "{}")
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want interrupt rerun error %v", err, want)
	}
}

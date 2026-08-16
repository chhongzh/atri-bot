// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type fakeMCPTool struct {
	runErr error
}

func (f *fakeMCPTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "remote_tool", Desc: "fake"}, nil
}

func (f *fakeMCPTool) InvokableRun(_ context.Context, arguments string, _ ...tool.Option) (string, error) {
	if f.runErr != nil {
		return "", f.runErr
	}
	return "ok:" + arguments, nil
}

func TestAuditToolWrapsInvocation(t *testing.T) {
	provider := &model.MCPProvider{Name: "p1", URL: "https://example.com/sse"}
	wrapped := wrapAuditTool(zap.NewNop(), 1, provider, &fakeMCPTool{})
	invokable, ok := wrapped.(tool.InvokableTool)
	if !ok {
		t.Fatal("wrapped tool is not invokable")
	}
	result, err := invokable.InvokableRun(context.Background(), `{"q":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok:{\"q\":1}" {
		t.Fatalf("result = %q", result)
	}
	info, err := wrapped.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "mcp_0_p1_remote_tool" {
		t.Fatalf("info name = %q, want namespaced tool name", info.Name)
	}

	failing := wrapAuditTool(zap.NewNop(), 1, provider, &fakeMCPTool{runErr: errors.New("boom")})
	if _, err = failing.(tool.InvokableTool).InvokableRun(context.Background(), "{}"); err == nil {
		t.Fatal("expected error from wrapped failing tool")
	}
}

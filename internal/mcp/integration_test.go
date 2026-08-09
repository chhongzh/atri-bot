package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TestLoadAsyncLoadsRealMCPServer exercises the whole loader chain against a
// real in-process SSE MCP server: connect, initialize, list tools, audit wrap
// and remote invocation.
func TestLoadAsyncLoadsRealMCPServer(t *testing.T) {
	mcpServer := server.NewMCPServer("integration", "1.0.0")
	mcpServer.AddTool(
		mcp.NewTool("remote_echo",
			mcp.WithString("message", mcp.Required(), mcp.Description("text to echo")),
		),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			message, _ := req.Params.Arguments.(map[string]any)["message"].(string)
			return mcp.NewToolResultText("echo:" + message), nil
		},
	)
	sseServer := server.NewTestServer(mcpServer)
	defer sseServer.Close()

	manager, _ := newTestManager(t, Config{BlockInternal: false})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	ctx := context.Background()
	if _, err := manager.Add(ctx, 42, "echo", sseServer.URL+"/sse", "", ""); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var tools []tool.BaseTool
	var loadResult *LoadResult
	if _, err := manager.LoadAsync(42, func(context.Context) (bool, error) {
		return true, nil
	}, func(result *LoadResult, err error) {
		defer close(done)
		if err != nil {
			t.Errorf("load error = %v", err)
			return
		}
		if result != nil {
			tools = result.Tools
			loadResult = result
		}
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("mcp load timed out")
	}
	if len(tools) != 1 {
		t.Fatalf("loaded tools = %d, want 1", len(tools))
	}
	info, err := tools[0].Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "mcp_1_echo_remote_echo" {
		t.Fatalf("tool name = %q, want namespaced remote tool", info.Name)
	}
	invokable, ok := tools[0].(tool.InvokableTool)
	if !ok {
		t.Fatal("loaded tool is not invokable")
	}
	output, err := invokable.InvokableRun(ctx, `{"message":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "echo:hello") {
		t.Fatalf("tool output = %q, want echo:hello", output)
	}
	loadResult.Close()
}

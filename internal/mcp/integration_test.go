package mcp

import (
	"context"
	"strings"
	"testing"

	configmanager "github.com/chhongzh/atri-bot/internal/config"
	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestLoadLoadsRealSSEMCPServer(t *testing.T) {
	mcpServer := newIntegrationMCPServer()
	sseServer := server.NewTestServer(mcpServer)
	defer sseServer.Close()

	testLoadLoadsRealMCPServer(t, sseServer.URL+"/sse")
}

func TestLoadLoadsRealStreamableHTTPMCPServer(t *testing.T) {
	mcpServer := newIntegrationMCPServer()
	httpServer := server.NewTestStreamableHTTPServer(mcpServer)
	defer httpServer.Close()

	testLoadLoadsRealMCPServer(t, httpServer.URL)
}

func newIntegrationMCPServer() *server.MCPServer {
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
	return mcpServer
}

func testLoadLoadsRealMCPServer(t *testing.T, endpoint string) {
	t.Helper()
	manager, _ := newTestManager(t, configmanager.RuntimeSettings{MCPDefaultMaxTools: 32, MCPBlockInternal: false})
	defer manager.Close()

	ctx := context.Background()
	if _, err := manager.Add(ctx, 42, "echo", endpoint, "", ""); err != nil {
		t.Fatal(err)
	}

	loadResult, err := manager.Load(ctx, 42, func(context.Context) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer loadResult.Close()
	tools := loadResult.Tools
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
}

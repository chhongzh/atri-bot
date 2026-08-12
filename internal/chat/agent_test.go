package chat

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	modelcomponent "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestBuildAgentDefersMCPToolSchemas(t *testing.T) {
	ctx := context.Background()
	chatModel := &recordingToolModel{dynamicToolName: "mcp_remote_lookup"}
	staticTool := &namedTestTool{name: "static_tool", description: "always visible"}
	mcpTool := &namedTestTool{name: "mcp_remote_lookup", description: "very large remote MCP schema"}
	agent, err := buildAgentWithTools(ctx, chatModel, []tool.BaseTool{staticTool}, []tool.BaseTool{mcpTool})
	if err != nil {
		t.Fatal(err)
	}

	events := agent.Run(ctx, &adk.AgentInput{Messages: []*schema.Message{schema.UserMessage("hello")}})
	for {
		event, ok := events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			_, _ = event.Output.MessageOutput.GetMessage()
		}
	}

	assertToolNames(t, chatModel.Tools(0), []string{"static_tool", "tool_search"})
	assertToolNames(t, chatModel.Tools(1), []string{"mcp_remote_lookup", "static_tool", "tool_search"})
	assertToolNames(t, chatModel.Tools(2), []string{"mcp_remote_lookup", "static_tool", "tool_search"})
}

func TestPersistentAgentOutputsDropsToolSearchExchange(t *testing.T) {
	outputs := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID:       "search-call",
			Function: schema.FunctionCall{Name: "tool_search", Arguments: `{"query":"remote"}`},
		}}),
		schema.ToolMessage(`{"matches":["mcp_remote_lookup"]}`, "search-call", schema.WithToolName("tool_search")),
		schema.AssistantMessage("", []schema.ToolCall{{
			ID:       "remote-call",
			Function: schema.FunctionCall{Name: "mcp_remote_lookup", Arguments: `{}`},
		}}),
		schema.ToolMessage("remote result", "remote-call", schema.WithToolName("mcp_remote_lookup")),
		schema.AssistantMessage("done", nil),
	}
	persisted := persistentAgentOutputs(outputs)
	if len(persisted) != 3 {
		t.Fatalf("persisted outputs = %#v", persisted)
	}
	if persisted[0].ToolCalls[0].Function.Name != "mcp_remote_lookup" || persisted[1].ToolName != "mcp_remote_lookup" || persisted[2].Content != "done" {
		t.Fatalf("persisted outputs = %#v", persisted)
	}
}

func TestPreemptedTurnMessagesPreserveDeliveredReply(t *testing.T) {
	state := &UserState{}
	requests := []*Request{{Text: "发一个小猫表情"}}
	state.requeueMessages(append(requestMessages(requests), assistantMessages([]string{"[猫咪]"})...))

	messages := state.startTurnMessages()
	if len(messages) != 2 {
		t.Fatalf("queued messages = %#v", messages)
	}
	if messages[0].Role != schema.User || messages[0].Content != "发一个小猫表情" {
		t.Fatalf("original request = %#v", messages[0])
	}
	if messages[1].Role != schema.Assistant || messages[1].Content != "[猫咪]" {
		t.Fatalf("delivered reply = %#v", messages[1])
	}
	if remaining := state.finishTurnMessages(); len(remaining) != 2 {
		t.Fatalf("active messages = %#v", remaining)
	}
}

func assertToolNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("model call tools = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("model call tools = %v, want %v", got, want)
		}
	}
}

type namedTestTool struct {
	name        string
	description string
}

func (t *namedTestTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        t.name,
		Desc:        t.description,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{}),
	}, nil
}

func (t *namedTestTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return `{}`, nil
}

type recordingToolModel struct {
	mu              sync.Mutex
	toolSets        [][]string
	dynamicToolName string
}

func (m *recordingToolModel) Generate(_ context.Context, _ []*schema.Message, opts ...modelcomponent.Option) (*schema.Message, error) {
	options := modelcomponent.GetCommonOptions(nil, opts...)
	names := make([]string, 0, len(options.Tools))
	for _, info := range options.Tools {
		names = append(names, info.Name)
	}
	sort.Strings(names)
	m.mu.Lock()
	m.toolSets = append(m.toolSets, names)
	call := len(m.toolSets)
	m.mu.Unlock()
	switch call {
	case 1:
		return schema.AssistantMessage("", []schema.ToolCall{{
			ID:       "search-call",
			Function: schema.FunctionCall{Name: "tool_search", Arguments: `{"query":"select:` + m.dynamicToolName + `"}`},
		}}), nil
	case 2:
		return schema.AssistantMessage("", []schema.ToolCall{{
			ID:       "dynamic-call",
			Function: schema.FunctionCall{Name: m.dynamicToolName, Arguments: `{}`},
		}}), nil
	default:
		return schema.AssistantMessage("done", nil), nil
	}
}

func (m *recordingToolModel) Stream(context.Context, []*schema.Message, ...modelcomponent.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("unexpected stream call")
}

func (m *recordingToolModel) WithTools([]*schema.ToolInfo) (modelcomponent.ToolCallingChatModel, error) {
	return m, nil
}

func (m *recordingToolModel) BindTools([]*schema.ToolInfo) error {
	return nil
}

func (m *recordingToolModel) BindForcedTools([]*schema.ToolInfo) error {
	return nil
}

func (m *recordingToolModel) Tools(index int) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= len(m.toolSets) {
		return nil
	}
	return append([]string(nil), m.toolSets[index]...)
}

var _ modelcomponent.ToolCallingChatModel = (*recordingToolModel)(nil)
var _ tool.InvokableTool = (*namedTestTool)(nil)

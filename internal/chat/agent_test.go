// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package chat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chhongzh/atri-bot/internal/character"
	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/chhongzh/atri-bot/internal/session"
	"github.com/cloudwego/eino/adk"
	modelcomponent "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
	"gorm.io/gorm"
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

func TestConsumeMessageVariantReturnsPartialMessageOnCancellation(t *testing.T) {
	index := 0
	cancelErr := errors.New("stream canceled")
	reader, writer := schema.Pipe[*schema.Message](2)
	writer.Send(schema.AssistantMessage("我", []schema.ToolCall{{
		Index: &index,
		ID:    "call-1",
		Type:  "function",
		Function: schema.FunctionCall{
			Name:      "lookup",
			Arguments: `{"query":"`,
		},
	}}), nil)
	writer.Send(schema.AssistantMessage("喜欢", []schema.ToolCall{{
		Index: &index,
		Function: schema.FunctionCall{
			Arguments: "火",
		},
	}}), cancelErr)
	writer.Close()

	var seen []string
	message, err := consumeMessageVariant(&adk.MessageVariant{
		IsStreaming:   true,
		Role:          schema.Assistant,
		MessageStream: reader,
	}, func(chunk *schema.Message) error {
		seen = append(seen, chunk.Content)
		return nil
	})
	if !errors.Is(err, cancelErr) {
		t.Fatalf("stream error = %v, want %v", err, cancelErr)
	}
	if message == nil || message.Content != "我喜欢" {
		t.Fatalf("partial message = %#v", message)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != "call-1" || message.ToolCalls[0].Function.Arguments != `{"query":"火` {
		t.Fatalf("partial tool call = %#v", message.ToolCalls)
	}
	if len(seen) != 2 {
		t.Fatalf("handled chunks = %v", seen)
	}
}

func TestOnAgentEventsSendsCompletedBlockBeforeStreamEnds(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:chat_stream_blocks?mode=memory&cache=shared"))
	if err != nil {
		t.Fatal(err)
	}
	sessions := session.New(db, zap.NewNop())
	if err = sessions.Init(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.Mkdir(filepath.Join(root, "chardefs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "chardefs", "character.one.yaml"), []byte("name: Test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	characters := character.New(db, zap.NewNop(), character.Config{CWD: root})
	if err = characters.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	roundID, err := sessions.StartRound(context.Background(), 20, "character.one", schema.UserMessage("请分段回答"))
	if err != nil {
		t.Fatal(err)
	}

	sent := make(chan string, 2)
	c := &recordingTelebotContext{Context: preparationTestContext(20), sentNotify: sent}
	request := &Request{
		Context:  c,
		Text:     "请分段回答",
		RoundID:  roundID,
		Revision: 1,
		done:     make(chan error, 1),
	}
	reader, writer := schema.Pipe[*schema.Message](0)
	events, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	generator.Send(adk.EventFromMessage(nil, reader, schema.Assistant, ""))
	manager := &Manager{
		logger:     zap.NewNop(),
		characters: characters,
		sessions:   sessions,
		cfg: Config{
			ModelTimeout:  time.Second,
			OnMessageSent: func(telebot.Context) {},
		},
		ctx: context.Background(),
	}
	state := &UserState{
		UserID:        20,
		CharacterID:   "character.one",
		MaxRounds:     10,
		activeRoundID: roundID,
		roundRevision: 1,
	}
	turn := &adk.TurnContext[*Request, *schema.Message]{
		Consumed:  []*Request{request},
		Preempted: make(chan struct{}),
		Stopped:   make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		done <- manager.onAgentEvents(context.Background(), state, nil, turn, events)
	}()

	chunksSent := make(chan bool, 1)
	go func() {
		closed := writer.Send(schema.AssistantMessage("第一段", nil), nil)
		closed = writer.Send(schema.AssistantMessage("\\", nil), nil) || closed
		closed = writer.Send(schema.AssistantMessage("m第二段", nil), nil) || closed
		chunksSent <- closed
	}()
	select {
	case text := <-sent:
		if text != "第一段" {
			t.Fatalf("first sent block = %q", text)
		}
	case <-time.After(time.Second):
		t.Fatal("completed block was not sent while agent stream remained open")
	}
	select {
	case closed := <-chunksSent:
		if closed {
			t.Fatal("agent stream closed while sending test chunks")
		}
	case <-time.After(time.Second):
		t.Fatal("agent stream stopped consuming after the completed block")
	}
	select {
	case err = <-done:
		t.Fatalf("agent events completed before stream EOF: %v", err)
	default:
	}

	writer.Close()
	generator.Close()
	select {
	case text := <-sent:
		if text != "第二段" {
			t.Fatalf("flushed tail block = %q", text)
		}
	case <-time.After(time.Second):
		t.Fatal("tail block was not flushed after stream EOF")
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if requestErr := <-request.done; requestErr != nil {
		t.Fatal(requestErr)
	}
}

func TestPreemptedTurnPersistsPartialOutput(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:chat_preempt_partial?mode=memory&cache=shared"))
	if err != nil {
		t.Fatal(err)
	}
	sessions := session.New(db, zap.NewNop())
	if err = sessions.Init(); err != nil {
		t.Fatal(err)
	}
	roundID, err := sessions.StartRound(context.Background(), 21, "character.one", schema.UserMessage("你喜欢吃"))
	if err != nil {
		t.Fatal(err)
	}
	cancelErr := errors.New("generation canceled")
	reader, writer := schema.Pipe[*schema.Message](1)
	writer.Send(schema.AssistantMessage("我刚想说", nil), cancelErr)
	writer.Close()
	events, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	generator.Send(adk.EventFromMessage(nil, reader, schema.Assistant, ""))
	generator.Close()
	preempted := make(chan struct{})
	close(preempted)
	request := &Request{Text: "你喜欢吃", RoundID: roundID, done: make(chan error, 1)}
	manager := &Manager{
		logger:   zap.NewNop(),
		sessions: sessions,
		cfg: Config{
			ModelTimeout:  time.Second,
			OnMessageSent: func(telebot.Context) {},
		},
	}
	state := &UserState{UserID: 21, CharacterID: "character.one", activeRoundID: roundID}
	turn := &adk.TurnContext[*Request, *schema.Message]{
		Consumed:  []*Request{request},
		Preempted: preempted,
		Stopped:   make(chan struct{}),
	}
	if err = manager.onAgentEvents(context.Background(), state, nil, turn, events); err != nil {
		t.Fatal(err)
	}
	if requestErr := <-request.done; !errors.Is(requestErr, errs.ErrTurnPreempted) {
		t.Fatalf("request result = %v", requestErr)
	}
	if err = sessions.AppendUser(context.Background(), 21, "character.one", roundID, schema.UserMessage("什么")); err != nil {
		t.Fatal(err)
	}
	loaded, err := sessions.Load(context.Background(), 21, "character.one", roundID, session.CompressionOptions{MaxRounds: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 5 || loaded[2].Role != schema.Assistant || loaded[2].Content != "我刚想说" {
		t.Fatalf("loaded interrupted history = %#v", loaded)
	}
	if loaded[3].Role != schema.System || !strings.Contains(loaded[3].Content, "<interrupt_info>") {
		t.Fatalf("interrupt metadata = %#v", loaded[3])
	}
	if loaded[4].Role != schema.User || loaded[4].Content != "什么" {
		t.Fatalf("interrupted user message = %#v", loaded[4])
	}
}

func TestCompletedOutputIsInterruptedWhenNewerUserRevisionExists(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:chat_late_preempt?mode=memory&cache=shared"))
	if err != nil {
		t.Fatal(err)
	}
	sessions := session.New(db, zap.NewNop())
	if err = sessions.Init(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.Mkdir(filepath.Join(root, "chardefs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "chardefs", "character.one.yaml"), []byte("name: Test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	characters := character.New(db, zap.NewNop(), character.Config{CWD: root})
	if err = characters.Init(context.Background()); err != nil {
		t.Fatal(err)
	}

	roundID, err := sessions.StartRound(context.Background(), 22, "character.one", schema.UserMessage("你喜欢吃"))
	if err != nil {
		t.Fatal(err)
	}
	c := &recordingTelebotContext{Context: preparationTestContext(22)}
	request := &Request{
		Context:  c,
		Text:     "你喜欢吃",
		RoundID:  roundID,
		Revision: 1,
		done:     make(chan error, 1),
	}
	events, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	generator.Send(adk.EventFromMessage(schema.AssistantMessage("我喜欢火锅", nil), nil, schema.Assistant, ""))
	generator.Close()
	manager := &Manager{
		logger:     zap.NewNop(),
		characters: characters,
		sessions:   sessions,
		cfg: Config{
			ModelTimeout:  time.Second,
			OnMessageSent: func(telebot.Context) {},
		},
		ctx: context.Background(),
	}
	state := &UserState{
		UserID:        22,
		CharacterID:   "character.one",
		MaxRounds:     10,
		activeRoundID: roundID,
		roundRevision: 2,
	}
	turn := &adk.TurnContext[*Request, *schema.Message]{
		Consumed:  []*Request{request},
		Preempted: make(chan struct{}),
		Stopped:   make(chan struct{}),
	}
	if err = manager.onAgentEvents(context.Background(), state, nil, turn, events); err != nil {
		t.Fatal(err)
	}
	if requestErr := <-request.done; !errors.Is(requestErr, errs.ErrTurnPreempted) {
		t.Fatalf("request result = %v", requestErr)
	}
	if len(c.sent) != 1 || c.sent[0] != "我喜欢火锅" {
		t.Fatalf("sent messages = %v", c.sent)
	}
	if state.activeRoundID != roundID || state.roundRevision != 2 {
		t.Fatalf("active round = (%d, %d), want (%d, 2)", state.activeRoundID, state.roundRevision, roundID)
	}
	if err = sessions.AppendUser(context.Background(), 22, "character.one", roundID, schema.UserMessage("什么")); err != nil {
		t.Fatal(err)
	}

	var round model.SessionRound
	if err = db.Where("id = ?", roundID).First(&round).Error; err != nil {
		t.Fatal(err)
	}
	if round.Completed || !round.Interrupted {
		t.Fatalf("round state = completed:%v interrupted:%v", round.Completed, round.Interrupted)
	}
	loaded, err := sessions.Load(context.Background(), 22, "character.one", roundID, session.CompressionOptions{MaxRounds: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 5 || loaded[2].Role != schema.Assistant || loaded[2].Content != "我喜欢火锅" {
		t.Fatalf("loaded superseded history = %#v", loaded)
	}
	if loaded[3].Role != schema.System || !strings.Contains(loaded[3].Content, "<interrupt_info>") {
		t.Fatalf("interrupt metadata = %#v", loaded[3])
	}
	if loaded[4].Role != schema.User || loaded[4].Content != "什么" {
		t.Fatalf("interrupted user message = %#v", loaded[4])
	}
}

type recordingTelebotContext struct {
	telebot.Context
	sent       []string
	sentNotify chan<- string
}

func (c *recordingTelebotContext) Send(what interface{}, _ ...interface{}) error {
	text := what.(string)
	c.sent = append(c.sent, text)
	if c.sentNotify != nil {
		c.sentNotify <- text
	}
	return nil
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

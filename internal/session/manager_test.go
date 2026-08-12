package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
)

func newTestManager(t *testing.T) (*Manager, *gorm.DB, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.DebugLevel)
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	manager := New(db, zap.New(core))
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	return manager, db, logs
}

func TestAppendRoundStoresCompleteTurnLoopRound(t *testing.T) {
	manager, db, _ := newTestManager(t)
	ctx := context.Background()
	round := []*schema.Message{
		schema.UserMessage("first telegram message"),
		schema.UserMessage("second telegram message"),
		schema.AssistantMessage("calling tool", []schema.ToolCall{{
			ID:       "call-1",
			Function: schema.FunctionCall{Name: "lookup", Arguments: `{"id":7}`},
		}}),
		schema.ToolMessage("tool result", "call-1", schema.WithToolName("lookup")),
		schema.AssistantMessage("final reply", nil),
	}
	if err := manager.AppendRound(ctx, 7, "character.one", CompressionOptions{MaxRounds: 2}, round...); err != nil {
		t.Fatal(err)
	}

	var records []roundEntry
	if err := db.Order("id ASC").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("stored entries = %#v, want one complete round", records)
	}
	loaded, err := manager.Load(ctx, 7, "character.one", CompressionOptions{MaxRounds: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertMessagesEqual(t, loaded, round)
}

func TestAppendRoundCompactsToolResultsBeforePersistence(t *testing.T) {
	manager, db, logs := newTestManager(t)
	content := strings.Repeat("前", maxPersistedToolResultRunes) + strings.Repeat("后", 2_000)
	round := []*schema.Message{
		schema.UserMessage("find it"),
		schema.AssistantMessage("", []schema.ToolCall{{
			ID:       "call-large",
			Function: schema.FunctionCall{Name: "lookup", Arguments: `{}`},
		}}),
		schema.ToolMessage(content, "call-large", schema.WithToolName("lookup")),
		schema.AssistantMessage("final reply", nil),
	}
	if err := manager.AppendRound(context.Background(), 14, "character.one", CompressionOptions{MaxRounds: 10}, round...); err != nil {
		t.Fatal(err)
	}

	var record roundEntry
	if err := db.Where("user_id = ? AND character_id = ?", 14, "character.one").First(&record).Error; err != nil {
		t.Fatal(err)
	}
	var stored []*schema.Message
	if err := json.Unmarshal([]byte(record.Messages), &stored); err != nil {
		t.Fatal(err)
	}
	toolResult := stored[2]
	if toolResult.ToolCallID != "call-large" || toolResult.ToolName != "lookup" {
		t.Fatalf("stored tool identity = (%q, %q)", toolResult.ToolCallID, toolResult.ToolName)
	}
	if !strings.Contains(toolResult.Content, "tool result truncated for long-term history") {
		t.Fatalf("stored tool result was not compacted: %q", toolResult.Content)
	}
	if len([]rune(toolResult.Content)) >= len([]rune(content)) {
		t.Fatalf("stored tool result characters = %d, original = %d", len([]rune(toolResult.Content)), len([]rune(content)))
	}
	if len([]rune(toolResult.Content)) != maxPersistedToolResultRunes {
		t.Fatalf("stored tool result characters = %d, want %d", len([]rune(toolResult.Content)), maxPersistedToolResultRunes)
	}
	if logs.FilterMessage("compacted tool results for session history").Len() != 1 {
		t.Fatal("missing tool result compaction log")
	}
}

func TestLoadCompactsLegacyToolResults(t *testing.T) {
	manager, db, _ := newTestManager(t)
	legacy := []*schema.Message{
		schema.UserMessage("legacy"),
		schema.ToolMessage(strings.Repeat("x", 20_000), "call-legacy", schema.WithToolName("legacy_lookup")),
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&roundEntry{
		UserID: 15, CharacterID: "character.one", Messages: string(data),
	}).Error; err != nil {
		t.Fatal(err)
	}

	loaded, err := manager.Load(context.Background(), 15, "character.one", CompressionOptions{MaxRounds: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || !strings.Contains(loaded[1].Content, "tool result truncated for long-term history") {
		t.Fatalf("loaded legacy tool result = %#v", loaded)
	}
	if len([]rune(loaded[1].Content)) >= len([]rune(legacy[1].Content)) {
		t.Fatalf("loaded tool result characters = %d, original = %d", len([]rune(loaded[1].Content)), len([]rune(legacy[1].Content)))
	}
}

func TestAppendRoundCompressesAtConfiguredRoundLimit(t *testing.T) {
	manager, db, logs := newTestManager(t)
	agent := &recordingAgent{responses: []agentResponse{{message: schema.AssistantMessage(" durable history ", nil)}}}
	opts := CompressionOptions{MaxRounds: 2, SystemPrompt: "dynamic character prompt", Agent: agent}
	firstRound := []*schema.Message{
		schema.UserMessage("first user"),
		schema.UserMessage("second user"),
		schema.AssistantMessage("tool call", []schema.ToolCall{{ID: "call-1", Function: schema.FunctionCall{Name: "search"}}}),
		schema.ToolMessage("search result", "call-1", schema.WithToolName("search")),
		schema.AssistantMessage("first final reply", nil),
	}
	secondRound := []*schema.Message{
		schema.UserMessage("third user"),
		schema.AssistantMessage("second final reply", nil),
	}
	if err := manager.AppendRound(context.Background(), 8, "character.one", opts, firstRound...); err != nil {
		t.Fatal(err)
	}
	if agent.CallCount() != 0 {
		t.Fatalf("compression calls before limit = %d, want 0", agent.CallCount())
	}
	if err := manager.AppendRound(context.Background(), 8, "character.one", opts, secondRound...); err != nil {
		t.Fatal(err)
	}

	input := agent.Input(0)
	if len(input) != len(firstRound)+len(secondRound)+2 {
		t.Fatalf("compression input messages = %d, want %d", len(input), len(firstRound)+len(secondRound)+2)
	}
	if input[0].Role != schema.System || input[0].Content != opts.SystemPrompt {
		t.Fatalf("compression first message = (%s, %q)", input[0].Role, input[0].Content)
	}
	assertMessagesEqual(t, input[1:1+len(firstRound)], firstRound)
	assertMessagesEqual(t, input[1+len(firstRound):len(input)-1], secondRound)
	if input[len(input)-1].Role != schema.System || !strings.Contains(input[len(input)-1].Content, "2 complete Telegram conversation rounds") {
		t.Fatalf("compression instruction = (%s, %q)", input[len(input)-1].Role, input[len(input)-1].Content)
	}

	var rounds []roundEntry
	if err := db.Where("user_id = ? AND character_id = ?", 8, "character.one").Order("id ASC").Find(&rounds).Error; err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 2 {
		t.Fatalf("stored rounds after compression = %d, want 2", len(rounds))
	}
	var summaries []summaryEntry
	if err := db.Where("user_id = ? AND character_id = ?", 8, "character.one").Find(&summaries).Error; err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].CutoffRoundID != rounds[1].ID {
		t.Fatalf("stored summaries after compression = %#v, cutoff want %d", summaries, rounds[1].ID)
	}
	loaded, err := manager.Load(context.Background(), 8, "character.one", opts)
	if err != nil {
		t.Fatal(err)
	}
	assertMessagesEqual(t, loaded, []*schema.Message{schema.SystemMessage("durable history")})
	for _, message := range []string{
		"session history compression threshold reached",
		"session history compression started",
		"session history compression completed",
	} {
		if logs.FilterMessage(message).Len() != 1 {
			t.Fatalf("log %q count = %d, want 1", message, logs.FilterMessage(message).Len())
		}
	}
}

func TestCompressionMergesPreviousSummaryAndRetainsAllRecords(t *testing.T) {
	manager, db, _ := newTestManager(t)
	agent := &recordingAgent{responses: []agentResponse{
		{message: schema.AssistantMessage("first summary", nil)},
		{message: schema.AssistantMessage("merged summary", nil)},
	}}
	opts := CompressionOptions{MaxRounds: 1, SystemPrompt: "dynamic system", Agent: agent}
	if err := manager.AppendRound(context.Background(), 9, "character.one", opts,
		schema.UserMessage("old user"), schema.AssistantMessage("old reply", nil)); err != nil {
		t.Fatal(err)
	}
	if err := manager.AppendRound(context.Background(), 9, "character.one", opts,
		schema.UserMessage("new user"), schema.AssistantMessage("new reply", nil)); err != nil {
		t.Fatal(err)
	}

	input := agent.Input(1)
	want := []*schema.Message{
		schema.SystemMessage("dynamic system"),
		schema.SystemMessage("first summary"),
		schema.UserMessage("new user"),
		schema.AssistantMessage("new reply", nil),
	}
	assertMessagesEqual(t, input[:len(input)-1], want)
	var rounds []roundEntry
	if err := db.Where("user_id = ? AND character_id = ?", 9, "character.one").Order("id ASC").Find(&rounds).Error; err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 2 {
		t.Fatalf("stored rounds = %d, want 2", len(rounds))
	}
	var summaries []summaryEntry
	if err := db.Where("user_id = ? AND character_id = ?", 9, "character.one").Order("id ASC").Find(&summaries).Error; err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("stored summaries = %d, want 2", len(summaries))
	}
	if summaries[0].CutoffRoundID != rounds[0].ID || summaries[1].CutoffRoundID != rounds[1].ID {
		t.Fatalf("summary cutoffs = [%d %d], want [%d %d]",
			summaries[0].CutoffRoundID, summaries[1].CutoffRoundID, rounds[0].ID, rounds[1].ID)
	}
	loaded, err := manager.Load(context.Background(), 9, "character.one", opts)
	if err != nil {
		t.Fatal(err)
	}
	assertMessagesEqual(t, loaded, []*schema.Message{schema.SystemMessage("merged summary")})
}

func TestLoadUsesLatestSummaryAndRoundsAfterCutoff(t *testing.T) {
	manager, db, _ := newTestManager(t)
	agent := &recordingAgent{responses: []agentResponse{{message: schema.AssistantMessage("first summary", nil)}}}
	opts := CompressionOptions{MaxRounds: 2, SystemPrompt: "dynamic system", Agent: agent}
	for _, round := range [][]*schema.Message{
		{schema.UserMessage("old one"), schema.AssistantMessage("old reply one", nil)},
		{schema.UserMessage("old two"), schema.AssistantMessage("old reply two", nil)},
		{schema.UserMessage("new after cutoff"), schema.AssistantMessage("new reply", nil)},
	} {
		if err := manager.AppendRound(context.Background(), 13, "character.one", opts, round...); err != nil {
			t.Fatal(err)
		}
	}

	loaded, err := manager.Load(context.Background(), 13, "character.one", opts)
	if err != nil {
		t.Fatal(err)
	}
	assertMessagesEqual(t, loaded, []*schema.Message{
		schema.SystemMessage("first summary"),
		schema.UserMessage("new after cutoff"),
		schema.AssistantMessage("new reply", nil),
	})
	var roundCount int64
	if err = db.Model(&roundEntry{}).Where("user_id = ? AND character_id = ?", 13, "character.one").Count(&roundCount).Error; err != nil {
		t.Fatal(err)
	}
	if roundCount != 3 {
		t.Fatalf("stored raw rounds = %d, want 3", roundCount)
	}
}

func TestCompressionFailurePreservesCompleteRounds(t *testing.T) {
	manager, db, logs := newTestManager(t)
	compressErr := errors.New("model unavailable")
	agent := &recordingAgent{responses: []agentResponse{{err: compressErr}}}
	round := []*schema.Message{schema.UserMessage("keep user"), schema.AssistantMessage("keep reply", nil)}
	err := manager.AppendRound(context.Background(), 10, "character.one", CompressionOptions{
		MaxRounds: 1, SystemPrompt: "system", Agent: agent,
	}, round...)
	if !errors.Is(err, compressErr) {
		t.Fatalf("compression error = %v, want %v", err, compressErr)
	}

	var rounds []roundEntry
	if err = db.Where("user_id = ? AND character_id = ?", 10, "character.one").Find(&rounds).Error; err != nil {
		t.Fatal(err)
	}
	history, err := decodeHistoryWindow(nil, rounds)
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 1 {
		t.Fatalf("preserved rounds = %d, want 1", len(rounds))
	}
	assertMessagesEqual(t, history, round)
	var summaryCount int64
	if err = db.Model(&summaryEntry{}).Where("user_id = ? AND character_id = ?", 10, "character.one").Count(&summaryCount).Error; err != nil {
		t.Fatal(err)
	}
	if summaryCount != 0 {
		t.Fatalf("summaries after failed compression = %d, want 0", summaryCount)
	}
	failed := logs.FilterMessage("session history compression failed").All()
	if len(failed) != 1 || failed[0].ContextMap()["history_preserved"] != true {
		t.Fatalf("compression failure logs = %#v", failed)
	}
}

func TestLoadCompressesExistingRoundsAtLimit(t *testing.T) {
	manager, db, _ := newTestManager(t)
	for _, round := range [][]*schema.Message{
		{schema.UserMessage("one"), schema.AssistantMessage("reply one", nil)},
		{schema.UserMessage("two"), schema.AssistantMessage("reply two", nil)},
	} {
		record, err := makeRoundEntry(11, "character.one", round)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	agent := &recordingAgent{responses: []agentResponse{{message: schema.AssistantMessage("loaded summary", nil)}}}
	loaded, err := manager.Load(context.Background(), 11, "character.one", CompressionOptions{
		MaxRounds: 2, SystemPrompt: "system", Agent: agent,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertMessagesEqual(t, loaded, []*schema.Message{schema.SystemMessage("loaded summary")})
}

func TestSessionIsolation(t *testing.T) {
	manager, _, _ := newTestManager(t)
	for _, input := range []struct {
		userID      int64
		characterID string
		text        string
	}{
		{userID: 1, characterID: "character.one", text: "wanted"},
		{userID: 2, characterID: "character.one", text: "other user"},
		{userID: 1, characterID: "character.two", text: "other character"},
	} {
		if err := manager.AppendRound(context.Background(), input.userID, input.characterID, CompressionOptions{MaxRounds: 10},
			schema.UserMessage(input.text), schema.AssistantMessage(input.text+" reply", nil)); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := manager.Load(context.Background(), 1, "character.one", CompressionOptions{MaxRounds: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertMessagesEqual(t, loaded, []*schema.Message{
		schema.UserMessage("wanted"), schema.AssistantMessage("wanted reply", nil),
	})
}

func TestWaitBlocksWhileCompressionRuns(t *testing.T) {
	manager, _, logs := newTestManager(t)
	releaseCompression := make(chan struct{})
	agent := &recordingAgent{
		started:   make(chan struct{}),
		release:   releaseCompression,
		responses: []agentResponse{{message: schema.AssistantMessage("summary", nil)}},
	}
	appendDone := make(chan error, 1)
	go func() {
		appendDone <- manager.AppendRound(context.Background(), 12, "character.one", CompressionOptions{
			MaxRounds: 1, SystemPrompt: "system", Agent: agent,
		}, schema.UserMessage("user"), schema.AssistantMessage("reply", nil))
	}()
	select {
	case <-agent.started:
	case <-time.After(time.Second):
		t.Fatal("compression did not start")
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- manager.Wait(context.Background(), 12, "character.one")
	}()
	select {
	case err := <-waitDone:
		t.Fatalf("Wait returned during compression: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseCompression)
	if err := waitResult(t, appendDone); err != nil {
		t.Fatal(err)
	}
	if err := waitResult(t, waitDone); err != nil {
		t.Fatal(err)
	}
	if logs.FilterMessage("waiting for session operation").Len() == 0 {
		t.Fatal("missing session wait log")
	}
}

type agentResponse struct {
	message *schema.Message
	err     error
}

type recordingAgent struct {
	mu        sync.Mutex
	inputs    [][]*schema.Message
	responses []agentResponse
	started   chan struct{}
	release   <-chan struct{}
	startOnce sync.Once
}

func (a *recordingAgent) Name(context.Context) string {
	return "recording-agent"
}

func (a *recordingAgent) Description(context.Context) string {
	return "records compression inputs"
}

func (a *recordingAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	a.mu.Lock()
	callIndex := len(a.inputs)
	a.inputs = append(a.inputs, append([]*schema.Message(nil), input.Messages...))
	response := a.responses[callIndex]
	a.mu.Unlock()
	a.startOnce.Do(func() {
		if a.started != nil {
			close(a.started)
		}
	})
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()
		if a.release != nil {
			select {
			case <-a.release:
			case <-ctx.Done():
				generator.Send(&adk.AgentEvent{Err: ctx.Err()})
				return
			}
		}
		if response.err != nil {
			generator.Send(&adk.AgentEvent{Err: response.err})
			return
		}
		generator.Send(adk.EventFromMessage(response.message, nil, schema.Assistant, ""))
	}()
	return iterator
}

func (a *recordingAgent) CallCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.inputs)
}

func (a *recordingAgent) Input(index int) []*schema.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*schema.Message(nil), a.inputs[index]...)
}

func assertMessagesEqual(t *testing.T, got, want []*schema.Message) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("messages = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index].Role != want[index].Role || got[index].Content != want[index].Content {
			t.Fatalf("message %d = (%s, %q), want (%s, %q)",
				index, got[index].Role, got[index].Content, want[index].Role, want[index].Content)
		}
		if len(got[index].ToolCalls) != len(want[index].ToolCalls) {
			t.Fatalf("message %d tool calls = %d, want %d", index, len(got[index].ToolCalls), len(want[index].ToolCalls))
		}
		if got[index].ToolCallID != want[index].ToolCallID {
			t.Fatalf("message %d tool call ID = %q, want %q", index, got[index].ToolCallID, want[index].ToolCallID)
		}
	}
}

func waitResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for operation")
		return nil
	}
}

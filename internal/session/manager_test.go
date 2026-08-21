// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

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

	"github.com/chhongzh/atri-bot/internal/model"
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

func TestSessionV2StoresOneMessagePerRow(t *testing.T) {
	manager, db, _ := newTestManager(t)
	ctx := context.Background()
	roundID, err := manager.StartRound(ctx, 7, "character.one", schema.UserMessage("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.CompleteRound(ctx, 7, "character.one", roundID, CompressionOptions{MaxRounds: 10},
		schema.AssistantMessage("hi", nil)); err != nil {
		t.Fatal(err)
	}

	var records []model.SessionMessage
	if err = db.Order("id ASC").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("stored message rows = %d, want 3", len(records))
	}
	for _, record := range records {
		if record.RoundID != roundID {
			t.Fatalf("message round id = %d, want %d", record.RoundID, roundID)
		}
		var message schema.Message
		if err = json.Unmarshal([]byte(record.Message), &message); err != nil {
			t.Fatalf("message row %d is not one schema.Message: %v", record.ID, err)
		}
	}
	var round model.SessionRound
	if err = db.Where("id = ?", roundID).First(&round).Error; err != nil {
		t.Fatal(err)
	}
	if !round.Completed || round.Interrupted {
		t.Fatalf("round state = completed:%v interrupted:%v", round.Completed, round.Interrupted)
	}

	loaded, err := manager.Load(ctx, 7, "character.one", 0, CompressionOptions{MaxRounds: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertRoundMessages(t, loaded, false, "hello", "hi")
}

func TestInterruptedRoundPreservesMessageOrder(t *testing.T) {
	manager, db, _ := newTestManager(t)
	ctx := context.Background()
	roundID, err := manager.StartRound(ctx, 8, "character.one", schema.UserMessage("你喜欢吃"))
	if err != nil {
		t.Fatal(err)
	}
	partialCall := schema.AssistantMessage("", []schema.ToolCall{{
		ID: "call-1", Function: schema.FunctionCall{Name: "lookup", Arguments: `{"query":"火`},
	}})
	if err = manager.AppendInterrupted(ctx, 8, "character.one", roundID,
		schema.AssistantMessage("我喜欢", nil),
		partialCall,
		schema.ToolMessage("半截结果", "call-1", schema.WithToolName("lookup")),
	); err != nil {
		t.Fatal(err)
	}
	if err = manager.AppendUser(ctx, 8, "character.one", roundID, schema.UserMessage("什么")); err != nil {
		t.Fatal(err)
	}

	loaded, err := manager.Load(ctx, 8, "character.one", roundID, CompressionOptions{MaxRounds: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 7 {
		t.Fatalf("current round messages = %#v", loaded)
	}
	assertMetadata(t, loaded[0], false)
	if loaded[1].Role != schema.User || loaded[1].Content != "你喜欢吃" {
		t.Fatalf("first user message = %#v", loaded[1])
	}
	if loaded[2].Role != schema.Assistant || loaded[2].Content != "我喜欢" {
		t.Fatalf("partial assistant = %#v", loaded[2])
	}
	if len(loaded[3].ToolCalls) != 1 || loaded[3].ToolCalls[0].Function.Arguments != `{"query":"火` {
		t.Fatalf("partial tool call = %#v", loaded[3])
	}
	if loaded[4].Role != schema.Tool || loaded[4].ToolCallID != "call-1" {
		t.Fatalf("partial tool result = %#v", loaded[4])
	}
	assertMetadata(t, loaded[5], true)
	if loaded[6].Role != schema.User || loaded[6].Content != "什么" {
		t.Fatalf("interrupted user message = %#v", loaded[6])
	}
	if err = manager.CompleteRound(ctx, 8, "character.one", roundID, CompressionOptions{MaxRounds: 10},
		schema.AssistantMessage("火锅。", nil)); err != nil {
		t.Fatal(err)
	}

	loaded, err = manager.Load(ctx, 8, "character.one", 0, CompressionOptions{MaxRounds: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 8 || loaded[7].Content != "火锅。" {
		t.Fatalf("completed interrupted round = %#v", loaded)
	}
	var stored int64
	if err = db.Model(&model.SessionMessage{}).Where("round_id = ?", roundID).Count(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored != 8 {
		t.Fatalf("stored rows = %d, want 8", stored)
	}
}

func TestAppendUserDoesNotDependOnSameValueUpdateRowsAffected(t *testing.T) {
	manager, db, _ := newTestManager(t)
	ctx := context.Background()
	zeroedUpdates := 0
	if err := db.Callback().Update().After("gorm:update").Register("test:zero_session_round_rows_affected", func(tx *gorm.DB) {
		if tx.Statement.Table == (model.SessionRound{}).TableName() {
			tx.RowsAffected = 0
			zeroedUpdates++
		}
	}); err != nil {
		t.Fatal(err)
	}

	roundID, err := manager.StartRound(ctx, 15, "character.one", schema.UserMessage("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.AppendInterrupted(ctx, 15, "character.one", roundID, schema.AssistantMessage("partial", nil)); err != nil {
		t.Fatal(err)
	}
	if zeroedUpdates != 1 {
		t.Fatalf("session round updates = %d, want 1", zeroedUpdates)
	}
	if err = manager.AppendUser(ctx, 15, "character.one", roundID, schema.UserMessage("continued")); err != nil {
		t.Fatal(err)
	}
	if zeroedUpdates != 1 {
		t.Fatalf("session round updates after append = %d, want no redundant update", zeroedUpdates)
	}
}

func TestToolResultIsCompactedPerMessageRow(t *testing.T) {
	manager, db, logs := newTestManager(t)
	ctx := context.Background()
	roundID, err := manager.StartRound(ctx, 14, "character.one", schema.UserMessage("find it"))
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("前", maxPersistedToolResultRunes) + strings.Repeat("后", 2_000)
	if err = manager.CompleteRound(ctx, 14, "character.one", roundID, CompressionOptions{MaxRounds: 10},
		schema.ToolMessage(content, "call-large", schema.WithToolName("lookup"))); err != nil {
		t.Fatal(err)
	}
	var record model.SessionMessage
	if err = db.Where("round_id = ? AND interrupted = ?", roundID, false).Order("id DESC").First(&record).Error; err != nil {
		t.Fatal(err)
	}
	message, err := decodeSessionMessage(record)
	if err != nil {
		t.Fatal(err)
	}
	if message.ToolCallID != "call-large" || message.ToolName != "lookup" {
		t.Fatalf("stored tool identity = (%q, %q)", message.ToolCallID, message.ToolName)
	}
	if !strings.Contains(message.Content, "tool result truncated for long-term history") || len([]rune(message.Content)) != maxPersistedToolResultRunes {
		t.Fatalf("stored tool result was not compacted: %q", message.Content)
	}
	if logs.FilterMessage("compacted tool results for session history").Len() != 1 {
		t.Fatal("missing tool result compaction log")
	}
}

func TestCompleteRoundRollsSummaryAfterConfiguredLimit(t *testing.T) {
	manager, db, logs := newTestManager(t)
	agent := &recordingAgent{responses: []agentResponse{
		{message: schema.AssistantMessage(" durable history ", nil)},
		{message: schema.AssistantMessage(" updated durable history ", nil)},
	}}
	opts := CompressionOptions{MaxRounds: 2, Agent: agent}
	firstID := completeTestRound(t, manager, 9, "character.one", opts, "first", "first reply")
	if agent.CallCount() != 0 {
		t.Fatalf("compression calls before limit = %d", agent.CallCount())
	}
	secondID := completeTestRound(t, manager, 9, "character.one", opts, "second", "second reply")
	if firstID == secondID {
		t.Fatal("round ids must be unique")
	}

	input := agent.Input(0)
	if input[len(input)-1].Role != schema.User || !strings.Contains(input[len(input)-1].Content, "2 newly completed Telegram conversation rounds") {
		t.Fatalf("compression instruction = %#v", input[len(input)-1])
	}
	assertMetadataBeforeEveryUser(t, input[:len(input)-1])

	thirdID := completeTestRound(t, manager, 9, "character.one", opts, "third", "third reply")
	if agent.CallCount() != 2 {
		t.Fatalf("compression calls after new round = %d, want 2", agent.CallCount())
	}
	rollingInput := agent.Input(1)
	if rollingInput[0].Role != schema.System || rollingInput[0].Content != "durable history" {
		t.Fatalf("previous summary = %#v", rollingInput[0])
	}
	rollingInstruction := rollingInput[len(rollingInput)-1]
	if rollingInstruction.Role != schema.User ||
		!strings.Contains(rollingInstruction.Content, "1 newly completed Telegram conversation round") ||
		!strings.Contains(rollingInstruction.Content, "previous compressed history") {
		t.Fatalf("rolling compression instruction = %#v", rollingInstruction)
	}
	assertMetadataBeforeEveryUser(t, rollingInput[1:len(rollingInput)-1])

	var summaries []model.SessionSummary
	if err := db.Order("id ASC").Find(&summaries).Error; err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || summaries[0].CutoffRoundID != secondID || summaries[1].CutoffRoundID != thirdID {
		t.Fatalf("summaries = %#v, cutoffs want %d and %d", summaries, secondID, thirdID)
	}
	loaded, err := manager.Load(context.Background(), 9, "character.one", 0, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Role != schema.System || loaded[0].Content != "updated durable history" {
		t.Fatalf("loaded summary = %#v", loaded)
	}
	for _, message := range []string{"session history compression threshold reached", "session history compression started", "session history compression completed"} {
		if logs.FilterMessage(message).Len() != 2 {
			t.Fatalf("log %q count = %d", message, logs.FilterMessage(message).Len())
		}
	}
}

func TestCompressionFailurePreservesMessageRows(t *testing.T) {
	manager, db, logs := newTestManager(t)
	compressErr := errors.New("model unavailable")
	agent := &recordingAgent{responses: []agentResponse{{err: compressErr}}}
	roundID, err := manager.StartRound(context.Background(), 10, "character.one", schema.UserMessage("keep user"))
	if err != nil {
		t.Fatal(err)
	}
	err = manager.CompleteRound(context.Background(), 10, "character.one", roundID, CompressionOptions{
		MaxRounds: 1, Agent: agent,
	}, schema.AssistantMessage("keep reply", nil))
	if !errors.Is(err, compressErr) {
		t.Fatalf("compression error = %v, want %v", err, compressErr)
	}
	var messageCount int64
	if err = db.Model(&model.SessionMessage{}).Where("round_id = ?", roundID).Count(&messageCount).Error; err != nil {
		t.Fatal(err)
	}
	if messageCount != 3 {
		t.Fatalf("preserved message rows = %d, want 3", messageCount)
	}
	var summaryCount int64
	if err = db.Model(&model.SessionSummary{}).Count(&summaryCount).Error; err != nil {
		t.Fatal(err)
	}
	if summaryCount != 0 || logs.FilterMessage("session history compression failed").Len() != 1 {
		t.Fatalf("summary count = %d", summaryCount)
	}
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
		completeTestRound(t, manager, input.userID, input.characterID, CompressionOptions{MaxRounds: 10}, input.text, input.text+" reply")
	}
	loaded, err := manager.Load(context.Background(), 1, "character.one", 0, CompressionOptions{MaxRounds: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertRoundMessages(t, loaded, false, "wanted", "wanted reply")
}

func TestAppendUserWaitsWhileCompressionRuns(t *testing.T) {
	manager, _, logs := newTestManager(t)
	releaseCompression := make(chan struct{})
	agent := &recordingAgent{
		started:   make(chan struct{}),
		release:   releaseCompression,
		responses: []agentResponse{{message: schema.AssistantMessage("summary", nil)}},
	}
	roundID, err := manager.StartRound(context.Background(), 12, "character.one", schema.UserMessage("user"))
	if err != nil {
		t.Fatal(err)
	}
	completeDone := make(chan error, 1)
	go func() {
		completeDone <- manager.CompleteRound(context.Background(), 12, "character.one", roundID, CompressionOptions{
			MaxRounds: 1, Agent: agent,
		}, schema.AssistantMessage("reply", nil))
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
	case err = <-waitDone:
		t.Fatalf("Wait returned during compression: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseCompression)
	if err = waitResult(t, completeDone); err != nil {
		t.Fatal(err)
	}
	if err = waitResult(t, waitDone); err != nil {
		t.Fatal(err)
	}
	if logs.FilterMessage("waiting for session operation").Len() == 0 {
		t.Fatal("missing session wait log")
	}
}

func completeTestRound(t *testing.T, manager *Manager, userID int64, characterID string, opts CompressionOptions, user, reply string) uint {
	t.Helper()
	roundID, err := manager.StartRound(context.Background(), userID, characterID, schema.UserMessage(user))
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.CompleteRound(context.Background(), userID, characterID, roundID, opts, schema.AssistantMessage(reply, nil)); err != nil {
		t.Fatal(err)
	}
	return roundID
}

func assertRoundMessages(t *testing.T, messages []*schema.Message, interrupted bool, user, assistant string) {
	t.Helper()
	if len(messages) != 3 {
		t.Fatalf("round messages = %#v", messages)
	}
	assertMetadata(t, messages[0], interrupted)
	if messages[1].Role != schema.User || messages[1].Content != user {
		t.Fatalf("user message = %#v", messages[1])
	}
	if messages[2].Role != schema.Assistant || messages[2].Content != assistant {
		t.Fatalf("assistant message = %#v", messages[2])
	}
}

func assertMetadata(t *testing.T, message *schema.Message, interrupted bool) {
	t.Helper()
	if message.Role != schema.System || !strings.Contains(message.Content, "<message_metadata>") || !strings.Contains(message.Content, "<current_time>") {
		t.Fatalf("message metadata = %#v", message)
	}
	if interrupted {
		if !strings.Contains(message.Content, "<interrupt_info>") || !strings.Contains(message.Content, "用户发送了一条新消息并打断了你") {
			t.Fatalf("interrupted metadata = %q", message.Content)
		}
	} else if strings.Contains(message.Content, "<interrupt_info>") || !strings.Contains(message.Content, "用户正常地发送了一条消息。") {
		t.Fatalf("normal metadata = %q", message.Content)
	}
}

func assertMetadataBeforeEveryUser(t *testing.T, messages []*schema.Message) {
	t.Helper()
	users := 0
	for index, message := range messages {
		if message.Role != schema.User {
			continue
		}
		users++
		if index == 0 {
			t.Fatal("user message has no preceding metadata")
		}
		assertMetadata(t, messages[index-1], strings.Contains(messages[index-1].Content, "<interrupt_info>"))
	}
	if users == 0 {
		t.Fatal("expected at least one user message")
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

func (a *recordingAgent) Name(context.Context) string { return "recording-agent" }

func (a *recordingAgent) Description(context.Context) string { return "records compression inputs" }

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

func waitResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result")
		return nil
	}
}

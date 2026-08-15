// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package chat

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chhongzh/atri-bot/internal/msgops"
	"github.com/chhongzh/atri-bot/internal/utils"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestConsumeMessageVariantStreamsOnlyAssistantText(t *testing.T) {
	chunks := []*schema.Message{
		{Role: schema.Assistant, ReasoningContent: "hidden reasoning "},
		{Role: schema.Assistant, Content: "first\n"},
		{Role: schema.Assistant, ReasoningContent: "stays hidden"},
		{Role: schema.Assistant, Content: "\nsecond"},
	}
	variant := &adk.MessageVariant{
		IsStreaming:   true,
		Role:          schema.Assistant,
		MessageStream: schema.StreamReaderFromArray(chunks),
	}

	var visible strings.Builder
	message, err := consumeMessageVariant(variant, func(chunk *schema.Message) error {
		visible.WriteString(msgops.AssistantDeltaText(chunk))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := visible.String(); got != "first\n\nsecond" {
		t.Fatalf("streamed text = %q, want only assistant content", got)
	}
	if message.Content != visible.String() {
		t.Fatalf("accumulated content = %q, streamed = %q", message.Content, visible.String())
	}
	if message.ReasoningContent != "hidden reasoning stays hidden" {
		t.Fatalf("accumulated reasoning = %q", message.ReasoningContent)
	}
}

func TestConsumeMessageVariantStopsOnStreamCallbackError(t *testing.T) {
	wantErr := errors.New("send failed")
	variant := &adk.MessageVariant{
		IsStreaming: true,
		Role:        schema.Assistant,
		MessageStream: schema.StreamReaderFromArray([]*schema.Message{
			{Role: schema.Assistant, Content: "first"},
			{Role: schema.Assistant, Content: "second"},
		}),
	}

	_, err := consumeMessageVariant(variant, func(*schema.Message) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("consume error = %v, want %v", err, wantErr)
	}
}

func TestConsumeMessageVariantSendsCompletedBlockBeforeStreamEnds(t *testing.T) {
	reader, writer := schema.Pipe[*schema.Message](0)
	variant := &adk.MessageVariant{
		IsStreaming:   true,
		Role:          schema.Assistant,
		MessageStream: reader,
	}
	sent := make(chan string, 1)
	done := make(chan error, 1)
	streamWriter := utils.NewAssistantStreamWriter(func(text string) error {
		sent <- text
		return nil
	})
	go func() {
		_, err := consumeMessageVariant(variant, func(chunk *schema.Message) error {
			return streamWriter.Write(msgops.AssistantDeltaText(chunk))
		})
		done <- err
	}()

	if closed := writer.Send(&schema.Message{Role: schema.Assistant, Content: "first block\n"}, nil); closed {
		t.Fatal("model stream closed before first chunk")
	}
	select {
	case text := <-sent:
		if text != "first block" {
			t.Fatalf("sent block = %q", text)
		}
	case <-time.After(time.Second):
		t.Fatal("completed block was not sent while model stream remained open")
	}
	select {
	case err := <-done:
		t.Fatalf("stream consumption finished before EOF: %v", err)
	default:
	}

	writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConsumeMessageVariantFlushesTextBeforeToolCallCompletes(t *testing.T) {
	reader, writer := schema.Pipe[*schema.Message](0)
	variant := &adk.MessageVariant{
		IsStreaming:   true,
		Role:          schema.Assistant,
		MessageStream: reader,
	}
	sent := make(chan string, 1)
	done := make(chan error, 1)
	streamWriter := utils.NewAssistantStreamWriter(func(text string) error {
		sent <- text
		return nil
	})
	go func() {
		_, err := consumeMessageVariant(variant, func(chunk *schema.Message) error {
			if err := streamWriter.Write(msgops.AssistantDeltaText(chunk)); err != nil {
				return err
			}
			if len(msgops.ToolCalls(chunk)) > 0 {
				return streamWriter.Seal()
			}
			return nil
		})
		done <- err
	}()

	if closed := writer.Send(&schema.Message{Role: schema.Assistant, Content: "checking now"}, nil); closed {
		t.Fatal("model stream closed before text chunk")
	}
	if closed := writer.Send(schema.AssistantMessage("", []schema.ToolCall{{
		ID:       "call-1",
		Function: schema.FunctionCall{Name: "lookup", Arguments: "{"},
	}}), nil); closed {
		t.Fatal("model stream closed before tool-call chunk")
	}
	select {
	case text := <-sent:
		if text != "checking now" {
			t.Fatalf("sent block = %q", text)
		}
	case <-time.After(time.Second):
		t.Fatal("pending assistant text was not flushed at tool call")
	}

	writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

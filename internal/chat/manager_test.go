package chat

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestAssistantStreamWriterBuffersUntilFlush(t *testing.T) {
	var sent []string
	writer := newAssistantStreamWriter(func(text string) error {
		sent = append(sent, text)
		return nil
	})
	if err := writer.Write("第一段\n"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Fatalf("sent before boundary = %#v, want no messages", sent)
	}
	if err := writer.Write("\n第二段"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Fatalf("sent before flush = %#v, want no messages", sent)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if got, want := sent, []string{"第一段", "第二段"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sent after final flush = %#v, want %#v", got, want)
	}
}

func TestAssistantStreamWriterDiscardsDraft(t *testing.T) {
	var sent []string
	writer := newAssistantStreamWriter(func(text string) error {
		sent = append(sent, text)
		return nil
	})
	if err := writer.Write("第一段\n\n尚未完成"); err != nil {
		t.Fatal(err)
	}
	writer.Discard()
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Fatalf("sent discarded draft = %#v, want no messages", sent)
	}
}

func TestAssistantStreamWriterPreservesWhitespace(t *testing.T) {
	var sent []string
	writer := newAssistantStreamWriter(func(text string) error {
		sent = append(sent, text)
		return nil
	})
	if err := writer.Write("  第一段  \n\n\t第二段\t"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if got, want := sent, []string{"  第一段  ", "\t第二段\t"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sent = %#v, want %#v", got, want)
	}
}

func TestConsumeMessageVariantBuffersChunksUntilStreamEnds(t *testing.T) {
	firstChunkRead := make(chan struct{})
	allowSecondChunk := make(chan struct{})
	stream, producer := schema.Pipe[*schema.Message](0)
	go func() {
		defer producer.Close()
		producer.Send(schema.AssistantMessage("第一段\n\n", nil), nil)
		close(firstChunkRead)
		<-allowSecondChunk
		producer.Send(schema.AssistantMessage("第二段", nil), nil)
	}()
	variant := &adk.MessageVariant{IsStreaming: true, MessageStream: stream, Role: schema.Assistant}

	sent := make(chan string, 2)
	writer := newAssistantStreamWriter(func(text string) error {
		sent <- text
		return nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := consumeMessageVariant(variant, func(chunk *schema.Message) error {
			return writer.Write(chunk.Content)
		})
		if err == nil {
			err = writer.Flush()
		}
		done <- err
	}()

	<-firstChunkRead
	select {
	case got := <-sent:
		t.Fatalf("sent block before stream completed = %q", got)
	default:
	}
	select {
	case err := <-done:
		t.Fatalf("stream completed before second chunk: %v", err)
	default:
	}
	close(allowSecondChunk)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"第一段", "第二段"} {
		if got := <-sent; got != want {
			t.Fatalf("sent block = %q, want %q", got, want)
		}
	}
}

func TestConsumeMessageVariantPropagatesChunkHandlerError(t *testing.T) {
	want := errors.New("chunk handler failed")
	stream := schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("partial", nil)})
	stream.Close()
	variant := &adk.MessageVariant{IsStreaming: true, MessageStream: stream, Role: schema.Assistant}
	_, err := consumeMessageVariant(variant, func(*schema.Message) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("consumeMessageVariant() error = %v, want %v", err, want)
	}
}

func TestUserStateRequeuesInterruptedInputsBeforeNewInputs(t *testing.T) {
	state := &UserState{queuedInputs: []string{"开始任务"}}
	if got, want := state.startTurnInputs(), []string{"开始任务"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("startTurnInputs() = %#v, want %#v", got, want)
	}

	interrupted := append(state.finishTurnInputs(), "补充参数")
	state.requeueInputs(interrupted)

	if got, want := state.startTurnInputs(), []string{"开始任务", "补充参数"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("requeued inputs = %#v, want %#v", got, want)
	}
}

// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package chat

import (
	"reflect"
	"testing"
)

func TestAssistantStreamWriterSendsCompletedBlocksImmediately(t *testing.T) {
	var sent []string
	writer := newAssistantStreamWriter(func(text string) error {
		sent = append(sent, text)
		return nil
	})

	if err := writer.Write("first"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Fatalf("sent before boundary = %#v", sent)
	}
	if err := writer.Write(" block\\msecond"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent, []string{"first block"}) {
		t.Fatalf("sent after boundary = %#v", sent)
	}
	if err := writer.Write(" block\\mthird"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent, []string{"first block", "second block"}) {
		t.Fatalf("sent after second boundary = %#v", sent)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent, []string{"first block", "second block", "third"}) {
		t.Fatalf("sent after flush = %#v", sent)
	}
}

func TestAssistantStreamWriterPreservesInlineNewlines(t *testing.T) {
	var sent []string
	writer := newAssistantStreamWriter(func(text string) error {
		sent = append(sent, text)
		return nil
	})

	if err := writer.Write("第一行\n第二行\\m第三行"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent, []string{"第一行\n第二行"}) {
		t.Fatalf("sent after boundary = %#v", sent)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent, []string{"第一行\n第二行", "第三行"}) {
		t.Fatalf("sent after flush = %#v", sent)
	}
}

func TestAssistantStreamWriterHoldsIncompleteBoundaryAcrossChunks(t *testing.T) {
	var sent []string
	writer := newAssistantStreamWriter(func(text string) error {
		sent = append(sent, text)
		return nil
	})

	if err := writer.Write("第一行\\"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Fatalf("sent before escape completed = %#v", sent)
	}
	if err := writer.Write("m第二行\n第三行"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent, []string{"第一行"}) {
		t.Fatalf("sent after boundary = %#v", sent)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent, []string{"第一行", "第二行\n第三行"}) {
		t.Fatalf("sent after flush = %#v", sent)
	}
}

func TestAssistantStreamWriterFlushKeepsTrailingBackslash(t *testing.T) {
	var sent []string
	writer := newAssistantStreamWriter(func(text string) error {
		sent = append(sent, text)
		return nil
	})

	if err := writer.Write("结尾有个反斜杠\\"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent, []string{"结尾有个反斜杠\\"}) {
		t.Fatalf("sent after flush = %#v", sent)
	}
}

func TestAssistantStreamWriterDropsEmptyBlocks(t *testing.T) {
	var sent []string
	writer := newAssistantStreamWriter(func(text string) error {
		sent = append(sent, text)
		return nil
	})

	if err := writer.Write("\\m第二行\\m\\m第三行"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent, []string{"第二行"}) {
		t.Fatalf("sent after boundaries = %#v", sent)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent, []string{"第二行", "第三行"}) {
		t.Fatalf("sent after flush = %#v", sent)
	}
}

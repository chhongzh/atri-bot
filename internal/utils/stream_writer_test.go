package utils

import (
	"reflect"
	"testing"
)

func TestAssistantStreamWriterSendsCompletedBlocksImmediately(t *testing.T) {
	var sent []string
	writer := NewAssistantStreamWriter(func(text string) error {
		sent = append(sent, text)
		return nil
	})

	if err := writer.Write("first"); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Fatalf("sent before boundary = %#v", sent)
	}
	if err := writer.Write(" block\n\nsecond"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent, []string{"first block"}) {
		t.Fatalf("sent after boundary = %#v", sent)
	}
	if err := writer.Write(" block\n\nthird"); err != nil {
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

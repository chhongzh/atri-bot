package utils

import (
	"reflect"
	"testing"
)

func TestSplitTelegramTextUsesRuneLimit(t *testing.T) {
	parts := SplitTelegramText("你好世界", 2)
	if len(parts) != 2 || parts[0] != "你好" || parts[1] != "世界" {
		t.Fatalf("SplitTelegramText() = %#v", parts)
	}
}

func TestSplitTelegramTextUsesDoubleNewlineAsMessageBoundary(t *testing.T) {
	parts := SplitTelegramText("first\n\nsecond\n\nthird", telegramTextLimit)
	if len(parts) != 3 {
		t.Fatalf("got %d parts, want 3: %#v", len(parts), parts)
	}
	for i, want := range []string{"first", "second", "third"} {
		if parts[i] != want {
			t.Errorf("part %d = %q, want %q", i, parts[i], want)
		}
	}
}

func TestSplitTelegramTextPreservesWhitespace(t *testing.T) {
	text := "  first  \n\n\tsecond\t"
	if got, want := SplitTelegramText(text, telegramTextLimit), []string{"  first  ", "\tsecond\t"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitTelegramText() = %#v, want %#v", got, want)
	}
}

func TestSplitTelegramTextSplitsLongMessageBlocks(t *testing.T) {
	parts := SplitTelegramText("abcdef\n\nghijkl", 3)
	want := []string{"abc", "def", "ghi", "jkl"}
	if len(parts) != len(want) {
		t.Fatalf("got %#v, want %#v", parts, want)
	}
	for i := range want {
		if parts[i] != want[i] {
			t.Errorf("part %d = %q, want %q", i, parts[i], want[i])
		}
	}
}

// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package utils

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitTelegramTextPreservesMarkdownParagraphs(t *testing.T) {
	text := "*标题*\n\n正文中的 `code`"

	parts := SplitTelegramText(text, 100)
	if len(parts) != 1 || parts[0] != text {
		t.Fatalf("parts = %#v", parts)
	}
}

func TestSplitTelegramTextPreservesContentWhenSplitting(t *testing.T) {
	text := "第一段内容\n第二段内容\n第三段内容"
	limit := 8

	parts := SplitTelegramText(text, limit)
	if strings.Join(parts, "") != text {
		t.Fatalf("joined parts = %q, want %q", strings.Join(parts, ""), text)
	}
	for _, part := range parts {
		if utf8.RuneCountInString(part) > limit {
			t.Fatalf("part exceeds limit: %q", part)
		}
	}
}

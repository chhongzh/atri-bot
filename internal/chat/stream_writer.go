// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package chat

import "strings"

// AssistantStreamWriter buffers assistant stream text and sends complete
// instant messages as soon as their boundaries arrive.
//
// A backslash followed by the letter m ("\m") separates two messages.
// Literal newline characters remain inside the current message, and empty
// blocks are dropped.
type assistantStreamWriter struct {
	pending string
	send    func(string) error
}

func newAssistantStreamWriter(send func(string) error) *assistantStreamWriter {
	return &assistantStreamWriter{send: send}
}

func (w *assistantStreamWriter) Write(text string) error {
	w.pending += text
	for {
		block, rest, ok := splitMessages(w.pending, false)
		if !ok {
			return nil
		}
		if block != "" {
			if err := w.send(block); err != nil {
				return err
			}
		}
		w.pending = rest
	}
}

func (w *assistantStreamWriter) Seal() error {
	return w.Flush()
}

func (w *assistantStreamWriter) Flush() error {
	if w.pending == "" {
		return nil
	}
	block, _, _ := splitMessages(w.pending, true)
	if block != "" {
		if err := w.send(block); err != nil {
			return err
		}
	}
	w.pending = ""
	return nil
}

func (w *assistantStreamWriter) Discard() {
	w.pending = ""
}

// splitMessages scans text for the first message boundary and returns the
// message text before it together with the remaining text after it. A trailing
// backslash is held back when atEOF is false so that a boundary split across
// chunks is not emitted as text; at EOF it is retained as a literal character.
func splitMessages(text string, atEOF bool) (block, rest string, found bool) {
	var builder strings.Builder
	for index := 0; index < len(text); {
		if text[index] == '\\' {
			if index+1 >= len(text) {
				if !atEOF {
					return "", "", false
				}
				builder.WriteByte('\\')
				index++
				continue
			}
			if text[index+1] == 'm' {
				return builder.String(), text[index+2:], true
			}
		}
		builder.WriteByte(text[index])
		index++
	}
	if atEOF {
		return builder.String(), "", false
	}
	return "", "", false
}

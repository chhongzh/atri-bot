// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package utils

import "strings"

// AssistantStreamWriter buffers assistant stream text and sends complete
// instant messages as soon as their boundaries arrive.
//
// A literal newline character separates two messages, so each line of output
// is delivered as its own message and empty blocks are dropped. A backslash
// followed by the letter n ("\n") is an escaped newline and becomes a line
// break inside the current message instead.
type AssistantStreamWriter struct {
	pending string
	send    func(string) error
}

func NewAssistantStreamWriter(send func(string) error) *AssistantStreamWriter {
	return &AssistantStreamWriter{send: send}
}

func (w *AssistantStreamWriter) Write(text string) error {
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

func (w *AssistantStreamWriter) Seal() error {
	return w.Flush()
}

func (w *AssistantStreamWriter) Flush() error {
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

func (w *AssistantStreamWriter) Discard() {
	w.pending = ""
}

// splitMessages scans text for the first message boundary and returns the
// unescaped message text before it together with the remaining text after it.
// A trailing backslash is held back when atEOF is false so that an escaped
// newline split across chunks is not misinterpreted; when atEOF is true it is
// kept as a literal character and the whole remainder is returned.
func splitMessages(text string, atEOF bool) (block, rest string, found bool) {
	var builder strings.Builder
	for index := 0; index < len(text); {
		switch text[index] {
		case '\\':
			if index+1 >= len(text) {
				if !atEOF {
					return "", "", false
				}
				builder.WriteByte('\\')
				index++
				continue
			}
			if text[index+1] == 'n' {
				builder.WriteByte('\n')
				index += 2
				continue
			}
			builder.WriteByte('\\')
			index++
		case '\n':
			return builder.String(), text[index+1:], true
		case '\r':
			if index+1 >= len(text) {
				if !atEOF {
					return "", "", false
				}
				builder.WriteByte('\r')
				index++
				continue
			}
			if text[index+1] == '\n' {
				return builder.String(), text[index+2:], true
			}
			builder.WriteByte('\r')
			index++
		default:
			builder.WriteByte(text[index])
			index++
		}
	}
	if atEOF {
		return builder.String(), "", false
	}
	return "", "", false
}

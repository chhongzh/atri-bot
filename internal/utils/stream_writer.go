// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package utils

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
		blockEnd, delimiterEnd, ok := findParagraphBoundary(w.pending)
		if !ok {
			return nil
		}
		block := w.pending[:blockEnd]
		if block != "" {
			if err := w.send(block); err != nil {
				return err
			}
		}
		w.pending = w.pending[delimiterEnd:]
	}
}

func findParagraphBoundary(text string) (int, int, bool) {
	for index := 0; index < len(text); index++ {
		firstEnd, ok := consumeLineBreak(text, index)
		if !ok {
			continue
		}
		secondStart := firstEnd
		for secondStart < len(text) && (text[secondStart] == ' ' || text[secondStart] == '\t') {
			secondStart++
		}
		secondEnd, ok := consumeLineBreak(text, secondStart)
		if ok {
			return index, secondEnd, true
		}
	}
	return 0, 0, false
}

func consumeLineBreak(text string, index int) (int, bool) {
	if index >= len(text) {
		return index, false
	}
	if text[index] == '\n' {
		return index + 1, true
	}
	if text[index] == '\r' && index+1 < len(text) && text[index+1] == '\n' {
		return index + 2, true
	}
	return index, false
}

func (w *AssistantStreamWriter) Seal() error {
	return w.Flush()
}

func (w *AssistantStreamWriter) Flush() error {
	if w.pending == "" {
		return nil
	}
	if err := w.send(w.pending); err != nil {
		return err
	}
	w.pending = ""
	return nil
}

func (w *AssistantStreamWriter) Discard() {
	w.pending = ""
}

// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package utils

import "strings"

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
		boundary := strings.Index(w.pending, "\n\n")
		if boundary < 0 {
			return nil
		}
		block := w.pending[:boundary]
		if block != "" {
			if err := w.send(block); err != nil {
				return err
			}
		}
		w.pending = w.pending[boundary+2:]
	}
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

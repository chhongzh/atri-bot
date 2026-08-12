package utils

import "strings"

type AssistantStreamWriter struct {
	pending string
	blocks  []string
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
		w.appendBlock(w.pending[:boundary])
		w.pending = w.pending[boundary+2:]
	}
}

func (w *AssistantStreamWriter) Seal() {
	w.appendBlock(w.pending)
	w.pending = ""
}

func (w *AssistantStreamWriter) Flush() error {
	w.Seal()
	for index, block := range w.blocks {
		if err := w.send(block); err != nil {
			w.blocks = w.blocks[index:]
			return err
		}
	}
	w.blocks = nil
	return nil
}

func (w *AssistantStreamWriter) Discard() {
	w.pending = ""
	w.blocks = nil
}

func (w *AssistantStreamWriter) appendBlock(text string) {
	if text != "" {
		w.blocks = append(w.blocks, text)
	}
}

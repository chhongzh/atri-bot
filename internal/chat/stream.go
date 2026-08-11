package chat

import "strings"

type assistantStreamWriter struct {
	pending string
	blocks  []string
	send    func(string) error
}

func newAssistantStreamWriter(send func(string) error) *assistantStreamWriter {
	return &assistantStreamWriter{send: send}
}

func (w *assistantStreamWriter) Write(text string) error {
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

func (w *assistantStreamWriter) Seal() {
	w.appendBlock(w.pending)
	w.pending = ""
}

func (w *assistantStreamWriter) Flush() error {
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

func (w *assistantStreamWriter) Discard() {
	w.pending = ""
	w.blocks = nil
}

func (w *assistantStreamWriter) appendBlock(text string) {
	if text != "" {
		w.blocks = append(w.blocks, text)
	}
}

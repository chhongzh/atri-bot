package chat

import "strings"

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
		boundary := strings.Index(w.pending, "\n\n")
		if boundary < 0 {
			return nil
		}
		if err := w.sendBlock(w.pending[:boundary]); err != nil {
			return err
		}
		w.pending = w.pending[boundary+2:]
	}
}

func (w *assistantStreamWriter) Flush() error {
	if err := w.sendBlock(w.pending); err != nil {
		return err
	}
	w.pending = ""
	return nil
}

func (w *assistantStreamWriter) sendBlock(text string) error {
	if text == "" || w.send == nil {
		return nil
	}
	return w.send(text)
}

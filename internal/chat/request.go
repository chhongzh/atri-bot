// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package chat

import (
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"gopkg.in/telebot.v4"
)

type Request struct {
	Context   telebot.Context
	Text      string
	FileRefs  []string
	RoundID   uint
	Revision  uint64
	MessageID int

	ReceivedAt time.Time
	QueuedAt   time.Time
	TurnAt     time.Time

	done chan error
	once sync.Once
}

func (r *Request) message() *schema.Message {
	message := schema.UserMessage(r.Text)
	if len(r.FileRefs) > 0 {
		message.Extra = map[string]any{fileRefsKey: r.FileRefs}
	}
	return message
}

func newRequest(c telebot.Context, text string, receivedAt time.Time) *Request {
	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}
	request := &Request{
		Context:    c,
		Text:       text,
		ReceivedAt: receivedAt,
		done:       make(chan error, 1),
	}
	if message := c.Message(); message != nil {
		request.MessageID = message.ID
	}
	return request
}

func (r *Request) complete(err error) {
	r.once.Do(func() {
		r.done <- err
		close(r.done)
	})
}

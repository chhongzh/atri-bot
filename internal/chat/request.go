// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package chat

import (
	"sync"

	"gopkg.in/telebot.v4"
)

type Request struct {
	Context telebot.Context
	Text    string

	done chan error
	once sync.Once
}

func newRequest(c telebot.Context, text string) *Request {
	return &Request{Context: c, Text: text, done: make(chan error, 1)}
}

func (r *Request) complete(err error) {
	r.once.Do(func() {
		r.done <- err
		close(r.done)
	})
}

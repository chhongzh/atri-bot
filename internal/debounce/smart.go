// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package debounce

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrSuperseded = errors.New("debounce wait superseded")
	ErrClosed     = errors.New("debouncer closed")
)

// Smart waits for a quiet interval per key. A new waiter supersedes the
// previous waiter for the same key and restarts the interval.
type Smart[K comparable] struct {
	delay time.Duration

	mu      sync.Mutex
	waiters map[K]*waiter
	closed  bool
}

type waiter struct {
	timer *time.Timer
	done  chan error
}

func NewSmart[K comparable](delay time.Duration) *Smart[K] {
	return &Smart[K]{
		delay:   delay,
		waiters: make(map[K]*waiter),
	}
}

// Wait returns nil after the key has been quiet for the configured interval.
// It returns ErrSuperseded when a newer waiter arrives for the same key.
func (d *Smart[K]) Wait(ctx context.Context, key K) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return ErrClosed
	}

	current := &waiter{
		timer: time.NewTimer(d.delay),
		done:  make(chan error, 1),
	}
	if previous := d.waiters[key]; previous != nil {
		previous.timer.Stop()
		previous.done <- ErrSuperseded
	}
	d.waiters[key] = current
	d.mu.Unlock()

	select {
	case <-current.timer.C:
		d.mu.Lock()
		if d.waiters[key] == current {
			delete(d.waiters, key)
			d.mu.Unlock()
			return nil
		}
		d.mu.Unlock()
		return <-current.done
	case err := <-current.done:
		return err
	case <-ctx.Done():
		d.mu.Lock()
		if d.waiters[key] == current {
			delete(d.waiters, key)
			current.timer.Stop()
		}
		d.mu.Unlock()
		return ctx.Err()
	}
}

// Close releases all current and future waiters with ErrClosed.
func (d *Smart[K]) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}

	d.closed = true
	for key, current := range d.waiters {
		current.timer.Stop()
		current.done <- ErrClosed
		delete(d.waiters, key)
	}
}

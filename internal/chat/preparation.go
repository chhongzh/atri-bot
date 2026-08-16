// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package chat

import (
	"context"
	"time"

	"github.com/chhongzh/atri-bot/internal/errs"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

// Preparation represents an in-flight user state load. Its lifecycle belongs
// to Manager, so canceling one caller does not cancel the shared load.
type Preparation struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error
}

func completedPreparation(err error) *Preparation {
	preparation := &Preparation{done: make(chan struct{}), err: err}
	close(preparation.done)
	return preparation
}

// Wait waits for state preparation without taking ownership of the shared
// load. Canceling ctx only stops this wait.
func (p *Preparation) Wait(ctx context.Context) error {
	select {
	case <-p.done:
		return p.err
	default:
	}

	select {
	case <-p.done:
		return p.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Prepare starts loading the sender's state immediately or joins the load
// already in progress for that user.
func (m *Manager) Prepare(c telebot.Context) *Preparation {
	userID := c.Sender().ID
	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return completedPreparation(errs.ErrStateStopped)
	}
	if preparation := m.preparations[userID]; preparation != nil {
		m.mu.Unlock()
		return preparation
	}
	if state := m.states[userID]; state != nil && !state.isStale() {
		state.TelebotContext = c
		state.touch()
		m.mu.Unlock()
		return completedPreparation(nil)
	}

	ctx, cancel := context.WithCancel(m.ctx)
	preparation := &Preparation{done: make(chan struct{}), cancel: cancel}
	m.preparations[userID] = preparation
	m.mu.Unlock()

	m.logger.Debug("chat state preparation started", zap.Int64("user_id", userID))
	go m.runPreparation(ctx, userID, c, preparation)
	return preparation
}

func (m *Manager) runPreparation(
	ctx context.Context,
	userID int64,
	c telebot.Context,
	preparation *Preparation,
) {
	defer preparation.cancel()
	startedAt := time.Now()
	_, preparation.err = m.stateForPreparation(ctx, userID, c)
	close(preparation.done)

	m.mu.Lock()
	if m.preparations[userID] == preparation {
		delete(m.preparations, userID)
	}
	m.mu.Unlock()

	fields := []zap.Field{
		zap.Int64("user_id", userID),
		zap.Duration("elapsed", time.Since(startedAt)),
	}
	if preparation.err != nil {
		m.logger.Debug("chat state preparation failed", append(fields, zap.Error(preparation.err))...)
		return
	}
	m.logger.Debug("chat state preparation completed", fields...)
}

func (m *Manager) cancelPreparation(userID int64) {
	m.mu.Lock()
	preparation := m.preparations[userID]
	if preparation != nil {
		delete(m.preparations, userID)
		preparation.cancel()
	}
	m.mu.Unlock()
	if preparation != nil {
		<-preparation.done
	}
}

func (m *Manager) cancelAllPreparations(stopping bool) {
	m.mu.Lock()
	if stopping {
		m.stopping = true
	}
	preparations := make([]*Preparation, 0, len(m.preparations))
	for userID, preparation := range m.preparations {
		preparations = append(preparations, preparation)
		preparation.cancel()
		delete(m.preparations, userID)
	}
	m.mu.Unlock()
	for _, preparation := range preparations {
		<-preparation.done
	}
}

// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package chat

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

func TestPrepareSharesInflightStateLoad(t *testing.T) {
	wantErr := errors.New("load failed")
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	manager := newPreparationTestManager(t, func(context.Context, int64, telebot.Context) (*UserState, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return nil, wantErr
	})
	c := preparationTestContext(7)

	first := manager.Prepare(c)
	awaitSignal(t, started)
	second := manager.Prepare(c)
	if first != second {
		t.Fatal("concurrent Prepare calls did not share the same preparation")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("state load calls = %d, want 1", got)
	}

	close(release)
	if err := first.Wait(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("first Wait error = %v, want %v", err, wantErr)
	}
	if err := second.Wait(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("second Wait error = %v, want %v", err, wantErr)
	}
}

func TestPreparationCallerCancellationDoesNotCancelSharedLoad(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	manager := newPreparationTestManager(t, func(ctx context.Context, _ int64, _ telebot.Context) (*UserState, error) {
		close(started)
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	c := preparationTestContext(7)
	preparation := manager.Prepare(c)
	awaitSignal(t, started)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := preparation.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Wait error = %v, want %v", err, context.Canceled)
	}
	if joined := manager.Prepare(c); joined != preparation {
		t.Fatal("caller cancellation removed the shared preparation")
	}

	close(release)
	if err := preparation.Wait(context.Background()); err != nil {
		t.Fatalf("shared preparation error = %v", err)
	}
}

func TestInvalidateCancelsInflightPreparation(t *testing.T) {
	started := make(chan struct{})
	manager := newPreparationTestManager(t, func(ctx context.Context, _ int64, _ telebot.Context) (*UserState, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	preparation := manager.Prepare(preparationTestContext(7))
	awaitSignal(t, started)

	manager.Invalidate(7)
	if err := preparation.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait error = %v, want %v", err, context.Canceled)
	}
}

func newPreparationTestManager(
	t *testing.T,
	load func(context.Context, int64, telebot.Context) (*UserState, error),
) *Manager {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		logger:              zap.NewNop(),
		ctx:                 ctx,
		cancel:              cancel,
		states:              make(map[int64]*UserState),
		preparations:        make(map[int64]*Preparation),
		stateForPreparation: load,
	}
	t.Cleanup(manager.Shutdown)
	return manager
}

func preparationTestContext(userID int64) telebot.Context {
	return telebot.NewContext(nil, telebot.Update{Message: &telebot.Message{
		Sender: &telebot.User{ID: userID},
	}})
}

func awaitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

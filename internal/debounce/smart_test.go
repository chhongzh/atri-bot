// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package debounce

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestSmartRestartsDelayForSameKey(t *testing.T) {
	const delay = 80 * time.Millisecond
	debouncer := NewSmart[int64](delay)
	t.Cleanup(debouncer.Close)

	first := waitAsync(debouncer, 1)
	firstWaiter := waitForWaiter(t, debouncer, 1, nil)
	time.Sleep(delay / 2)
	second := waitAsync(debouncer, 1)
	secondWaiter := waitForWaiter(t, debouncer, 1, firstWaiter)

	assertWaitResult(t, first, ErrSuperseded, delay)
	time.Sleep(delay / 2)
	thirdStarted := time.Now()
	third := waitAsync(debouncer, 1)
	waitForWaiter(t, debouncer, 1, secondWaiter)

	assertWaitResult(t, second, ErrSuperseded, delay)
	select {
	case err := <-third:
		t.Fatalf("third wait completed before quiet interval: %v", err)
	case <-time.After(delay / 2):
	}

	assertWaitResult(t, third, nil, delay)
	if elapsed := time.Since(thirdStarted); elapsed < delay {
		t.Fatalf("third wait elapsed = %v, want at least %v", elapsed, delay)
	}
}

func TestSmartKeepsKeysIndependent(t *testing.T) {
	const delay = 60 * time.Millisecond
	debouncer := NewSmart[int64](delay)
	t.Cleanup(debouncer.Close)

	firstUser := waitAsync(debouncer, 1)
	firstWaiter := waitForWaiter(t, debouncer, 1, nil)
	secondUser := waitAsync(debouncer, 2)
	waitForWaiter(t, debouncer, 2, nil)
	replacement := waitAsync(debouncer, 1)
	waitForWaiter(t, debouncer, 1, firstWaiter)

	assertWaitResult(t, firstUser, ErrSuperseded, delay)
	assertWaitResult(t, secondUser, nil, 2*delay)
	assertWaitResult(t, replacement, nil, 2*delay)
}

func TestSmartWaitHonorsContextCancellation(t *testing.T) {
	debouncer := NewSmart[int64](time.Hour)
	t.Cleanup(debouncer.Close)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- debouncer.Wait(ctx, 1)
	}()
	cancel()

	assertWaitResult(t, result, context.Canceled, time.Second)

	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := debouncer.Wait(ctx, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestSmartCanceledContextDoesNotSupersedeCurrentWaiter(t *testing.T) {
	debouncer := NewSmart[int64](time.Hour)
	current := waitAsync(debouncer, 1)
	waitForWaiter(t, debouncer, 1, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := debouncer.Wait(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait error = %v, want %v", err, context.Canceled)
	}
	select {
	case err := <-current:
		t.Fatalf("current waiter was unexpectedly released: %v", err)
	default:
	}

	debouncer.Close()
	assertWaitResult(t, current, ErrClosed, time.Second)
}

func TestSmartCloseReleasesWaitersAndRejectsNewOnes(t *testing.T) {
	debouncer := NewSmart[int64](time.Hour)
	first := waitAsync(debouncer, 1)
	waitForWaiter(t, debouncer, 1, nil)
	second := waitAsync(debouncer, 2)
	waitForWaiter(t, debouncer, 2, nil)

	debouncer.Close()
	debouncer.Close()

	assertWaitResult(t, first, ErrClosed, time.Second)
	assertWaitResult(t, second, ErrClosed, time.Second)
	if err := debouncer.Wait(context.Background(), 3); !errors.Is(err, ErrClosed) {
		t.Fatalf("Wait after Close error = %v, want %v", err, ErrClosed)
	}
}

func waitAsync(debouncer *Smart[int64], key int64) <-chan error {
	result := make(chan error, 1)
	go func() {
		result <- debouncer.Wait(context.Background(), key)
	}()
	return result
}

func waitForWaiter(t *testing.T, debouncer *Smart[int64], key int64, previous *waiter) *waiter {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		debouncer.mu.Lock()
		current := debouncer.waiters[key]
		debouncer.mu.Unlock()
		if current != nil && current != previous {
			return current
		}
		runtime.Gosched()
	}
	t.Fatalf("waiter for key %d was not registered", key)
	return nil
}

func assertWaitResult(t *testing.T, result <-chan error, want error, timeout time.Duration) {
	t.Helper()
	select {
	case err := <-result:
		if !errors.Is(err, want) {
			t.Fatalf("Wait error = %v, want %v", err, want)
		}
	case <-time.After(timeout):
		t.Fatalf("Wait did not return within %v", timeout)
	}
}

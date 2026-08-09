package runner

import (
	"sync"
	"testing"

	"gopkg.in/telebot.v4"
)

func TestCommandResultDeletesWhenUserSendsNewMessage(t *testing.T) {
	runner := New(nil, nil, nil)
	user := &telebot.User{ID: 1}

	var calls int
	runner.setSystemResultDelete(user, func() { calls++ })
	runner.deleteSystemResult(user)

	if calls != 1 {
		t.Fatalf("delete callback calls = %d, want 1", calls)
	}
	runner.deleteSystemResult(user)
	if calls != 1 {
		t.Fatalf("delete callback calls after second message = %d, want 1", calls)
	}
}

func TestCommandResultReplacesPreviousCallback(t *testing.T) {
	runner := New(nil, nil, nil)
	user := &telebot.User{ID: 1}

	var firstCalls, secondCalls int
	runner.setSystemResultDelete(user, func() { firstCalls++ })
	runner.setSystemResultDelete(user, func() { secondCalls++ })
	runner.deleteSystemResult(user)

	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("callback calls = (%d, %d), want (1, 1)", firstCalls, secondCalls)
	}
}

func TestCommandResultDeletesOnStop(t *testing.T) {
	runner := New(nil, nil, nil)

	var mu sync.Mutex
	deleted := make(map[int64]bool)
	for _, id := range []int64{1, 2} {
		userID := id
		runner.setSystemResultDelete(&telebot.User{ID: userID}, func() {
			mu.Lock()
			deleted[userID] = true
			mu.Unlock()
		})
	}
	runner.deleteAllSystemResults()

	mu.Lock()
	defer func() {
		mu.Unlock()
	}()
	if !deleted[1] || !deleted[2] {
		t.Fatalf("deleted users = %#v, want both users", deleted)
	}
}

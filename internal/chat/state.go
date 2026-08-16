// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package chat

import (
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/telebot.v4"
)

type UserState struct {
	UserID         int64
	CharacterID    string
	MaxRounds      int
	Agent          *adk.ChatModelAgent
	Runner         *adk.Runner
	TurnLoop       *adk.TurnLoop[*Request, *schema.Message]
	TelebotContext telebot.Context
	CreatedAt      time.Time
	LastActiveAt   time.Time

	mu            sync.RWMutex
	loopMu        sync.Mutex
	closed        bool
	preempted     sync.Map
	roundMu       sync.Mutex
	activeRoundID uint
	roundRevision uint64
	mcpClose      func()
	stale         bool
}

// ActiveUser describes an in-memory chat state without exposing credentials.
type ActiveUser struct {
	UserID       int64
	CharacterID  string
	MaxRounds    int
	CreatedAt    time.Time
	LastActiveAt time.Time
}

func (s *UserState) touch() {
	s.mu.Lock()
	s.LastActiveAt = time.Now()
	s.mu.Unlock()
}

func (s *UserState) activeUser() ActiveUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ActiveUser{
		UserID:       s.UserID,
		CharacterID:  s.CharacterID,
		MaxRounds:    s.MaxRounds,
		CreatedAt:    s.CreatedAt,
		LastActiveAt: s.LastActiveAt,
	}
}

func (s *UserState) agent() *adk.ChatModelAgent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Agent
}

func (s *UserState) markStale() {
	s.mu.Lock()
	s.stale = true
	s.mu.Unlock()
}

func (s *UserState) isStale() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stale
}

// closeMCP releases MCP connections exactly once.
func (s *UserState) closeMCP() {
	s.mu.Lock()
	closer := s.mcpClose
	s.mcpClose = func() {}
	s.mu.Unlock()
	closer()
}

func (s *UserState) markImmediatePreempt(loop *adk.TurnLoop[*Request, *schema.Message]) {
	s.preempted.Store(loop, struct{}{})
}

func (s *UserState) isImmediatePreempt(loop *adk.TurnLoop[*Request, *schema.Message]) bool {
	_, ok := s.preempted.Load(loop)
	return ok
}

func (s *UserState) takeImmediatePreempt(loop *adk.TurnLoop[*Request, *schema.Message]) bool {
	_, ok := s.preempted.LoadAndDelete(loop)
	return ok
}

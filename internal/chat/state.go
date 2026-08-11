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

	mu           sync.RWMutex
	mcpClose     func()
	queuedInputs []string
	activeInputs []string
	closed       bool
	stale        bool
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

// agent returns the agent built for this state.
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

func (s *UserState) startTurnInputs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	inputs := s.queuedInputs
	s.queuedInputs = nil
	s.activeInputs = inputs
	return inputs
}

func (s *UserState) finishTurnInputs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	inputs := s.activeInputs
	s.activeInputs = nil
	return inputs
}

func (s *UserState) requeueInputs(inputs []string) {
	s.mu.Lock()
	s.queuedInputs = append(inputs, s.queuedInputs...)
	s.mu.Unlock()
}

// closeMCP marks the state closed and releases MCP connections exactly once.
func (s *UserState) closeMCP() {
	s.mu.Lock()
	closer := s.mcpClose
	s.mcpClose = func() {}
	s.closed = true
	s.mu.Unlock()
	closer()
}

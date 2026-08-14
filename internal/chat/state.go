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

	mu             sync.RWMutex
	mcpClose       func()
	queuedMessages []*schema.Message
	activeMessages []*schema.Message
	closed         bool
	stale          bool
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

func (s *UserState) startTurnMessages() []*schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	messages := s.queuedMessages
	s.queuedMessages = nil
	s.activeMessages = messages
	return messages
}

func (s *UserState) finishTurnMessages() []*schema.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	messages := s.activeMessages
	s.activeMessages = nil
	return messages
}

func (s *UserState) requeueMessages(messages []*schema.Message) {
	s.mu.Lock()
	s.queuedMessages = append(messages, s.queuedMessages...)
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

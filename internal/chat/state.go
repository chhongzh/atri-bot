package chat

import (
	"sync"

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

	mu           sync.RWMutex
	mcpClose     func()
	queuedInputs []string
	activeInputs []string
	closed       bool
	stale        bool
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
	if len(inputs) == 0 {
		return
	}
	s.mu.Lock()
	s.queuedInputs = append(inputs, s.queuedInputs...)
	s.mu.Unlock()
}

// closeMCP marks the state closed and releases MCP connections exactly once.
func (s *UserState) closeMCP() {
	s.mu.Lock()
	closer := s.mcpClose
	s.mcpClose = nil
	s.closed = true
	s.mu.Unlock()
	if closer != nil {
		closer()
	}
}

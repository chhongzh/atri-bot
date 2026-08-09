package chat

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
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

	mu        sync.RWMutex
	model     model.ChatModel
	mcpTools  []tool.BaseTool
	mcpCancel context.CancelFunc
	mcpClose  func()
	closed    bool
	stale     bool
}

// agent returns the current agent, which may have been rebuilt after MCP tools
// finished loading.
func (s *UserState) agent() *adk.ChatModelAgent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Agent
}

func (s *UserState) setMCPLoadCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		return
	}
	s.mcpCancel = cancel
	s.mu.Unlock()
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

func (s *UserState) attachMCP(agent *adk.ChatModelAgent, mcpTools []tool.BaseTool, closeFn func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.Agent = agent
	s.mcpTools = mcpTools
	s.mcpClose = closeFn
	return true
}

// closeMCP marks the state closed and releases MCP connections exactly once.
func (s *UserState) closeMCP() {
	s.mu.Lock()
	cancel := s.mcpCancel
	closer := s.mcpClose
	s.mcpCancel = nil
	s.mcpClose = nil
	s.closed = true
	s.mcpTools = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if closer != nil {
		closer()
	}
}

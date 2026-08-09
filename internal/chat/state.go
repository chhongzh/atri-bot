package chat

import (
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
}

package session

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
)

type flattenedToolHistory struct {
	toolCalls   int
	toolResults int
}

func (h flattenedToolHistory) changed() bool {
	return h.toolCalls > 0 || h.toolResults > 0
}

func flattenToolCallHistory(messages []*schema.Message) ([]*schema.Message, flattenedToolHistory) {
	flattened := make([]*schema.Message, 0, len(messages))
	var history flattenedToolHistory
	for _, message := range messages {
		if message == nil {
			continue
		}
		switch {
		case message.Role == schema.Assistant && len(message.ToolCalls) > 0:
			flattened = append(flattened, flattenAssistantToolCalls(message))
			history.toolCalls += len(message.ToolCalls)
		case message.Role == schema.Tool:
			flattened = append(flattened, flattenToolResult(message))
			history.toolResults++
		default:
			flattened = append(flattened, message)
		}
	}
	return flattened, history
}

func flattenAssistantToolCalls(message *schema.Message) *schema.Message {
	copy := *message
	copy.MultiContent = nil
	copy.UserInputMultiContent = nil
	copy.AssistantGenMultiContent = nil
	copy.ToolCalls = nil

	var content strings.Builder
	content.WriteString(assistantHistoryText(message))
	if content.Len() > 0 {
		content.WriteString("\n\n")
	}
	content.WriteString("<tool_calls_from_history>\n")
	for _, call := range message.ToolCalls {
		fmt.Fprintf(&content, "call_id: %q\ntool_name: %q\narguments: %s\n", call.ID, call.Function.Name, call.Function.Arguments)
	}
	content.WriteString("</tool_calls_from_history>")
	copy.Content = content.String()
	return &copy
}

func assistantHistoryText(message *schema.Message) string {
	if message.Content != "" {
		return message.Content
	}
	var text strings.Builder
	for _, part := range message.MultiContent {
		if part.Type == schema.ChatMessagePartTypeText {
			text.WriteString(part.Text)
		}
	}
	for _, part := range message.AssistantGenMultiContent {
		if part.Type == schema.ChatMessagePartTypeText {
			text.WriteString(part.Text)
		}
	}
	return text.String()
}

func flattenToolResult(message *schema.Message) *schema.Message {
	copy := *message
	copy.Role = schema.Assistant
	copy.Content = fmt.Sprintf(
		"<tool_result_from_history>\ncall_id: %q\ntool_name: %q\nresult: %s\n</tool_result_from_history>",
		message.ToolCallID,
		message.ToolName,
		toolResultText(message),
	)
	copy.MultiContent = nil
	copy.UserInputMultiContent = nil
	copy.AssistantGenMultiContent = nil
	copy.ToolCallID = ""
	copy.ToolName = ""
	return &copy
}

func toolResultText(message *schema.Message) string {
	if message.Content != "" {
		return message.Content
	}
	var text strings.Builder
	for _, part := range message.MultiContent {
		if part.Type == schema.ChatMessagePartTypeText {
			text.WriteString(part.Text)
		}
	}
	for _, part := range message.UserInputMultiContent {
		if part.Type == schema.ChatMessagePartTypeText {
			text.WriteString(part.Text)
		}
	}
	if text.Len() > 0 {
		return text.String()
	}
	if data, err := json.Marshal(message.UserInputMultiContent); err == nil && len(data) > 2 {
		return string(data)
	}
	if data, err := json.Marshal(message.MultiContent); err == nil && len(data) > 2 {
		return string(data)
	}
	return "[tool result contains no textual content]"
}

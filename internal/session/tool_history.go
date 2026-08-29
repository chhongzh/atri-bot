package session

import (
	"fmt"

	"github.com/cloudwego/eino/schema"
)

const interruptedToolResult = "[tool execution was interrupted before a result was recorded]"

type toolHistoryCompletion struct {
	generatedCallIDs int
	generatedResults int
	discardedResults int
}

func (c toolHistoryCompletion) changed() bool {
	return c.generatedCallIDs > 0 || c.generatedResults > 0 || c.discardedResults > 0
}

func completeToolCallHistory(messages []*schema.Message) ([]*schema.Message, toolHistoryCompletion) {
	completed := make([]*schema.Message, 0, len(messages))
	usedCallIDs := collectToolCallIDs(messages)
	var completion toolHistoryCompletion

	for messageIndex := 0; messageIndex < len(messages); {
		message := messages[messageIndex]
		if message == nil {
			messageIndex++
			continue
		}
		if message.Role == schema.Tool {
			completion.discardedResults++
			messageIndex++
			continue
		}
		if message.Role != schema.Assistant || len(message.ToolCalls) == 0 {
			completed = append(completed, message)
			messageIndex++
			continue
		}

		assistant := message
		originalCallIDs := make([]string, len(message.ToolCalls))
		seenCallIDs := make(map[string]struct{}, len(message.ToolCalls))
		for callIndex, call := range message.ToolCalls {
			originalCallIDs[callIndex] = call.ID
			if call.ID != "" {
				if _, duplicate := seenCallIDs[call.ID]; !duplicate {
					seenCallIDs[call.ID] = struct{}{}
					continue
				}
			}
			if assistant == message {
				copy := *message
				copy.ToolCalls = append([]schema.ToolCall(nil), message.ToolCalls...)
				assistant = &copy
			}
			call.ID = nextInterruptedToolCallID(messageIndex, callIndex, usedCallIDs)
			assistant.ToolCalls[callIndex] = call
			seenCallIDs[call.ID] = struct{}{}
			completion.generatedCallIDs++
		}

		completed = append(completed, assistant)
		messageIndex++
		callIndexes := make(map[string][]int, len(assistant.ToolCalls))
		for callIndex, callID := range originalCallIDs {
			callIndexes[callID] = append(callIndexes[callID], callIndex)
		}
		matchedByCallID := make(map[string]int, len(callIndexes))
		responses := make([]*schema.Message, len(assistant.ToolCalls))
		for messageIndex < len(messages) {
			result := messages[messageIndex]
			if result == nil {
				messageIndex++
				continue
			}
			if result.Role != schema.Tool {
				break
			}
			messageIndex++
			indexes := callIndexes[result.ToolCallID]
			matched := matchedByCallID[result.ToolCallID]
			if matched >= len(indexes) {
				completion.discardedResults++
				continue
			}
			callIndex := indexes[matched]
			matchedByCallID[result.ToolCallID] = matched + 1
			if result.ToolCallID != assistant.ToolCalls[callIndex].ID {
				copy := *result
				copy.ToolCallID = assistant.ToolCalls[callIndex].ID
				result = &copy
			}
			responses[callIndex] = result
		}
		for callIndex, call := range assistant.ToolCalls {
			if result := responses[callIndex]; result != nil {
				completed = append(completed, result)
				continue
			}
			completed = append(completed, schema.ToolMessage(
				interruptedToolResult,
				call.ID,
				schema.WithToolName(call.Function.Name),
			))
			completion.generatedResults++
		}
	}

	return completed, completion
}

func collectToolCallIDs(messages []*schema.Message) map[string]struct{} {
	used := make(map[string]struct{})
	for _, message := range messages {
		if message == nil {
			continue
		}
		for _, call := range message.ToolCalls {
			if call.ID != "" {
				used[call.ID] = struct{}{}
			}
		}
		if message.ToolCallID != "" {
			used[message.ToolCallID] = struct{}{}
		}
	}
	return used
}

func nextInterruptedToolCallID(messageIndex, callIndex int, used map[string]struct{}) string {
	base := fmt.Sprintf("interrupted_call_%d_%d", messageIndex, callIndex)
	callID := base
	for suffix := 2; ; suffix++ {
		if _, exists := used[callID]; !exists {
			used[callID] = struct{}{}
			return callID
		}
		callID = fmt.Sprintf("%s_%d", base, suffix)
	}
}

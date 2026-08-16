/*
 * Copyright 2026 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package msgops

import (
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// AssistantText returns generated assistant text from a message.
func AssistantText[M adk.MessageType](msg M) string {
	switch m := any(msg).(type) {
	case *schema.Message:
		if m == nil {
			return ""
		}
		return messageAssistantText(m)
	case *schema.AgenticMessage:
		if m == nil {
			return ""
		}
		var parts []string
		for _, block := range m.ContentBlocks {
			if block != nil && block.Type == schema.ContentBlockTypeAssistantGenText && block.AssistantGenText != nil {
				parts = append(parts, block.AssistantGenText.Text)
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

// AssistantDeltaText returns assistant text from one streaming chunk. It
// intentionally delegates to AssistantText because schema.Message and
// AgenticMessage currently expose streamed assistant deltas through the same
// text-bearing fields; keeping this helper separate makes streaming call sites
// explicit and leaves room for chunk-specific handling later.
func AssistantDeltaText[M adk.MessageType](msg M) string {
	return AssistantText(msg)
}

// ToolCalls extracts function tool calls from a message or streaming chunk.
func ToolCalls[M adk.MessageType](msg M) []ToolCall {
	switch m := any(msg).(type) {
	case *schema.Message:
		if m == nil {
			return nil
		}
		out := make([]ToolCall, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			out = append(out, ToolCall{
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Args:  tc.Function.Arguments,
				Index: idx,
			})
		}
		return out
	case *schema.AgenticMessage:
		if m == nil {
			return nil
		}
		var out []ToolCall
		for _, block := range m.ContentBlocks {
			if block == nil || block.Type != schema.ContentBlockTypeFunctionToolCall || block.FunctionToolCall == nil {
				continue
			}
			idx := 0
			if block.StreamingMeta != nil {
				idx = block.StreamingMeta.Index
			}
			out = append(out, ToolCall{
				ID:    block.FunctionToolCall.CallID,
				Name:  block.FunctionToolCall.Name,
				Args:  block.FunctionToolCall.Arguments,
				Index: idx,
			})
		}
		return out
	default:
		return nil
	}
}

func messageAssistantText(msg *schema.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Content != "" {
		return msg.Content
	}
	var parts []string
	for _, part := range msg.AssistantGenMultiContent {
		if part.Type == schema.ChatMessagePartTypeText && part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "")
}

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
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// ToolCall contains the common function-tool-call fields used by the examples.
type ToolCall struct {
	ID    string
	Name  string
	Args  string
	Index int
}

// NewAssistant constructs an assistant message with optional function tool calls.
func NewAssistant[M adk.MessageType](text string, calls []ToolCall) M {
	if KindOf[M]() == KindAgentic {
		blocks := make([]*schema.ContentBlock, 0, len(calls)+1)
		if text != "" {
			blocks = append(blocks, normalizeAgenticContentBlock(schema.NewContentBlock(&schema.AssistantGenText{Text: text})))
		}
		for _, call := range calls {
			blocks = append(blocks, normalizeAgenticContentBlock(schema.NewContentBlock(&schema.FunctionToolCall{
				CallID:    call.ID,
				Name:      call.Name,
				Arguments: call.Args,
			})))
		}
		return any(&schema.AgenticMessage{
			Role:          schema.AgenticRoleTypeAssistant,
			ContentBlocks: blocks,
		}).(M)
	}

	schemaCalls := make([]schema.ToolCall, 0, len(calls))
	for _, call := range calls {
		idx := call.Index
		schemaCalls = append(schemaCalls, schema.ToolCall{
			ID:       call.ID,
			Index:    &idx,
			Function: schema.FunctionCall{Name: call.Name, Arguments: call.Args},
		})
	}
	return any(schema.AssistantMessage(text, schemaCalls)).(M)
}

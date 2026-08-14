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
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// ConcatChunks merges streaming message chunks using the official Eino schema
// helpers. For AgenticMessage this preserves reasoning blocks and reasoning
// signatures emitted by the model convertors.
func ConcatChunks[M adk.MessageType](chunks []M) (M, error) {
	var zero M
	if len(chunks) == 0 {
		return zero, fmt.Errorf("no chunks to concat")
	}
	if len(chunks) == 1 {
		return chunks[0], nil
	}

	if KindOf[M]() == KindAgentic {
		msgs := make([]*schema.AgenticMessage, 0, len(chunks))
		for _, chunk := range chunks {
			msg, ok := any(chunk).(*schema.AgenticMessage)
			if !ok || msg == nil {
				return zero, fmt.Errorf("unexpected agentic chunk type %T", chunk)
			}
			msgs = append(msgs, msg)
		}
		merged, err := schema.ConcatAgenticMessages(msgs)
		if err != nil {
			return zero, err
		}
		return any(merged).(M), nil
	}

	msgs := make([]*schema.Message, 0, len(chunks))
	for _, chunk := range chunks {
		msg, ok := any(chunk).(*schema.Message)
		if !ok || msg == nil {
			return zero, fmt.Errorf("unexpected message chunk type %T", chunk)
		}
		msgs = append(msgs, msg)
	}
	merged, err := schema.ConcatMessages(msgs)
	if err != nil {
		return zero, err
	}
	return any(merged).(M), nil
}

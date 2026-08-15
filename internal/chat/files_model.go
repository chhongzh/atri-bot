package chat

import (
	"context"
	"encoding/json"

	filesmanager "github.com/chhongzh/atri-bot/internal/files"
	openai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const fileRefsKey = "atri_file_refs"

type filesModel struct {
	inner       model.ToolCallingChatModel
	files       *filesmanager.Manager
	userID      int64
	characterID string
	revision    uint64
}

func (m *filesModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return m.inner.Generate(ctx, in, append(opts, openai.WithRequestPayloadModifier(m.modify))...)
}
func (m *filesModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.inner.Stream(ctx, in, append(opts, openai.WithRequestPayloadModifier(m.modify))...)
}
func (m *filesModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	inner, err := m.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	copy := *m
	copy.inner = inner
	return &copy, nil
}
func (m *filesModel) modify(ctx context.Context, messages []*schema.Message, raw []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	payloadMessages, _ := payload["messages"].([]any)
	for index, message := range messages {
		if message.Role != schema.User || message.Extra == nil {
			continue
		}
		rawRefs := fileRefs(message.Extra[fileRefsKey])
		if len(rawRefs) == 0 {
			continue
		}
		ids, err := m.files.IDs(ctx, m.userID, m.characterID, m.revision, rawRefs)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 || index >= len(payloadMessages) {
			continue
		}
		payloadMessage, ok := payloadMessages[index].(map[string]any)
		if !ok {
			continue
		}
		parts := []any{map[string]any{"type": "text", "text": message.Content}}
		for _, id := range ids {
			parts = append(parts, map[string]any{"type": "file", "file": map[string]any{"file_id": id}})
		}
		payloadMessage["content"] = parts
	}
	return json.Marshal(payload)
}

func fileRefs(value any) []string {
	switch refs := value.(type) {
	case []string:
		return refs
	case []any:
		result := make([]string, 0, len(refs))
		for _, ref := range refs {
			if id, ok := ref.(string); ok {
				result = append(result, id)
			}
		}
		return result
	default:
		return nil
	}
}

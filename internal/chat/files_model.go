package chat

import (
	"context"

	filesmanager "github.com/chhongzh/atri-bot/internal/files"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const fileRefsKey = "atri_file_refs"

type filesModel struct {
	inner model.ToolCallingChatModel
	files *filesmanager.Manager
}

func (m *filesModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	messages, err := m.load(ctx, in)
	if err != nil {
		return nil, err
	}
	return m.inner.Generate(ctx, messages, opts...)
}

func (m *filesModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	messages, err := m.load(ctx, in)
	if err != nil {
		return nil, err
	}
	return m.inner.Stream(ctx, messages, opts...)
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

func (m *filesModel) load(ctx context.Context, messages []*schema.Message) ([]*schema.Message, error) {
	loaded := append([]*schema.Message(nil), messages...)
	for index, message := range messages {
		if message.Role != schema.User || message.Extra == nil {
			continue
		}
		attachments, err := m.files.Load(ctx, fileRefs(message.Extra[fileRefsKey]))
		if err != nil {
			return nil, err
		}
		if len(attachments) == 0 {
			continue
		}
		copy := *message
		copy.Content = ""
		copy.UserInputMultiContent = []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: message.Content}}
		for _, attachment := range attachments {
			copy.UserInputMultiContent = append(copy.UserInputMultiContent, inputPart(attachment))
		}
		loaded[index] = &copy
	}
	return loaded, nil
}

func inputPart(attachment filesmanager.Attachment) schema.MessageInputPart {
	common := schema.MessagePartCommon{Base64Data: &attachment.Base64, MIMEType: attachment.MIMEType}
	switch attachment.Kind {
	case "audio":
		return schema.MessageInputPart{Type: schema.ChatMessagePartTypeAudioURL, Audio: &schema.MessageInputAudio{MessagePartCommon: common}}
	case "video":
		return schema.MessageInputPart{Type: schema.ChatMessagePartTypeVideoURL, Video: &schema.MessageInputVideo{MessagePartCommon: common}}
	default:
		return schema.MessageInputPart{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{MessagePartCommon: common}}
	}
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

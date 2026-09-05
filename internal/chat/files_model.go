package chat

import (
	"context"
	"mime"
	"strings"

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
		copy.MultiContent = nil
		copy.UserInputMultiContent = nil
		if containsAudio(attachments) {
			copy.MultiContent = []schema.ChatMessagePart{{Type: schema.ChatMessagePartTypeText, Text: message.Content}}
			for _, attachment := range attachments {
				copy.MultiContent = append(copy.MultiContent, legacyInputPart(attachment))
			}
		} else {
			copy.UserInputMultiContent = []schema.MessageInputPart{{Type: schema.ChatMessagePartTypeText, Text: message.Content}}
			for _, attachment := range attachments {
				copy.UserInputMultiContent = append(copy.UserInputMultiContent, inputPart(attachment))
			}
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

func containsAudio(attachments []filesmanager.Attachment) bool {
	for _, attachment := range attachments {
		if attachment.Kind == "audio" {
			return true
		}
	}
	return false
}

func legacyInputPart(attachment filesmanager.Attachment) schema.ChatMessagePart {
	switch attachment.Kind {
	case "audio":
		return schema.ChatMessagePart{
			Type: schema.ChatMessagePartTypeAudioURL,
			AudioURL: &schema.ChatMessageAudioURL{
				URL:      inlineDataURL(attachment.MIMEType, attachment.Base64),
				MIMEType: audioFormat(attachment.MIMEType),
			},
		}
	case "video":
		return schema.ChatMessagePart{
			Type:     schema.ChatMessagePartTypeVideoURL,
			VideoURL: &schema.ChatMessageVideoURL{URL: inlineDataURL(attachment.MIMEType, attachment.Base64)},
		}
	default:
		return schema.ChatMessagePart{
			Type:     schema.ChatMessagePartTypeImageURL,
			ImageURL: &schema.ChatMessageImageURL{URL: inlineDataURL(attachment.MIMEType, attachment.Base64)},
		}
	}
}

func audioFormat(mimeType string) string {
	mediaType, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])
	}
	switch strings.ToLower(mediaType) {
	case "audio/wav", "audio/vnd.wav", "audio/vnd.wave", "audio/wave", "audio/x-pn-wav", "audio/x-wav":
		return "wav"
	case "audio/mpeg", "audio/mp3", "audio/mpeg3", "audio/x-mpeg-3":
		return "mp3"
	case "audio/ogg", "audio/opus":
		return "opus"
	case "audio/flac", "audio/x-flac":
		return "flac"
	case "audio/aac":
		return "aac"
	default:
		if strings.HasPrefix(strings.ToLower(mediaType), "audio/") {
			format := strings.TrimPrefix(strings.ToLower(mediaType), "audio/")
			format = strings.TrimPrefix(format, "x-")
			if format != "" {
				return format
			}
		}
		return "wav"
	}
}

func inlineDataURL(mimeType, data string) string {
	mimeType = strings.TrimSpace(mimeType)
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return "data:" + mimeType + ";base64," + data
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

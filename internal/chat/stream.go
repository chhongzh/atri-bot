package chat

import (
	"errors"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func consumeMessageVariant(
	variant *adk.MessageVariant,
	handleChunk func(*schema.Message) error,
) (*schema.Message, error) {
	if !variant.IsStreaming {
		message, err := variant.GetMessage()
		if err != nil || message == nil {
			return message, err
		}
		if err = handleChunk(message); err != nil {
			return nil, err
		}
		return message, nil
	}
	if variant.MessageStream == nil {
		return nil, errors.New("streaming message variant has no stream")
	}

	stream := schema.StreamReaderWithConvert(variant.MessageStream, func(chunk *schema.Message) (*schema.Message, error) {
		if chunk == nil {
			return nil, schema.ErrNoValue
		}
		if err := handleChunk(chunk); err != nil {
			return nil, err
		}
		return chunk, nil
	})

	return schema.ConcatMessageStream(stream)
}

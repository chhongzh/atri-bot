// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package chat

import (
	"errors"
	"io"

	"github.com/chhongzh/atri-bot/internal/errs"
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
		return nil, errs.ErrStreamingVariantNoStream
	}
	defer variant.MessageStream.Close()

	var chunks []*schema.Message
	for {
		chunk, recvErr := variant.MessageStream.Recv()
		var handleErr error
		if chunk != nil {
			chunks = append(chunks, chunk)
			handleErr = handleChunk(chunk)
		}

		if errors.Is(recvErr, io.EOF) {
			return concatMessageChunks(chunks, handleErr)
		}
		if recvErr != nil {
			message, concatErr := concatMessageChunks(chunks, nil)
			if concatErr != nil {
				return nil, concatErr
			}
			return message, recvErr
		}
		if handleErr != nil {
			message, concatErr := concatMessageChunks(chunks, nil)
			if concatErr != nil {
				return nil, concatErr
			}
			return message, handleErr
		}
	}
}

func concatMessageChunks(chunks []*schema.Message, streamErr error) (*schema.Message, error) {
	if len(chunks) == 0 {
		return nil, streamErr
	}
	message, err := schema.ConcatMessages(chunks)
	if err != nil {
		return nil, err
	}
	return message, streamErr
}

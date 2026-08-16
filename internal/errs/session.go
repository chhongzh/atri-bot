// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	"errors"
	"fmt"
)

var (
	ErrCompressionEmptyHistory          = errors.New("compression model returned empty history")
	ErrCompressionTemplateNoMessage     = errors.New("compression template returned no message")
	ErrCompressionAgentNoMessage        = errors.New("compression agent returned no assistant message")
	ErrMessageMetadataTemplateNoMessage = errors.New("message metadata template returned no message")
)

// SessionRoundNotFound reports a missing session round.
func SessionRoundNotFound(roundID uint) error {
	return fmt.Errorf("session round %d not found", roundID)
}

// SessionMetadataNotSystem reports a metadata record that is not a system message.
func SessionMetadataNotSystem(recordID uint) error {
	return fmt.Errorf("session metadata message %d is not a system message", recordID)
}

// SessionUserNoMetadata reports a user message without a preceding metadata record.
func SessionUserNoMetadata(recordID uint) error {
	return fmt.Errorf("session user message %d has no preceding metadata message", recordID)
}

// SessionRoundEmpty reports a session round with no messages.
func SessionRoundEmpty(roundID uint) error {
	return fmt.Errorf("session round %d contains no messages", roundID)
}

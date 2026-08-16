// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	stderrors "errors"
	"strings"

	"github.com/pkg/errors"
)

var (
	ErrNoCharacters                 = stderrors.New("no characters are available")
	ErrAIConfigIncomplete           = stderrors.New("user AI config is incomplete")
	ErrTurnPreempted                = stderrors.New("turn preempted by a newer message")
	ErrStateStopped                 = stderrors.New("user turn loop has stopped")
	ErrInterruptedOutputPersistence = stderrors.New("failed to persist interrupted chat output")
	ErrChatRoundChanged             = stderrors.New("chat round changed while appending an interrupted user message")
	ErrTurnMixedSessionRounds       = stderrors.New("turn contains requests from different session rounds")
	ErrStreamingVariantNoStream     = stderrors.New("streaming message variant has no stream")
	ErrUnknownError                 = stderrors.New("unknown error")
)

// AIConfigIncomplete reports which AI connection fields a user has not set.
func AIConfigIncomplete(missing []string) error {
	return errors.Wrapf(ErrAIConfigIncomplete, "missing %s", strings.Join(missing, ", "))
}

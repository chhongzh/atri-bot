// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	"errors"
	"fmt"
)

var ErrNoChunksToConcat = errors.New("no chunks to concat")

// UnexpectedAgenticChunkType reports an unknown agentic message chunk type.
func UnexpectedAgenticChunkType(chunk any) error {
	return fmt.Errorf("unexpected agentic chunk type %T", chunk)
}

// UnexpectedMessageChunkType reports an unknown message chunk type.
func UnexpectedMessageChunkType(chunk any) error {
	return fmt.Errorf("unexpected message chunk type %T", chunk)
}

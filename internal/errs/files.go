// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	"errors"
	"fmt"
)

var ErrInvalidImageDimensions = errors.New("invalid image dimensions")

// FileTooLarge reports a media file exceeding the upload limit.
func FileTooLarge(maxMB int64) error {
	return fmt.Errorf("media file cannot exceed %d MB", maxMB)
}

// MediaPoolFull reports the local media pool reaching its size limit.
func MediaPoolFull(maxMB int64) error {
	return fmt.Errorf("local media pool reached its %d MB limit", maxMB)
}

// ImageMaxEdgeExceeded reports an image longer than the allowed max edge.
func ImageMaxEdgeExceeded(maxEdge int) error {
	return fmt.Errorf("image longest edge cannot exceed %d pixels", maxEdge)
}

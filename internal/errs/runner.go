// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidUserID   = errors.New("user id must be a positive integer")
	ErrInvalidPage     = errors.New("page must be a positive integer")
	ErrInvalidRounds   = errors.New("rounds must be a positive integer")
	ErrInvalidMCPLimit = errors.New("mcp limit must be a non-negative integer (0 restores the default)")
	ErrEmptyToolName   = errors.New("tool name is required")
)

// CommandUsage reports invalid command arguments with the expected usage.
func CommandUsage(usage string) error {
	return fmt.Errorf("usage: %s", usage)
}

// UnknownAIItem reports an unknown AI configuration item.
func UnknownAIItem(action string) error {
	return fmt.Errorf("unknown AI configuration item %q", action)
}

// InvalidImageSize reports an out-of-range image-size value.
func InvalidImageSize(maxEdge int) error {
	return fmt.Errorf("image-size must be an integer between 1 and %d", maxEdge)
}

// UnknownAdminAction reports an unknown administrator action.
func UnknownAdminAction(action string) error {
	return fmt.Errorf("unknown administrator action %q", action)
}

// UnknownMCPAction reports an unknown mcp operation.
func UnknownMCPAction(action string) error {
	return fmt.Errorf("unknown mcp operation %q", action)
}

// UnknownProviderAction reports an unknown provider operation.
func UnknownProviderAction(action string) error {
	return fmt.Errorf("unknown provider operation %q", action)
}

// UnknownToolPermAction reports an unknown tool permission operation.
func UnknownToolPermAction(action string) error {
	return fmt.Errorf("unknown tool permission operation %q", action)
}

// PageOutOfRange reports a requested page beyond the available pages.
func PageOutOfRange(page, pages int) error {
	return fmt.Errorf("page %d is out of range (total %d pages)", page, pages)
}

// AdminTemplateMessageCount reports an administrator notification template
// that returned an unexpected number of messages.
func AdminTemplateMessageCount(count int) error {
	return fmt.Errorf("administrator notification template returned %d messages", count)
}

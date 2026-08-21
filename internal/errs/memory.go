package errs

import (
	"errors"
	"fmt"
)

var (
	ErrMemoryContentRequired   = errors.New("memory content is required")
	ErrMemoryContentTooLong    = errors.New("memory content is too long")
	ErrMemoryIDRequired        = errors.New("memory id is required")
	ErrMemoryLimitReached      = errors.New("memory limit reached")
	ErrMemoryNotFound          = errors.New("memory not found")
	ErrMemoryTemplateNoMessage = errors.New("memory template returned no message")
)

// MemoryNotFound reports a memory that does not belong to the current user.
func MemoryNotFound(id uint) error {
	return fmt.Errorf("memory %d: %w", id, ErrMemoryNotFound)
}

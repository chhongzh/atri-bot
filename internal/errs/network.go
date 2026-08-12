package errs

import "errors"

var (
	ErrInternalHostBlocked = errors.New("mcp url points to an internal or private network address")
	ErrInvalidScheme       = errors.New("mcp url scheme must be http or https")
)

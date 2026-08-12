package errs

import (
	"errors"

	"github.com/chhongzh/atri-bot/internal/security"
)

var (
	ErrInternalHostBlocked = security.ErrPrivateAddressBlocked
	ErrInvalidScheme       = errors.New("mcp url scheme must be http or https")
)

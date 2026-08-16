// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	"errors"
	"fmt"
)

var (
	ErrUserNotFound        = errors.New("user not found")
	ErrPermissionDenied    = errors.New("permission denied")
	ErrLastAdmin           = errors.New("the last active administrator cannot be removed")
	ErrSelfDemotion        = errors.New("administrators cannot demote themselves")
	ErrInvalidRole         = errors.New("invalid role")
	ErrInvalidAIMaxRounds  = errors.New("AI max rounds must be positive")
	ErrInvalidMCPMaxTools  = errors.New("MCP max tools cannot be negative")
	ErrPageNotPositive     = errors.New("page must be positive")
	ErrPageSizeNotPositive = errors.New("page size must be positive")
)

// InvalidRole reports an unsupported role name.
func InvalidRole(role any) error {
	return fmt.Errorf("invalid role %q", role)
}

// InvalidAIImageMaxEdge reports an out-of-range image max edge value.
func InvalidAIImageMaxEdge(maxEdge int) error {
	return fmt.Errorf("AI image max edge must be between 1 and %d", maxEdge)
}

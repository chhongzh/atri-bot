// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import "errors"

var (
	ErrInternalHostBlocked = ErrPrivateAddressBlocked
	ErrInvalidScheme       = errors.New("mcp url scheme must be http or https")
)

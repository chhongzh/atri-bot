// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import "errors"

var (
	ErrProviderNotFound = errors.New("mcp provider not found")
	ErrProviderExists   = errors.New("mcp provider already exists")
	ErrProviderLimit    = errors.New("mcp provider limit reached")
	ErrInvalidJSON      = errors.New("invalid mcp provider json field")
	ErrPathNotFound     = errors.New("mcp provider path not found")
	ErrPathForbidden    = errors.New("mcp provider path is read-only")
	ErrLoaderClosed     = errors.New("mcp loader is closed")
)

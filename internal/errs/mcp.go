// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	stderrors "errors"
	"fmt"

	"github.com/pkg/errors"
)

var (
	ErrProviderNotFound         = stderrors.New("mcp provider not found")
	ErrProviderExists           = stderrors.New("mcp provider already exists")
	ErrProviderLimit            = stderrors.New("mcp provider limit reached")
	ErrInvalidJSON              = stderrors.New("invalid mcp provider json field")
	ErrPathNotFound             = stderrors.New("mcp provider path not found")
	ErrPathForbidden            = stderrors.New("mcp provider path is read-only")
	ErrLoaderClosed             = stderrors.New("mcp loader is closed")
	ErrMCPURLEmpty              = stderrors.New("mcp url is required")
	ErrMCPURLHostRequired       = stderrors.New("mcp url host is required")
	ErrMCPURLUserInfoForbidden  = stderrors.New("mcp url must not contain user info")
	ErrMCPURLFragmentForbidden  = stderrors.New("mcp url must not contain a fragment")
	ErrMCPProviderValueRequired = stderrors.New("mcp provider value is required")
	ErrNameRequired             = stderrors.New("name is required")
)

// MCPProviderLimit reports that a user has reached their provider limit.
func MCPProviderLimit(maxTools int) error {
	return errors.Wrapf(ErrProviderLimit, "at most %d", maxTools)
}

// MCPProviderExists reports a duplicate provider name.
func MCPProviderExists(name string) error {
	return errors.Wrapf(ErrProviderExists, "%s", name)
}

// MCPPathNotFound reports a missing provider JSON path.
func MCPPathNotFound(name, path string) error {
	return errors.Wrapf(ErrPathNotFound, "%s.%s", name, path)
}

// MCPPathForbidden reports an attempt to modify a read-only provider path.
func MCPPathForbidden(top string) error {
	return errors.Wrapf(ErrPathForbidden, "%s", top)
}

// MCPProviderNameTooLong reports an overlong provider name.
func MCPProviderNameTooLong(limit int) error {
	return fmt.Errorf("mcp provider name exceeds %d bytes", limit)
}

// MCPURLTooLong reports an overlong provider url.
func MCPURLTooLong(limit int) error {
	return fmt.Errorf("mcp url exceeds %d bytes", limit)
}

// MCPInvalidJSONTooLarge reports a JSON field exceeding the byte limit.
func MCPInvalidJSONTooLarge(field string, limit int) error {
	return errors.Wrapf(ErrInvalidJSON, "%s exceeds %d bytes", field, limit)
}

// MCPInvalidJSONNotObject reports a JSON field that is not an object.
func MCPInvalidJSONNotObject(field string) error {
	return errors.Wrapf(ErrInvalidJSON, "%s must be a JSON object", field)
}

// MCPInvalidJSONNotValid reports a JSON field that is not valid JSON.
func MCPInvalidJSONNotValid(field string) error {
	return errors.Wrapf(ErrInvalidJSON, "%s must be valid JSON", field)
}

// MCPInvalidJSONNotStringMap reports a JSON field whose values are not strings.
func MCPInvalidJSONNotStringMap(field string) error {
	return errors.Wrapf(ErrInvalidJSON, "%s values must be strings", field)
}

// MCPInvalidJSONInvalidHeader reports a JSON field containing an invalid HTTP header.
func MCPInvalidJSONInvalidHeader(field string) error {
	return errors.Wrapf(ErrInvalidJSON, "%s contains an invalid HTTP header", field)
}

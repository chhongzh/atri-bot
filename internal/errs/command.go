// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	stderrors "errors"

	"github.com/pkg/errors"
)

var (
	ErrCommandProviderExists    = stderrors.New("command provider already exists")
	ErrCommandProviderNotFound  = stderrors.New("command provider not found")
	ErrCommandAlreadyRegistered = stderrors.New("command already registered")
)

// CommandProviderExists reports a duplicate command provider registration.
func CommandProviderExists(name string) error {
	return errors.Wrapf(ErrCommandProviderExists, "%q", name)
}

// CommandProviderNotFound reports an unknown command provider.
func CommandProviderNotFound(name string) error {
	return errors.Wrapf(ErrCommandProviderNotFound, "%q", name)
}

// CommandAlreadyRegistered reports a duplicate command registration.
func CommandAlreadyRegistered(command string) error {
	return errors.Wrapf(ErrCommandAlreadyRegistered, "%q", command)
}

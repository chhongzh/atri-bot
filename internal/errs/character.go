// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	stderrors "errors"

	"github.com/pkg/errors"
)

var (
	ErrCharacterNotFound        = stderrors.New("character not found")
	ErrCharacterUnavailable     = stderrors.New("selected character is unavailable")
	ErrPromptTemplateNoMessage  = stderrors.New("prompt template returned no message")
	ErrInvalidProviderID        = stderrors.New("invalid provider id")
	ErrRemoteOnlyUpdate         = stderrors.New("only remote providers can be updated")
	ErrBuiltInProviderProtected = stderrors.New("built-in provider cannot be removed")
	ErrRemoteURLEmpty           = stderrors.New("remote url is empty")
	ErrUnsupportedProviderKind  = stderrors.New("unsupported provider kind")
)

// CharacterNotFound reports a missing character by id.
func CharacterNotFound(id string) error {
	return errors.Wrapf(ErrCharacterNotFound, "%q", id)
}

// CharacterUnavailable reports that a selected character no longer exists.
func CharacterUnavailable(id string) error {
	return errors.Wrapf(ErrCharacterUnavailable, "%q", id)
}

// PromptTemplateNoMessage reports a prompt template that returned no message.
func PromptTemplateNoMessage(name string) error {
	return errors.Wrapf(ErrPromptTemplateNoMessage, "%s", name)
}

// UnsupportedProviderKind reports an unknown character provider kind.
func UnsupportedProviderKind(kind string) error {
	return errors.Wrapf(ErrUnsupportedProviderKind, "%q", kind)
}

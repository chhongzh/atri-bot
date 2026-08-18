// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	stderrors "errors"
	"fmt"

	"github.com/pkg/errors"
)

var (
	ErrRunningStateMissing     = stderrors.New("tool running state is missing")
	ErrTelebotContextMissing   = stderrors.New("telegram context is missing")
	ErrToolNotFound            = stderrors.New("tool not found")
	ErrConfigPathNotFound      = stderrors.New("tool config path not found")
	ErrToolConfigValueRequired = stderrors.New("tool config value is required")
	ErrToolNameRequired        = stderrors.New("tool_name is required")
	ErrImageURLRequired        = stderrors.New("image URL is required")
	ErrImageURLInvalid         = stderrors.New("image URL must be an HTTP or HTTPS URL")
)

// ToolNotFound reports an unknown tool by name.
func ToolNotFound(name string) error {
	return errors.Wrapf(ErrToolNotFound, "%s", name)
}

// ToolConfigPathNotFound reports a missing tool config path.
func ToolConfigPathNotFound(name, path string) error {
	return errors.Wrapf(ErrConfigPathNotFound, "%s.%s", name, path)
}

// ToolConfigNotStruct reports a config type that is not a struct.
func ToolConfigNotStruct(configType string) error {
	return fmt.Errorf("tool config %s must be a struct", configType)
}

// ToolUnexpectedConfigType reports an unexpected config type at runtime.
func ToolUnexpectedConfigType(name string) error {
	return fmt.Errorf("unexpected config type for tool %s", name)
}

// ToolAlreadyRegistered reports a duplicate tool registration.
func ToolAlreadyRegistered(name string) error {
	return fmt.Errorf("tool %q already registered", name)
}

// ToolAlreadyBuiltin reports a tool already registered as builtin.
func ToolAlreadyBuiltin(name string) error {
	return fmt.Errorf("tool %q already registered as builtin", name)
}

// ToolConflictsWithPermission reports a tool name conflicting with a permission capability.
func ToolConflictsWithPermission(name string) error {
	return fmt.Errorf("tool %q conflicts with a permission-only capability", name)
}

// BuiltinToolNameMismatch reports a builtin tool whose declared name differs.
func BuiltinToolNameMismatch(name, declaredName string) error {
	return fmt.Errorf("builtin tool name mismatch: registered %q, declared %q", name, declaredName)
}

// ToolAlreadyConfigurable reports a tool already registered as configurable.
func ToolAlreadyConfigurable(name string) error {
	return fmt.Errorf("tool %q already registered as configurable", name)
}

// BuiltinToolAlreadyRegistered reports a duplicate builtin tool registration.
func BuiltinToolAlreadyRegistered(name string) error {
	return fmt.Errorf("builtin tool %q already registered", name)
}

// BuiltinToolConflictsWithPermission reports a builtin tool name conflicting with a permission capability.
func BuiltinToolConflictsWithPermission(name string) error {
	return fmt.Errorf("builtin tool %q conflicts with a permission-only capability", name)
}

// ToolPermissionNotRegistered reports an unknown tool permission.
func ToolPermissionNotRegistered(permission string) error {
	return fmt.Errorf("tool permission %q is not registered", permission)
}

// ToolPermissionAlreadyRegistered reports a duplicate tool permission registration.
func ToolPermissionAlreadyRegistered(name string) error {
	return fmt.Errorf("tool permission %q already registered", name)
}

// ToolPermissionConflictsWithTool reports a tool permission name conflicting with a tool.
func ToolPermissionConflictsWithTool(name string) error {
	return fmt.Errorf("tool permission %q conflicts with a tool", name)
}

// ToolInvalidConfigMalformed reports a tool config that is malformed JSON.
func ToolInvalidConfigMalformed(name string) error {
	return fmt.Errorf("invalid config for %s: malformed JSON", name)
}

// ToolInvalidConfigTrailing reports a tool config with trailing JSON data.
func ToolInvalidConfigTrailing(name string) error {
	return fmt.Errorf("invalid config for %s: trailing JSON data", name)
}

// ToolNoPermissionMapping reports a tool without a permission mapping.
func ToolNoPermissionMapping(name string) error {
	return fmt.Errorf("tool %q has no permission mapping", name)
}

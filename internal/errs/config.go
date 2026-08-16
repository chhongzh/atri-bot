// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	stderrors "errors"
	"fmt"

	"github.com/pkg/errors"
)

var (
	ErrConfigNotFound                 = stderrors.New("configuration not found")
	ErrConfigKeyRequired              = stderrors.New("configuration key is required")
	ErrConfigBotTokenMissing          = stderrors.New("required configuration telegram.bot_token is missing")
	ErrConfigMaxRoundsInvalid         = stderrors.New("configuration default.max_rounds must be positive")
	ErrConfigMCPMaxToolsInvalid       = stderrors.New("configuration default.mcp_max_tools must be positive")
	ErrConfigFilesMaxStorageInvalid   = stderrors.New("configuration files.max_storage_mb must be positive")
	ErrConfigFilesCleanupAfterInvalid = stderrors.New("configuration files.cleanup_after must be a positive duration")
	ErrConfigMySQLDSNRequired         = stderrors.New("configuration database.dsn is required for database.type=mysql")
)

// ConfigNotFound reports a missing configuration value by key.
func ConfigNotFound(key string) error {
	return errors.Wrapf(ErrConfigNotFound, "%s", key)
}

// ConfigImageMaxEdgeInvalid reports an out-of-range default image max edge.
func ConfigImageMaxEdgeInvalid(maxEdge int) error {
	return fmt.Errorf("configuration default.image_max_edge must be between 1 and %d", maxEdge)
}

// ConfigDatabaseTypeUnsupported reports an unknown database type.
func ConfigDatabaseTypeUnsupported(databaseType string) error {
	return fmt.Errorf("unsupported database.type %q (supported: sqlite, mysql)", databaseType)
}

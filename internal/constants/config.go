// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

// Package constants contains application-wide configuration defaults and limits.
package constants

import "time"

const (
	DefaultMaxRounds               = 36
	DefaultImageMaxEdge            = 1024
	MaxImageMaxEdge                = 2048
	DefaultMCPMaxTools             = 128
	DefaultSQLitePath              = "atri-bot.db"
	DefaultFilesMaxStorageMB       = 1024
	DefaultFilesCleanupAfter       = "7d"
	DefaultFilesStorageBytes       = int64(1 << 30)
	DefaultFilesCleanupAge         = 7 * 24 * time.Hour
	MaxUploadFileBytes       int64 = 20 << 20

	DefaultCharacterRepositoryURL = "https://github.com/mihari-bot/chardef"
	DefaultCharacterBranch        = "main"
	DefaultChatStateTTL           = 30 * time.Minute
	DefaultAIModelTimeout         = 2 * time.Minute
)

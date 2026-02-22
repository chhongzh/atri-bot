//go:build android

package main

import "go.uber.org/zap"

func exitWithError(logger *zap.Logger, msg string, err error) {
	printFriendlyError(logger, err)
	logger.Error(msg, zap.Error(err))
}

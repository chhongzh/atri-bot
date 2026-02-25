//go:build android

package main

import (
	"go.uber.org/zap"
)

var errBuf chan error

func exitWithError(logger *zap.Logger, msg string, err error) {
	logger.Error(msg, zap.Error(err))
	errBuf <- err
}

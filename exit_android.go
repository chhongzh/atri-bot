//go:build android

package main

import (
	"go.uber.org/zap"
)

var errBuf = make(chan error, 64)

func exitWithError(logger *zap.Logger, msg string, err error) {
	logger.Error(msg, zap.Error(err))
	errBuf <- err
}

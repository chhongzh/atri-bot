package main

import (
	"fmt"
	"os"
	"runtime"

	"go.uber.org/zap"
)

func exitWithError(logger *zap.Logger, msg string, err error) {
	printFriendlyError(logger, err)
	if runtime.GOOS == "windows" {
		logger.Error(msg, zap.Error(err))
		fmt.Println("按回车键退出...")
		var s string
		fmt.Scanln(&s)
		os.Exit(1)
	}
	logger.Fatal(msg, zap.Error(err))
}

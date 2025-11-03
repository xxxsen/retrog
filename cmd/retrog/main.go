package main

import (
	"context"
	"os"

	"retrog/internal/cli"

	"github.com/xxxsen/common/logger"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

func main() {
	logger.Init("", "debug", 0, 0, 0, true)
	if err := cli.Execute(); err != nil {
		logutil.GetLogger(context.Background()).Fatal("exec cli failed", zap.Error(err))
		os.Exit(1)
	}
}

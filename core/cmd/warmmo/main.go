package main

import (
	"log/slog"
	"os"

	"warmmo/core/internal/infra"
)

var (
	version       = "dev"
	allowedOrigin = ""
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := infra.Run(logger, version, allowedOrigin); err != nil {
		logger.Error("runtime stopped", "error", err)
		os.Exit(1)
	}
}

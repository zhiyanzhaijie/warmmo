package main

import (
	"log/slog"
	"os"

	"warmmo/core/internal/infra"
)

const version = "0.1.0"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := infra.Run(logger, version); err != nil {
		logger.Error("runtime stopped", "error", err)
		os.Exit(1)
	}
}

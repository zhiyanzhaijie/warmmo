package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"warmnote/core/internal/controller"
	"warmnote/core/internal/repository"
	"warmnote/core/internal/service"
	"warmnote/core/internal/webserver"
)

const version = "0.1.0"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("runtime stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtimeService := service.NewRuntimeService(version)
	runtimeController := controller.NewRuntimeController(runtimeService, logger)
	dataDirectory, err := resolveDataDirectory()
	if err != nil {
		return err
	}
	providerRepository, err := repository.NewProviderRepository(dataDirectory)
	if err != nil {
		return err
	}
	defer providerRepository.Close()
	logger.Info("Warmnote data initialized", "database", providerRepository.DatabasePath())
	providerService := service.NewProviderService(providerRepository)
	providerController := controller.NewProviderController(providerService, logger)
	server := &http.Server{
		Addr:              "127.0.0.1:8787",
		Handler:           webserver.NewRouter(runtimeController, providerController),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("Warmnote Core is listening", "address", "http://"+server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func resolveDataDirectory() (string, error) {
	if configured := os.Getenv("WARMNOTE_DATA_DIR"); configured != "" {
		return filepath.Abs(configured)
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if fileExists(filepath.Join(workingDirectory, "core", "go.mod")) {
		return filepath.Join(workingDirectory, ".data"), nil
	}
	if fileExists(filepath.Join(workingDirectory, "go.mod")) && filepath.Base(workingDirectory) == "core" {
		return filepath.Join(filepath.Dir(workingDirectory), ".data"), nil
	}

	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDirectory, "warmnote"), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

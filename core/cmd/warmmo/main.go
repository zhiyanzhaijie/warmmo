package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	adk "warmmo/core/internal/agent/adapter/adk"
	agentcore "warmmo/core/internal/agent/core"
	agenttools "warmmo/core/internal/agent/tools"
	agent "warmmo/core/internal/agent/writing"
	"warmmo/core/internal/ai"
	"warmmo/core/internal/ai/embedding"
	aiprovider "warmmo/core/internal/ai/provider"
	"warmmo/core/internal/application"
	"warmmo/core/internal/httpapi"
	"warmmo/core/internal/storage"
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

	runtimeService := application.NewRuntimeService(version)
	runtimeController := httpapi.NewRuntimeController(runtimeService, logger)
	dataDirectory, err := resolveDataDirectory()
	if err != nil {
		return err
	}
	providerRepository, err := storage.NewProviderRepository(dataDirectory)
	if err != nil {
		return err
	}
	defer providerRepository.Close()
	logger.Info("Warmmo data initialized", "database", providerRepository.DatabasePath())
	providerService := application.NewProviderService(providerRepository, aiprovider.NewProbe(nil))
	providerController := httpapi.NewProviderController(providerService, logger)
	workRepository := storage.NewWorkRepository(providerRepository)
	workService := application.NewWorkService(workRepository)
	workController := httpapi.NewWorkController(workService, logger)
	agentRepository := storage.NewAgentRepository(providerRepository)
	if err := agentRepository.FailInterruptedRuns(); err != nil {
		return err
	}
	skillsDirectory, err := resolveSkillsDirectory()
	if err != nil {
		return err
	}
	skillCatalog, err := agent.LoadCatalog(skillsDirectory)
	if err != nil {
		return err
	}
	logger.Info("Warmmo skills loaded", "directory", skillsDirectory, "count", skillCatalog.Len())
	canvasRepository := storage.NewCanvasRepository(providerRepository)
	workFileRepository := storage.NewWorkFileRepository(dataDirectory)
	contextSearchGateway := storage.NewContextSearchGateway(ctx, func() (*storage.ContextIndex, error) {
		return configureContextIndex(providerRepository, providerService)
	})
	toolRegistry := agentcore.NewToolRegistry(
		agenttools.NewGetNodesTool(canvasRepository),
		agenttools.NewSearchTextTool(workFileRepository),
		agenttools.NewSearchStorySpineTool(workFileRepository, agentRepository),
		agenttools.NewCreateCandidateTool(canvasRepository),
		agenttools.NewSearchContextTool(contextSearchGateway),
	)
	agentLoop := agent.NewLoop(skillCatalog, toolRegistry, agentcore.DefaultBudget())
	agentEngine := adk.NewEngine(agentLoop, func(_ context.Context, providerID, modelID string) (adk.ModelConfig, error) {
		baseURL, apiKey, err := providerRepository.ResolveModel(providerID, modelID)
		if err != nil {
			return adk.ModelConfig{}, err
		}
		return adk.ModelConfig{BaseURL: baseURL, APIKey: apiKey, ModelID: modelID}, nil
	})
	agentService := application.NewAgentService(ctx, agentRepository, agentEngine, logger)
	agentController := httpapi.NewAgentController(agentService, logger)
	canvasService := application.NewCanvasService(canvasRepository)
	canvasService.SetCandidateDecisionHandler(agentService.ResumeAfterCandidateDecision)
	canvasController := httpapi.NewCanvasController(canvasService, logger)
	server := &http.Server{
		Addr:              "127.0.0.1:8787",
		Handler:           httpapi.NewRouter(runtimeController, providerController, workController, agentController, canvasController),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("Warmmo Core is listening", "address", "http://"+server.Addr)
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

func configureContextIndex(providerRepository *storage.ProviderRepository, providerService *application.ProviderService) (*storage.ContextIndex, error) {
	providerID := strings.TrimSpace(os.Getenv("WARMMO_EMBEDDING_PROVIDER_ID"))
	modelID := strings.TrimSpace(os.Getenv("WARMMO_EMBEDDING_MODEL_ID"))
	if providerID == "" && modelID == "" {
		enabledModels, err := providerService.EnabledModels()
		if err != nil {
			return nil, fmt.Errorf("list enabled embedding models: %w", err)
		}
		for _, enabledModel := range enabledModels {
			if enabledModel.Capability != ai.ModelCapabilityEmbedding {
				continue
			}
			providerID = enabledModel.ProviderID
			modelID = enabledModel.ModelID
			break
		}
		if providerID == "" {
			return nil, nil
		}
	}
	if providerID == "" || modelID == "" {
		return nil, errors.New("WARMMO_EMBEDDING_PROVIDER_ID and WARMMO_EMBEDDING_MODEL_ID must be configured together")
	}
	if providerID != ai.CanonicalEmbeddingProviderID {
		return nil, fmt.Errorf("context search only supports embedding provider %q", ai.CanonicalEmbeddingProviderID)
	}
	if modelID != ai.CanonicalEmbeddingModelID {
		return nil, fmt.Errorf("context search only supports embedding model %q", ai.CanonicalEmbeddingModelID)
	}
	baseURL, apiKey, err := providerRepository.ResolveModel(providerID, modelID)
	if err != nil {
		return nil, fmt.Errorf("resolve embedding model: %w", err)
	}
	embedder, err := embedding.NewOpenAICompatible(embedding.OpenAICompatibleConfig{
		BaseURL: baseURL, APIKey: apiKey, ModelID: modelID,
		Dimensions: storage.ContextEmbeddingDimension,
	})
	if err != nil {
		return nil, err
	}
	return storage.NewContextIndex(providerRepository, embedder), nil
}

func resolveDataDirectory() (string, error) {
	if configured := os.Getenv("WARMMO_DATA_DIR"); configured != "" {
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
	return filepath.Join(configDirectory, "warmmo"), nil
}

func resolveSkillsDirectory() (string, error) {
	if configured := os.Getenv("WARMMO_SKILLS_DIR"); configured != "" {
		return filepath.Abs(configured)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if fileExists(filepath.Join(workingDirectory, "core", "go.mod")) {
		return filepath.Join(workingDirectory, "core", "skills"), nil
	}
	if fileExists(filepath.Join(workingDirectory, "go.mod")) && filepath.Base(workingDirectory) == "core" {
		return filepath.Join(workingDirectory, "skills"), nil
	}
	return "", errors.New("cannot locate built-in skills; set WARMMO_SKILLS_DIR")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

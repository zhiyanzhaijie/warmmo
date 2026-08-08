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

	"warmnote/core/internal/agent"
	"warmnote/core/internal/agent/adkadapter"
	"warmnote/core/internal/canvas"
	"warmnote/core/internal/controller"
	"warmnote/core/internal/embedding"
	"warmnote/core/internal/model"
	"warmnote/core/internal/repository"
	"warmnote/core/internal/service"
	"warmnote/core/internal/webserver"
	"warmnote/core/internal/workspace"
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
	workRepository := repository.NewWorkRepository(providerRepository)
	workService := service.NewWorkService(workRepository)
	workController := controller.NewWorkController(workService, logger)
	agentRepository := repository.NewAgentRepository(providerRepository)
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
	logger.Info("Warmnote skills loaded", "directory", skillsDirectory, "count", skillCatalog.Len())
	canvasRepository := repository.NewCanvasRepository(providerRepository)
	workFileRepository := repository.NewWorkFileRepository(dataDirectory)
	contextSearchGateway := repository.NewContextSearchGateway(ctx, func() (*repository.ContextIndex, error) {
		return configureContextIndex(providerRepository, providerService)
	})
	toolRegistry := agent.NewToolRegistry(
		canvas.NewGetNodesTool(canvasRepository),
		workspace.NewSearchTextTool(workFileRepository),
		canvas.NewSearchStorySpineTool(workFileRepository, agentRepository),
		canvas.NewCreateCandidateTool(canvasRepository),
		canvas.NewSearchContextTool(contextSearchGateway),
	)
	agentLoop := agent.NewLoop(skillCatalog, toolRegistry, agent.DefaultBudget())
	agentEngine := adkadapter.NewEngine(agentLoop, func(_ context.Context, providerID, modelID string) (adkadapter.ModelConfig, error) {
		baseURL, apiKey, err := providerRepository.ResolveModel(providerID, modelID)
		if err != nil {
			return adkadapter.ModelConfig{}, err
		}
		return adkadapter.ModelConfig{BaseURL: baseURL, APIKey: apiKey, ModelID: modelID}, nil
	})
	agentService := service.NewAgentService(ctx, agentRepository, agentEngine, logger)
	agentController := controller.NewAgentController(agentService, logger)
	canvasService := service.NewCanvasService(canvasRepository)
	canvasController := controller.NewCanvasController(canvasService, logger)
	server := &http.Server{
		Addr:              "127.0.0.1:8787",
		Handler:           webserver.NewRouter(runtimeController, providerController, workController, agentController, canvasController),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      0,
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

func configureContextIndex(providerRepository *repository.ProviderRepository, providerService *service.ProviderService) (*repository.ContextIndex, error) {
	providerID := strings.TrimSpace(os.Getenv("WARMNOTE_EMBEDDING_PROVIDER_ID"))
	modelID := strings.TrimSpace(os.Getenv("WARMNOTE_EMBEDDING_MODEL_ID"))
	if providerID == "" && modelID == "" {
		enabledModels, err := providerService.EnabledModels()
		if err != nil {
			return nil, fmt.Errorf("list enabled embedding models: %w", err)
		}
		for _, enabledModel := range enabledModels {
			if enabledModel.Capability != model.ModelCapabilityEmbedding {
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
		return nil, errors.New("WARMNOTE_EMBEDDING_PROVIDER_ID and WARMNOTE_EMBEDDING_MODEL_ID must be configured together")
	}
	if providerID != model.CanonicalEmbeddingProviderID {
		return nil, fmt.Errorf("context search only supports embedding provider %q", model.CanonicalEmbeddingProviderID)
	}
	if modelID != model.CanonicalEmbeddingModelID {
		return nil, fmt.Errorf("context search only supports embedding model %q", model.CanonicalEmbeddingModelID)
	}
	baseURL, apiKey, err := providerRepository.ResolveModel(providerID, modelID)
	if err != nil {
		return nil, fmt.Errorf("resolve embedding model: %w", err)
	}
	embedder, err := embedding.NewOpenAICompatible(embedding.OpenAICompatibleConfig{
		BaseURL: baseURL, APIKey: apiKey, ModelID: modelID,
		Dimensions: repository.ContextEmbeddingDimension,
	})
	if err != nil {
		return nil, err
	}
	return repository.NewContextIndex(providerRepository, embedder), nil
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

func resolveSkillsDirectory() (string, error) {
	if configured := os.Getenv("WARMNOTE_SKILLS_DIR"); configured != "" {
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
	return "", errors.New("cannot locate built-in skills; set WARMNOTE_SKILLS_DIR")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

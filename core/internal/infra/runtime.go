package infra

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

	adk "warmmo/core/internal/adapter/agent/adk"
	"warmmo/core/internal/adapter/agent/collaboration"
	agentcore "warmmo/core/internal/adapter/agent/core"
	"warmmo/core/internal/adapter/agent/embedding"
	aiprovider "warmmo/core/internal/adapter/agent/provider"
	agenttools "warmmo/core/internal/adapter/agent/tools"
	agentworkspace "warmmo/core/internal/adapter/agent/workspace"
	agent "warmmo/core/internal/adapter/agent/writing"
	"warmmo/core/internal/adapter/httpapi"
	"warmmo/core/internal/adapter/persistence"
	"warmmo/core/internal/application"
	"warmmo/core/internal/domain/ai"
)

func Run(logger *slog.Logger, version string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtimeService := application.NewRuntimeService(version)
	runtimeController := httpapi.NewRuntimeController(runtimeService, logger)
	dataDirectory, err := resolveDataDirectory()
	if err != nil {
		return err
	}
	database, err := persistence.OpenDatabase(dataDirectory)
	if err != nil {
		return err
	}
	defer database.Close()
	providerRepository := persistence.NewProviderRepositoryWithDatabase(database)
	logger.Info("Warmmo data initialized", "database", database.Path())
	providerService := application.NewProviderService(providerRepository, aiprovider.NewProbe(nil))
	providerController := httpapi.NewProviderController(providerService, logger)
	workRepository := persistence.NewWorkRepositoryWithDatabase(database)
	workService := application.NewWorkService(workRepository)
	workController := httpapi.NewWorkController(workService, logger)
	agentRepository := persistence.NewAgentRepositoryWithDatabase(database)
	skillsDirectory, err := resolveSkillsDirectory()
	if err != nil {
		return err
	}
	skillCatalog, err := agent.LoadCatalog(skillsDirectory)
	if err != nil {
		return err
	}
	logger.Info("Warmmo skills loaded", "directory", skillsDirectory, "count", skillCatalog.Len())
	canvasRepository := persistence.NewCanvasRepositoryWithDatabase(database)
	workspaceSearcher := agentworkspace.NewSearcher(dataDirectory)
	contextSearchGateway := persistence.NewContextSearchGateway(ctx, func() (*persistence.ContextIndex, error) {
		return configureContextIndex(database, providerRepository, providerService)
	})
	toolRegistry := agentcore.NewToolRegistry(
		agenttools.NewGetNodesTool(canvasRepository),
		agenttools.NewSearchTextTool(workspaceSearcher),
		agenttools.NewSearchStorySpineTool(workspaceSearcher, agentRepository),
		agenttools.NewCreateCandidateTool(canvasRepository),
		agenttools.NewSearchContextTool(contextSearchGateway),
	)
	modelResolver := func(_ context.Context, providerID, modelID string) (adk.ModelConfig, error) {
		baseURL, apiKey, err := providerRepository.ResolveModel(providerID, modelID)
		if err != nil {
			return adk.ModelConfig{}, err
		}
		return adk.ModelConfig{BaseURL: baseURL, APIKey: apiKey, ModelID: modelID}, nil
	}
	agentSessionService := persistence.NewAgentSessionService(database)
	agentCheckpointStore := persistence.NewAgentCheckpointStore(database)
	agentArtifactStore := persistence.NewAgentArtifactStore(database)
	agentToolCallStore := persistence.NewAgentToolCallStore(database)
	agentMemoryStore := persistence.NewAgentMemoryStore(database)
	agentConversationStore := persistence.NewAgentConversationStore(database)
	turnExecutor := adk.NewLLMTurnExecutorWithConversation(
		toolRegistry, modelResolver, agentSessionService, agentCheckpointStore, agentArtifactStore, agentToolCallStore,
		agentMemoryStore, agentConversationStore, agentRepository,
	)
	definitionRegistry, err := collaboration.NewDefinitionRegistry()
	if err != nil {
		return err
	}
	for _, definition := range collaboration.Definitions() {
		if _, err := toolRegistry.StrictSnapshot(definition.Tools); err != nil {
			return fmt.Errorf("validate tools for agent definition %q: %w", definition.ID, err)
		}
	}
	agentChildRunStore := persistence.NewAgentChildRunStore(database)
	durableTurnRunner, err := collaboration.NewDurableChildRunner(
		turnExecutor, definitionRegistry, agentCheckpointStore, agentChildRunStore,
	)
	if err != nil {
		return err
	}
	writingChain, err := collaboration.NewWritingCollaborationChain(
		definitionRegistry, durableTurnRunner, agentArtifactStore, agentCheckpointStore, skillCatalog,
	)
	if err != nil {
		return err
	}
	canvasOrchestrator, err := collaboration.NewCanvasOrchestrator(
		definitionRegistry, durableTurnRunner, writingChain, agentCheckpointStore,
	)
	if err != nil {
		return err
	}
	nonCollaborativeChain, err := collaboration.NewNonCollaborativeChain(
		definitionRegistry, durableTurnRunner, agentArtifactStore, agentCheckpointStore, skillCatalog,
	)
	if err != nil {
		return err
	}
	agentEngine, err := collaboration.NewEngine(canvasOrchestrator, nonCollaborativeChain)
	if err != nil {
		return err
	}
	agentService := application.NewAgentService(ctx, agentRepository, agentEngine, logger, agentConversationStore)
	if err := agentService.RecoverInterruptedRuns(); err != nil {
		logger.Error("recover interrupted agent runs", "error", err)
	}
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

func configureContextIndex(database *persistence.Database, providerRepository *persistence.ProviderRepository, providerService *application.ProviderService) (*persistence.ContextIndex, error) {
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
		Dimensions: persistence.ContextEmbeddingDimension,
	})
	if err != nil {
		return nil, err
	}
	return persistence.NewContextIndexWithDatabase(database, embedder), nil
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

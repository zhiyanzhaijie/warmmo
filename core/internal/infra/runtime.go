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

	"google.golang.org/adk/model"

	adk "warmmo/core/internal/adapter/agent/adk"
	"warmmo/core/internal/adapter/agent/embedding"
	"warmmo/core/internal/adapter/agent/projection"
	aiprovider "warmmo/core/internal/adapter/agent/provider"
	agenttool "warmmo/core/internal/adapter/agenttool"
	"warmmo/core/internal/adapter/httpapi"
	"warmmo/core/internal/adapter/persistence"
	"warmmo/core/internal/adapter/skillcatalog"
	adapterworkspace "warmmo/core/internal/adapter/workspace"
	"warmmo/core/internal/application"
	appharness "warmmo/core/internal/application/harness"
	"warmmo/core/internal/application/orchestration"
	"warmmo/core/internal/domain/ai"
)

func Run(logger *slog.Logger, version, allowedOrigin string) error {
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
	skillCatalog, err := skillcatalog.LoadCatalog(skillsDirectory)
	if err != nil {
		return err
	}
	logger.Info("Warmmo skills loaded", "directory", skillsDirectory, "count", skillCatalog.Len())
	canvasRepository := persistence.NewCanvasRepositoryWithDatabase(database)
	workspaceSearcher := adapterworkspace.NewSearcher(dataDirectory)
	contextSearchGateway := persistence.NewContextSearchGateway(ctx, func() (*persistence.ContextIndex, error) {
		return configureContextIndex(database, providerRepository, providerService)
	})
	toolRegistry := agenttool.NewRegistry(
		agenttool.NewGetNodesTool(canvasRepository),
		agenttool.NewSearchTextTool(workspaceSearcher),
		agenttool.NewSearchStorySpineTool(workspaceSearcher, agentRepository),
		agenttool.NewCreateCandidateTool(canvasRepository),
		agenttool.NewSearchContextTool(contextSearchGateway),
	)
	structuredOutput := aiprovider.StructuredOutputRegistry{
		"deepseek": aiprovider.StructuredOutputToolCall,
		"openai":   aiprovider.StructuredOutputJSONSchema,
	}
	modelResolver := func(_ context.Context, providerID, modelID string) (model.LLM, error) {
		baseURL, apiKey, err := providerRepository.ResolveModel(providerID, modelID)
		if err != nil {
			return nil, err
		}
		return aiprovider.NewOpenAICompatible(aiprovider.OpenAICompatibleConfig{
			BaseURL: baseURL, APIKey: apiKey, ModelID: modelID,
			StructuredOutput: structuredOutput.Resolve(providerID),
		}, nil), nil
	}
	agentSessionService := persistence.NewAgentSessionService(database)
	agentCheckpointStore := persistence.NewAgentCheckpointStore(database)
	agentArtifactStore := persistence.NewAgentArtifactStore(database)
	agentToolCallStore := persistence.NewAgentToolCallStore(database)
	agentMemoryStore := persistence.NewAgentMemoryStore(database)
	agentConversationStore := persistence.NewAgentConversationStore(database)
	outcomeConsumer := projection.NewOutcomeConsumer(agentConversationStore, agentMemoryStore)
	definitionRegistry, err := orchestration.NewDefinitionRegistry()
	if err != nil {
		return err
	}
	for _, definition := range orchestration.Definitions() {
		if _, err := toolRegistry.StrictSnapshot(definition.Tools); err != nil {
			return fmt.Errorf("validate tools for agent definition %q: %w", definition.ID, err)
		}
	}
	var delegationTools *agenttool.DelegationTools
	turnExecutor := adk.NewLLMTurnExecutor(adk.LLMTurnDependencies{
		Tools: toolRegistry, Resolve: modelResolver, Sessions: agentSessionService,
		Checkpoints: agentCheckpointStore, ToolCalls: agentToolCallStore,
		Memories: agentMemoryStore, Conversation: agentConversationStore, AmbientContext: agentRepository,
		Consume: outcomeConsumer.Consume,
		DynamicTools: func(request appharness.RuntimeRequest, emit appharness.RuntimeEmitter) ([]appharness.Tool, error) {
			if delegationTools == nil {
				return nil, errors.New("delegation tools are not configured")
			}
			return delegationTools.Provider(request, emit)
		},
	})
	writingChain, err := orchestration.NewWritingCollaborationChain(
		definitionRegistry, turnExecutor, agentArtifactStore, agentCheckpointStore, skillCatalog,
	)
	if err != nil {
		return err
	}
	delegator, err := orchestration.NewSpecialistDelegator(writingChain)
	if err != nil {
		return err
	}
	delegationTools, err = agenttool.NewDelegationTools(delegator)
	if err != nil {
		return err
	}
	canvasOrchestrator, err := orchestration.NewCanvasOrchestrator(
		definitionRegistry, turnExecutor, writingChain, agentCheckpointStore,
	)
	if err != nil {
		return err
	}
	nonCollaborativeChain, err := orchestration.NewNonCollaborativeChain(
		definitionRegistry, turnExecutor, agentArtifactStore, agentCheckpointStore, skillCatalog,
	)
	if err != nil {
		return err
	}
	agentEngine, err := orchestration.NewEngine(canvasOrchestrator, nonCollaborativeChain)
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
	localSecurity, err := httpapi.NewLocalSecurityFromEnvironment(allowedOrigin)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              "127.0.0.1:8787",
		Handler:           httpapi.NewRouter(runtimeController, providerController, workController, agentController, canvasController, localSecurity),
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

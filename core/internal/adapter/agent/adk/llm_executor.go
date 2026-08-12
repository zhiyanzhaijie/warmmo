package adk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	agentcore "warmmo/core/internal/adapter/agent/core"
	appharness "warmmo/core/internal/application/harness"
)

type LLMTurnStatus = appharness.TurnStatus

const (
	LLMTurnCompleted LLMTurnStatus = appharness.TurnCompleted
	LLMTurnPaused    LLMTurnStatus = appharness.TurnAwaitingUser
)

type LLMTurnEventType string

const (
	LLMEventMessageDelta       LLMTurnEventType = "message.delta"
	LLMEventReasoningStarted   LLMTurnEventType = "reasoning.started"
	LLMEventReasoningDelta     LLMTurnEventType = "reasoning.delta"
	LLMEventReasoningCompleted LLMTurnEventType = "reasoning.completed"
	LLMEventArtifactDelta      LLMTurnEventType = "artifact.delta"
	LLMEventToolRequested      LLMTurnEventType = "tool.requested"
	LLMEventToolStarted        LLMTurnEventType = "tool.started"
	LLMEventToolCompleted      LLMTurnEventType = "tool.completed"
	LLMEventToolFailed         LLMTurnEventType = "tool.failed"
	LLMEventMemoryFailed       LLMTurnEventType = "memory.failed"
	LLMEventConversationFailed LLMTurnEventType = "conversation.failed"
	LLMEventTurnCompleted      LLMTurnEventType = "turn.completed"
	LLMEventTurnPaused         LLMTurnEventType = "turn.paused"
	LLMEventTurnStopped        LLMTurnEventType = "turn.stopped"
	LLMEventTurnCancelled      LLMTurnEventType = "turn.cancelled"
)

type LLMTurnRequest struct {
	RunID                   string
	TurnID                  string
	ParentTurnID            string
	ParentAgentID           string
	AgentID                 string
	AgentName               string
	Description             string
	Instruction             string
	DefinitionVersion       string
	DefinitionHash          string
	PromptHash              string
	ToolsetHash             string
	ProviderID              string
	ModelID                 string
	ConversationSessionID   string
	UserID                  string
	SessionID               string
	Prompt                  string
	ConversationUserContent string
	PublishConversation     bool
	AllowedTools            []string
	ControlTools            []string
	AllowedChildren         []appharness.ChildContract
	ToolInvocation          agentcore.ToolInvocation
	Budget                  appharness.BudgetPolicy
	Context                 appharness.ContextPolicy
	Memory                  appharness.MemoryPolicy
	Output                  appharness.OutputContract
	Resume                  *appharness.ResumeInput
}

type LLMTurnEvent struct {
	Type        LLMTurnEventType
	EventID     string
	AgentName   string
	Text        string
	ToolName    string
	ToolCallID  string
	ErrorCode   string
	Retryable   bool
	Summary     string
	ResultBytes int
	Truncated   bool
	Payload     json.RawMessage
	Usage       appharness.Usage
	Budget      appharness.BudgetUsage
}

type LLMTurnOutcome = appharness.TurnOutcome

type LLMTurnEmitter func(LLMTurnEvent) error

type LLMTurnExecutor struct {
	tools        *agentcore.ToolRegistry
	resolve      ModelResolver
	sessions     session.Service
	checkpoints  appharness.CheckpointStore
	artifacts    appharness.ArtifactStore
	toolCalls    appharness.ToolCallStore
	memories     appharness.MemoryStore
	conversation appharness.ConversationStore
	canvas       appharness.CanvasContextStore
	httpClient   *http.Client
}

func NewLLMTurnExecutor(
	tools *agentcore.ToolRegistry,
	resolve ModelResolver,
	sessions session.Service,
	checkpoints appharness.CheckpointStore,
	artifacts appharness.ArtifactStore,
	toolCalls appharness.ToolCallStore,
	memories appharness.MemoryStore,
) *LLMTurnExecutor {
	return NewLLMTurnExecutorWithConversation(tools, resolve, sessions, checkpoints, artifacts, toolCalls, memories, nil, nil)
}

func NewLLMTurnExecutorWithConversation(
	tools *agentcore.ToolRegistry,
	resolve ModelResolver,
	sessions session.Service,
	checkpoints appharness.CheckpointStore,
	artifacts appharness.ArtifactStore,
	toolCalls appharness.ToolCallStore,
	memories appharness.MemoryStore,
	conversation appharness.ConversationStore,
	canvas appharness.CanvasContextStore,
) *LLMTurnExecutor {
	return &LLMTurnExecutor{
		tools: tools, resolve: resolve, sessions: sessions, checkpoints: checkpoints,
		artifacts: artifacts, toolCalls: toolCalls, memories: memories,
		conversation: conversation, canvas: canvas,
		httpClient: &http.Client{Timeout: 4 * time.Minute},
	}
}

func (e *LLMTurnExecutor) Run(ctx context.Context, request LLMTurnRequest, emit LLMTurnEmitter) (LLMTurnOutcome, error) {
	prepared, err := e.prepareRequest(request)
	if err != nil {
		return LLMTurnOutcome{}, err
	}
	request = prepared
	if err := e.validate(request, emit); err != nil {
		return LLMTurnOutcome{}, err
	}
	var prior *appharness.Checkpoint
	if request.Resume != nil {
		checkpoint, err := e.resumeCheckpoint(ctx, request)
		if err != nil {
			return LLMTurnOutcome{}, err
		}
		prior = &checkpoint
	} else {
		if outcome, found, err := e.completedOutcome(ctx, request); err != nil {
			return LLMTurnOutcome{}, err
		} else if found {
			e.consumeMemoryBestEffort(ctx, request, outcome, emit)
			e.consumeConversationBestEffort(ctx, request, outcome, emit)
			return outcome, nil
		}
	}
	checkpointVersion := int64(0)
	initialBudget := appharness.BudgetUsage{}
	initialUsage := appharness.Usage{}
	childRunIDs := []string(nil)
	var compactionManifest json.RawMessage
	lastCheckpointEventID := ""
	var checkpointMu sync.Mutex
	if prior != nil {
		checkpointVersion = prior.Version
		initialBudget = prior.Budget
		initialUsage = prior.Usage
		childRunIDs = append([]string(nil), prior.ChildRunIDs...)
		compactionManifest = append(json.RawMessage(nil), prior.CompactionManifest...)
		lastCheckpointEventID = prior.LastCanonicalEventID
	}
	saveRunningCheckpoint := func(
		saveCtx context.Context,
		usage appharness.BudgetUsage,
		manifest json.RawMessage,
	) error {
		checkpointMu.Lock()
		defer checkpointMu.Unlock()
		if manifest != nil {
			compactionManifest = append(compactionManifest[:0], manifest...)
		}
		checkpoint := appharness.Checkpoint{
			RunID: request.RunID, TurnID: request.TurnID, SessionID: request.SessionID, AgentID: request.AgentID,
			DefinitionVersion: request.DefinitionVersion, DefinitionHash: request.DefinitionHash,
			PromptHash: request.PromptHash, ToolsetHash: request.ToolsetHash,
			Status: appharness.TurnRunning, Budget: usage, Version: checkpointVersion,
			Snapshot: snapshotFromRequest(request),
			Usage:    initialUsage, ChildRunIDs: childRunIDs, CompactionManifest: compactionManifest,
			LastCanonicalEventID: lastCheckpointEventID,
		}
		stored, err := e.checkpoints.SaveCheckpoint(saveCtx, checkpoint)
		if err != nil {
			return err
		}
		checkpointVersion = stored.Version
		return nil
	}
	reserveBudget := func(reserveCtx context.Context, usage appharness.BudgetUsage) error {
		return saveRunningCheckpoint(reserveCtx, usage, nil)
	}
	policy, err := newTurnPolicy(turnPolicyConfig{
		Budget: request.Budget, MaxToolResultBytes: request.Context.MaxToolResultBytes,
		Initial: initialBudget, Emit: emit, Reserve: reserveBudget, Artifacts: e.artifacts,
		RunID: request.RunID, TurnID: request.TurnID, AgentID: request.AgentID,
	})
	if err != nil {
		return LLMTurnOutcome{}, err
	}
	if prior != nil {
		resumeStart := *prior
		resumeStart.Status = appharness.TurnRunning
		resumeStart.StopReason = ""
		resumeStart.Pending = nil
		resumeStart.Snapshot = snapshotFromRequest(request)
		checkpointMu.Lock()
		stored, err := e.checkpoints.SaveCheckpoint(ctx, resumeStart)
		checkpointMu.Unlock()
		if err != nil {
			return LLMTurnOutcome{}, fmt.Errorf("persist resume start: %w", err)
		}
		checkpointVersion = stored.Version
	}
	runCtx, cancel := context.WithTimeout(ctx, policy.budget.MaxDuration)
	defer cancel()
	if err := e.ensureSession(runCtx, request); err != nil {
		return LLMTurnOutcome{}, err
	}
	modelConfig, err := e.resolve(runCtx, request.ProviderID, request.ModelID)
	if err != nil {
		return LLMTurnOutcome{}, fmt.Errorf("resolve model: %w", err)
	}
	additionalTools, submitTool, err := e.additionalTools(request)
	if err != nil {
		return LLMTurnOutcome{}, err
	}
	tools, err := adaptTools(e.tools, request.AllowedTools, request.ToolInvocation, policy, e.toolCalls, additionalTools...)
	if err != nil {
		return LLMTurnOutcome{}, err
	}
	memoryFrozen, frozenMemoryIDs, err := frozenRecallFromManifest(compactionManifest)
	if err != nil {
		return LLMTurnOutcome{}, err
	}
	budgeter := contextBudgeter{
		policy: request.Context, memory: request.Memory, memories: e.memories, artifacts: e.artifacts,
		conversation: e.conversation, canvas: e.canvas,
		runID: request.RunID, turnID: request.TurnID, agentID: request.AgentID,
		workID: request.ToolInvocation.WorkID, query: memoryRecallQuery(request),
		conversationSessionID: request.ConversationSessionID,
		memoryFrozen:          memoryFrozen, frozenMemoryIDs: frozenMemoryIDs,
		persist: func(persistCtx context.Context, manifest json.RawMessage) error {
			return saveRunningCheckpoint(persistCtx, policy.Usage(), manifest)
		},
	}
	configuredAgent, err := llmagent.New(llmagent.Config{
		Name: request.AgentName, Description: request.Description,
		Instruction: request.Instruction, Model: NewLLM(modelConfig, e.httpClient), Tools: tools,
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{policy.beforeModel, budgeter.beforeModel},
	})
	if err != nil {
		return LLMTurnOutcome{}, fmt.Errorf("create LLM agent: %w", err)
	}
	adkRunner, err := runner.New(runner.Config{
		AppName: appName, Agent: configuredAgent, SessionService: e.sessions,
	})
	if err != nil {
		return LLMTurnOutcome{}, fmt.Errorf("create ADK runner: %w", err)
	}

	message, err := e.turnMessage(runCtx, request)
	if err != nil {
		return LLMTurnOutcome{}, err
	}
	outcome := LLMTurnOutcome{SessionID: request.SessionID, Usage: initialUsage}
	lastEventID := ""
	projector := newLLMEventProjector(policy.project)
	finalSeen := false
	var terminalErr error
runLoop:
	for event, runErr := range adkRunner.Run(runCtx, request.UserID, request.SessionID, message, agent.RunConfig{
		StreamingMode: agent.StreamingModeSSE,
	}) {
		if runErr != nil {
			if err := projector.completeReasoning(lastEventID, request.AgentName); err != nil {
				return e.finish(ctx, request, outcome, lastEventID, fmt.Errorf("complete reasoning phase: %w", err))
			}
			outcome.Budget = policy.Usage()
			if errors.Is(runErr, appharness.ErrBudgetExceeded) || isBudgetDeadline(ctx, runCtx, runErr) {
				outcome.Status = appharness.TurnStoppedBudget
				outcome.StopReason = appharness.StopBudgetExceeded
				return e.finishExpected(ctx, request, outcome, lastEventID, emit, policy)
			}
			if isContextCancellation(ctx, runErr) {
				outcome.Status = appharness.TurnCancelled
				outcome.StopReason = appharness.StopContextCancelled
				return e.finishExpected(ctx, request, outcome, lastEventID, emit, policy)
			}
			outcome.Status = appharness.TurnFailed
			outcome.StopReason = appharness.StopExecutionFailed
			return e.finish(ctx, request, outcome, lastEventID, fmt.Errorf("run LLM agent: %w", runErr))
		}
		if event == nil {
			continue
		}
		if err := projector.project(event, &outcome); err != nil {
			outcome.Budget = policy.Usage()
			outcome.Status = appharness.TurnFailed
			outcome.StopReason = appharness.StopExecutionFailed
			failedLastEventID := lastEventID
			if !event.Partial {
				failedLastEventID = event.ID
			}
			return e.finish(ctx, request, outcome, failedLastEventID, fmt.Errorf("project LLM event: %w", err))
		}
		if !event.Partial {
			lastEventID = event.ID
		}
		if event.IsFinalResponse() {
			finalSeen = true
			if eventStoppedForBudget(event) {
				outcome.Status = appharness.TurnStoppedBudget
				outcome.StopReason = appharness.StopBudgetExceeded
				outcome.Pending = nil
			} else if eventStoppedForExecution(event) {
				outcome.Status = appharness.TurnFailed
				outcome.StopReason = appharness.StopExecutionFailed
				outcome.Pending = nil
				terminalErr = policy.ExecutionError()
			} else if eventNeedsControl(event) {
				if err := policy.reserveControlTool(runCtx); err != nil {
					if errors.Is(err, appharness.ErrBudgetExceeded) {
						outcome.Status = appharness.TurnStoppedBudget
						outcome.StopReason = appharness.StopBudgetExceeded
						return e.finishExpected(ctx, request, outcome, lastEventID, emit, policy)
					}
					outcome.Status = appharness.TurnFailed
					outcome.StopReason = appharness.StopExecutionFailed
					return e.finish(ctx, request, outcome, lastEventID, err)
				}
				outcome.Pending = pendingAction(event)
				if outcome.Pending != nil && outcome.Pending.Kind == "child" {
					outcome.Status = appharness.TurnAwaitingChild
					outcome.StopReason = appharness.StopChildPending
				} else {
					outcome.Status = appharness.TurnAwaitingUser
					outcome.StopReason = appharness.StopUserInputRequired
				}
			} else {
				outcome.Status = appharness.TurnCompleted
				outcome.StopReason = appharness.StopFinalResponse
			}
		}
		if outcome.Status == appharness.TurnAwaitingUser || outcome.Status == appharness.TurnAwaitingChild {
			break runLoop
		}
		if submitted := submitTool.Submitted(); submitted != nil && eventContainsToolResponse(event, SubmitArtifactToolName) {
			outcome.Status = appharness.TurnCompleted
			outcome.StopReason = appharness.StopArtifactSubmitted
			outcome.Artifact = submitted
			finalSeen = true
			break runLoop
		}
	}
	if !finalSeen {
		if err := projector.completeReasoning(lastEventID, request.AgentName); err != nil {
			return e.finish(ctx, request, outcome, lastEventID, fmt.Errorf("complete reasoning phase: %w", err))
		}
		outcome.Budget = policy.Usage()
		if runCtx.Err() != nil && ctx.Err() == nil {
			outcome.Status = appharness.TurnStoppedBudget
			outcome.StopReason = appharness.StopBudgetExceeded
			return e.finishExpected(ctx, request, outcome, lastEventID, emit, policy)
		}
		if ctx.Err() != nil {
			outcome.Status = appharness.TurnCancelled
			outcome.StopReason = appharness.StopContextCancelled
			return e.finishExpected(ctx, request, outcome, lastEventID, emit, policy)
		}
		outcome.Status = appharness.TurnFailed
		outcome.StopReason = appharness.StopExecutionFailed
		return e.finish(ctx, request, outcome, lastEventID, errors.New("LLM agent completed without a final event"))
	}
	if outcome.Status == appharness.TurnCompleted && request.Resume != nil && request.Resume.ToolName == DelegateAgentToolName {
		outcome.Artifact = delegatedArtifactRef(request.Resume.Response)
	}
	outcome.Budget = policy.Usage()
	outcome, err = e.finish(ctx, request, outcome, lastEventID, terminalErr)
	if err != nil {
		return outcome, err
	}
	e.consumeMemoryBestEffort(ctx, request, outcome, emit)
	e.consumeConversationBestEffort(ctx, request, outcome, emit)
	if err := emitTerminalOutcome(emit, lastEventID, request.AgentName, outcome); err != nil {
		return outcome, fmt.Errorf("%w: %v", appharness.ErrProjectionFailed, err)
	}
	if err := policy.ProjectionError(); err != nil {
		return outcome, fmt.Errorf("%w: %v", appharness.ErrProjectionFailed, err)
	}
	return outcome, nil
}

func delegatedArtifactRef(response map[string]any) *appharness.ArtifactRef {
	if response == nil {
		return nil
	}
	id, _ := response["artifactId"].(string)
	kind, _ := response["artifactKind"].(string)
	schemaVersion, _ := response["artifactSchemaVersion"].(string)
	if strings.TrimSpace(id) == "" || strings.TrimSpace(kind) == "" || strings.TrimSpace(schemaVersion) == "" {
		return nil
	}
	return &appharness.ArtifactRef{ID: id, Kind: kind, SchemaVersion: schemaVersion}
}

func (e *LLMTurnExecutor) validate(request LLMTurnRequest, emit LLMTurnEmitter) error {
	if e == nil || e.resolve == nil || e.sessions == nil || e.checkpoints == nil || e.artifacts == nil {
		return errors.New("LLM turn executor is not configured")
	}
	if (request.Memory.Recall || request.Memory.Remember) && e.memories == nil {
		return errors.New("agent memory store is required by the memory policy")
	}
	if emit == nil {
		return errors.New("LLM turn emitter is required")
	}
	if strings.TrimSpace(request.AgentName) == "" || strings.TrimSpace(request.Instruction) == "" {
		return errors.New("agent name and instruction are required")
	}
	if strings.TrimSpace(request.ProviderID) == "" || strings.TrimSpace(request.ModelID) == "" {
		return errors.New("provider and model are required")
	}
	if strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.SessionID) == "" {
		return errors.New("user and session are required")
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return errors.New("turn prompt is required")
	}
	if strings.TrimSpace(request.RunID) == "" || strings.TrimSpace(request.TurnID) == "" || strings.TrimSpace(request.AgentID) == "" {
		return errors.New("run, turn and agent IDs are required")
	}
	if strings.TrimSpace(request.DefinitionVersion) == "" || strings.TrimSpace(request.DefinitionHash) == "" ||
		strings.TrimSpace(request.PromptHash) == "" || strings.TrimSpace(request.ToolsetHash) == "" {
		return errors.New("definition version, definition hash, prompt hash and toolset hash are required")
	}
	if err := validateContextPolicy(request.Context); err != nil {
		return err
	}
	if request.Memory.Recall != (request.Context.Memory == "recall") {
		return errors.New("context and memory recall policies do not match")
	}
	return nil
}

func (e *LLMTurnExecutor) prepareRequest(request LLMTurnRequest) (LLMTurnRequest, error) {
	if request.PromptHash == "" {
		hash, err := appharness.StableHash(request.Instruction)
		if err != nil {
			return LLMTurnRequest{}, fmt.Errorf("hash instruction: %w", err)
		}
		request.PromptHash = hash
	}
	if request.ToolsetHash == "" {
		if e == nil || e.tools == nil {
			return LLMTurnRequest{}, errors.New("tool registry is required")
		}
		snapshot, err := e.tools.StrictSnapshot(request.AllowedTools)
		if err != nil {
			return LLMTurnRequest{}, err
		}
		specs := make([]agentcore.ToolSpec, 0, len(snapshot)+1)
		for _, tool := range snapshot {
			specs = append(specs, tool.Spec())
		}
		additional, _, err := e.additionalTools(request)
		if err != nil {
			return LLMTurnRequest{}, err
		}
		for _, tool := range additional {
			specs = append(specs, tool.Spec())
		}
		sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
		hash, err := appharness.StableHash(specs)
		if err != nil {
			return LLMTurnRequest{}, fmt.Errorf("hash tool snapshot: %w", err)
		}
		request.ToolsetHash = hash
	}
	return request, nil
}

func (e *LLMTurnExecutor) additionalTools(request LLMTurnRequest) ([]agentcore.Tool, *submitArtifactTool, error) {
	tools := make([]agentcore.Tool, 0, len(request.ControlTools)+1)
	for _, name := range request.ControlTools {
		switch name {
		case AskUserToolName:
			tools = append(tools, askUserTool{})
		case DelegateAgentToolName:
			tool, err := newDelegateAgentTool(request.AllowedChildren)
			if err != nil {
				return nil, nil, err
			}
			tools = append(tools, tool)
		default:
			return nil, nil, fmt.Errorf("unsupported control tool %q", name)
		}
	}
	var submit *submitArtifactTool
	if request.Output.Kind == appharness.OutputKindArtifact {
		var err error
		submit, err = newSubmitArtifactTool(request.Output, e.artifacts, request.RunID, request.TurnID, request.AgentID)
		if err != nil {
			return nil, nil, err
		}
		tools = append(tools, submit)
	}
	return tools, submit, nil
}

func (e *LLMTurnExecutor) turnMessage(ctx context.Context, request LLMTurnRequest) (*genai.Content, error) {
	if request.Resume == nil {
		return genai.NewContentFromText(request.Prompt, genai.RoleUser), nil
	}
	loaded, err := e.sessions.Get(ctx, &session.GetRequest{
		AppName: appName, UserID: request.UserID, SessionID: request.SessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("load agent session for control response: %w", err)
	}
	responses, err := controlResponses(loaded.Session, *request.Resume)
	if err != nil {
		return nil, err
	}
	return &genai.Content{Role: genai.RoleUser, Parts: responses}, nil
}

func controlResponses(current session.Session, resume appharness.ResumeInput) ([]*genai.Part, error) {
	if current == nil {
		return nil, errors.New("agent session is empty while resuming a control call")
	}
	callEventIndex := -1
	for index := current.Events().Len() - 1; index >= 0; index-- {
		event := current.Events().At(index)
		if eventHasFunctionCall(event, resume.ToolCallID) {
			callEventIndex = index
			break
		}
	}
	if callEventIndex < 0 {
		return nil, fmt.Errorf("control call %s is missing from agent session", resume.ToolCallID)
	}
	answered := make(map[string]struct{})
	for index := callEventIndex + 1; index < current.Events().Len(); index++ {
		event := current.Events().At(index)
		if event == nil || event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			if part != nil && part.FunctionResponse != nil {
				answered[part.FunctionResponse.ID] = struct{}{}
			}
		}
	}
	callEvent := current.Events().At(callEventIndex)
	parts := make([]*genai.Part, 0, len(callEvent.Content.Parts))
	foundSelected := false
	for _, part := range callEvent.Content.Parts {
		if part == nil || part.FunctionCall == nil || part.FunctionCall.ID == "" {
			continue
		}
		call := part.FunctionCall
		if _, exists := answered[call.ID]; exists {
			continue
		}
		response := cancelledControlResponse(call.ID)
		if call.ID == resume.ToolCallID {
			foundSelected = true
			response = resume.Response
		}
		parts = append(parts, &genai.Part{FunctionResponse: &genai.FunctionResponse{
			ID: call.ID, Name: call.Name, Response: response,
		}})
	}
	if !foundSelected {
		return nil, fmt.Errorf("control call %s has already been answered", resume.ToolCallID)
	}
	return parts, nil
}

func eventHasFunctionCall(event *session.Event, callID string) bool {
	if event == nil || event.Content == nil {
		return false
	}
	for _, part := range event.Content.Parts {
		if part != nil && part.FunctionCall != nil && part.FunctionCall.ID == callID {
			return true
		}
	}
	return false
}

func cancelledControlResponse(selectedCallID string) map[string]any {
	return map[string]any{
		"status": "cancelled",
		"error": map[string]any{
			"code":    "superseded_control_call",
			"message": fmt.Sprintf("Control call %s was cancelled because another action from the same model turn was selected.", selectedCallID),
		},
	}
}

func snapshotFromRequest(request LLMTurnRequest) *appharness.TurnSnapshot {
	return &appharness.TurnSnapshot{
		AgentName: request.AgentName, Description: request.Description, Instruction: request.Instruction,
		ProviderID: request.ProviderID, ModelID: request.ModelID, ConversationSessionID: request.ConversationSessionID, UserID: request.UserID, Prompt: request.Prompt,
		ConversationUserContent: request.ConversationUserContent, PublishConversation: request.PublishConversation,
		AllowedTools: append([]string(nil), request.AllowedTools...), ControlTools: append([]string(nil), request.ControlTools...),
		AllowedChildren: append([]appharness.ChildContract(nil), request.AllowedChildren...),
		WorkID:          request.ToolInvocation.WorkID, SkillID: request.ToolInvocation.SkillID,
		SkillVersion: request.ToolInvocation.SkillVersion, Budget: request.Budget, Output: request.Output,
		Context: request.Context, Memory: request.Memory,
	}
}

func eventContainsToolResponse(event *session.Event, name string) bool {
	if event == nil || event.Content == nil {
		return false
	}
	for _, part := range event.Content.Parts {
		if part != nil && part.FunctionResponse != nil && part.FunctionResponse.Name == name {
			return true
		}
	}
	return false
}

const reasoningDeltaChunkBytes = 512

type llmEventProjector struct {
	emit            LLMTurnEmitter
	reasoningActive bool
	reasoningBuffer strings.Builder
}

func newLLMEventProjector(emit LLMTurnEmitter) *llmEventProjector {
	return &llmEventProjector{emit: emit}
}

func (p *llmEventProjector) project(event *session.Event, outcome *LLMTurnOutcome) error {
	usage := appharness.Usage{}
	if event.UsageMetadata != nil && !event.Partial {
		usage.InputTokens = int64(event.UsageMetadata.PromptTokenCount)
		usage.CachedInputTokens = int64(event.UsageMetadata.CachedContentTokenCount)
		usage.OutputTokens = int64(event.UsageMetadata.CandidatesTokenCount)
		outcome.Usage.InputTokens += usage.InputTokens
		outcome.Usage.CachedInputTokens += usage.CachedInputTokens
		outcome.Usage.OutputTokens += usage.OutputTokens
	}
	if event.Content != nil {
		for _, part := range event.Content.Parts {
			if part == nil {
				continue
			}
			if part.Text != "" {
				if event.Partial && part.Thought {
					if err := p.appendReasoning(event.ID, event.Author, part.Text); err != nil {
						return err
					}
				} else if event.Partial {
					if err := p.completeReasoning(event.ID, event.Author); err != nil {
						return err
					}
					if err := p.emit(LLMTurnEvent{
						Type: LLMEventMessageDelta, EventID: event.ID, AgentName: event.Author, Text: part.Text,
					}); err != nil {
						return err
					}
				} else if event.IsFinalResponse() && !part.Thought {
					if outcome.Final == nil {
						outcome.Final = &appharness.Message{Role: string(genai.RoleModel)}
					}
					outcome.Final.Content += part.Text
				}
			}
			if part.FunctionCall != nil {
				if err := p.completeReasoning(event.ID, event.Author); err != nil {
					return err
				}
				if event.Partial {
					for _, argument := range part.FunctionCall.PartialArgs {
						if part.FunctionCall.Name == SubmitArtifactToolName && argument != nil &&
							argument.JsonPath == "$.artifact" && argument.StringValue != "" {
							if err := p.emit(LLMTurnEvent{
								Type: LLMEventArtifactDelta, EventID: event.ID,
								AgentName: event.Author, Text: argument.StringValue,
							}); err != nil {
								return err
							}
						}
					}
					continue
				}
				if err := p.emit(LLMTurnEvent{
					Type: LLMEventToolRequested, EventID: event.ID, AgentName: event.Author,
					ToolName: part.FunctionCall.Name, ToolCallID: part.FunctionCall.ID,
				}); err != nil {
					return err
				}
			}
			if part.FunctionResponse != nil {
				if err := p.completeReasoning(event.ID, event.Author); err != nil {
					return err
				}
				projected := projectToolResponse(event, part.FunctionResponse)
				if err := p.emit(projected); err != nil {
					return err
				}
			}
		}
	}
	if event.IsFinalResponse() {
		return p.completeReasoning(event.ID, event.Author)
	}
	return nil
}

func (p *llmEventProjector) appendReasoning(eventID, agentName, delta string) error {
	if !p.reasoningActive {
		if err := p.emit(LLMTurnEvent{Type: LLMEventReasoningStarted, EventID: eventID, AgentName: agentName}); err != nil {
			return err
		}
		p.reasoningActive = true
	}
	p.reasoningBuffer.WriteString(delta)
	if p.reasoningBuffer.Len() < reasoningDeltaChunkBytes {
		return nil
	}
	return p.flushReasoning(eventID, agentName)
}

func (p *llmEventProjector) flushReasoning(eventID, agentName string) error {
	if p.reasoningBuffer.Len() == 0 {
		return nil
	}
	delta := p.reasoningBuffer.String()
	p.reasoningBuffer.Reset()
	return p.emit(LLMTurnEvent{Type: LLMEventReasoningDelta, EventID: eventID, AgentName: agentName, Text: delta})
}

func (p *llmEventProjector) completeReasoning(eventID, agentName string) error {
	if !p.reasoningActive {
		return nil
	}
	if err := p.flushReasoning(eventID, agentName); err != nil {
		return err
	}
	p.reasoningActive = false
	return p.emit(LLMTurnEvent{Type: LLMEventReasoningCompleted, EventID: eventID, AgentName: agentName})
}

func (e *LLMTurnExecutor) ensureSession(ctx context.Context, request LLMTurnRequest) error {
	response, err := e.sessions.Get(ctx, &session.GetRequest{
		AppName: appName, UserID: request.UserID, SessionID: request.SessionID,
	})
	if err == nil {
		return validateSessionIdentity(response.Session, request)
	}
	if !errors.Is(err, appharness.ErrSessionNotFound) {
		return fmt.Errorf("load agent session: %w", err)
	}
	_, err = e.sessions.Create(ctx, &session.CreateRequest{
		AppName: appName, UserID: request.UserID, SessionID: request.SessionID,
		State: map[string]any{
			"warmmo:agent_id":           request.AgentID,
			"warmmo:definition_version": request.DefinitionVersion,
			"warmmo:definition_hash":    request.DefinitionHash,
			"warmmo:prompt_hash":        request.PromptHash,
			"warmmo:toolset_hash":       request.ToolsetHash,
		},
	})
	if err != nil {
		response, loadErr := e.sessions.Get(ctx, &session.GetRequest{
			AppName: appName, UserID: request.UserID, SessionID: request.SessionID,
		})
		if loadErr == nil {
			return validateSessionIdentity(response.Session, request)
		}
		return errors.Join(fmt.Errorf("create agent session: %w", err), fmt.Errorf("reload agent session: %w", loadErr))
	}
	return nil
}

func (e *LLMTurnExecutor) finish(
	ctx context.Context,
	request LLMTurnRequest,
	outcome LLMTurnOutcome,
	lastEventID string,
	runErr error,
) (LLMTurnOutcome, error) {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	checkpoint := appharness.Checkpoint{
		RunID: request.RunID, TurnID: request.TurnID, SessionID: request.SessionID, AgentID: request.AgentID,
		DefinitionVersion: request.DefinitionVersion, DefinitionHash: request.DefinitionHash,
		PromptHash: request.PromptHash, ToolsetHash: request.ToolsetHash,
		Status: outcome.Status, StopReason: outcome.StopReason, Final: outcome.Final, Pending: outcome.Pending,
		Artifact: outcome.Artifact,
		Usage:    outcome.Usage, Budget: outcome.Budget, LastCanonicalEventID: lastEventID,
		Snapshot: snapshotFromRequest(request),
	}
	stored, err := e.checkpoints.GetCheckpoint(persistCtx, request.TurnID)
	if err == nil {
		checkpoint.Version = stored.Version
		checkpoint.ChildRunIDs = stored.ChildRunIDs
		checkpoint.CompactionManifest = stored.CompactionManifest
	} else if !errors.Is(err, appharness.ErrCheckpointNotFound) {
		return outcome, errors.Join(runErr, fmt.Errorf("load turn checkpoint: %w", err))
	}
	if _, err := e.checkpoints.SaveCheckpoint(persistCtx, checkpoint); err != nil {
		return outcome, errors.Join(runErr, fmt.Errorf("save turn checkpoint: %w", err))
	}
	return outcome, runErr
}

func (e *LLMTurnExecutor) completedOutcome(
	ctx context.Context,
	request LLMTurnRequest,
) (LLMTurnOutcome, bool, error) {
	checkpoint, err := e.checkpoints.GetCheckpoint(ctx, request.TurnID)
	if errors.Is(err, appharness.ErrCheckpointNotFound) {
		return LLMTurnOutcome{}, false, nil
	}
	if err != nil {
		return LLMTurnOutcome{}, false, fmt.Errorf("load existing turn checkpoint: %w", err)
	}
	if err := validateCheckpointIdentity(checkpoint, request); err != nil {
		return LLMTurnOutcome{}, false, err
	}
	if checkpoint.Status == appharness.TurnRunning {
		return LLMTurnOutcome{}, false, fmt.Errorf("%w: %s", appharness.ErrTurnRecoveryRequired, request.TurnID)
	}
	switch checkpoint.Status {
	case appharness.TurnCompleted, appharness.TurnAwaitingUser, appharness.TurnAwaitingChild,
		appharness.TurnStoppedBudget, appharness.TurnStoppedNoProgress, appharness.TurnCancelled, appharness.TurnFailed:
	default:
		return LLMTurnOutcome{}, false, fmt.Errorf("turn checkpoint %s has invalid status %q", request.TurnID, checkpoint.Status)
	}
	return LLMTurnOutcome{
		Status: checkpoint.Status, StopReason: checkpoint.StopReason, SessionID: checkpoint.SessionID,
		Final: checkpoint.Final, Pending: checkpoint.Pending, Artifact: checkpoint.Artifact,
		Usage: checkpoint.Usage, Budget: checkpoint.Budget,
	}, true, nil
}

func (e *LLMTurnExecutor) resumeCheckpoint(ctx context.Context, request LLMTurnRequest) (appharness.Checkpoint, error) {
	checkpoint, err := e.checkpoints.GetCheckpoint(ctx, request.TurnID)
	if err != nil {
		return appharness.Checkpoint{}, fmt.Errorf("load paused turn checkpoint: %w", err)
	}
	if err := validateCheckpointIdentity(checkpoint, request); err != nil {
		return appharness.Checkpoint{}, err
	}
	if (checkpoint.Status != appharness.TurnAwaitingUser && checkpoint.Status != appharness.TurnAwaitingChild) || checkpoint.Pending == nil {
		return appharness.Checkpoint{}, fmt.Errorf("turn %s is not awaiting a control response", request.TurnID)
	}
	if request.Resume == nil || request.Resume.ToolCallID == "" || request.Resume.ToolName == "" || request.Resume.Response == nil {
		return appharness.Checkpoint{}, errors.New("resume function response is incomplete")
	}
	if checkpoint.Pending.ToolCallID != request.Resume.ToolCallID || checkpoint.Pending.ToolName != request.Resume.ToolName {
		return appharness.Checkpoint{}, fmt.Errorf("resume function response does not match pending call %s", checkpoint.Pending.ToolCallID)
	}
	return checkpoint, nil
}

func validateCheckpointIdentity(checkpoint appharness.Checkpoint, request LLMTurnRequest) error {
	if checkpoint.RunID != request.RunID || checkpoint.SessionID != request.SessionID || checkpoint.AgentID != request.AgentID ||
		checkpoint.DefinitionVersion != request.DefinitionVersion || checkpoint.DefinitionHash != request.DefinitionHash ||
		checkpoint.PromptHash != request.PromptHash || checkpoint.ToolsetHash != request.ToolsetHash {
		return fmt.Errorf("turn checkpoint %s does not match the requested turn identity", request.TurnID)
	}
	return nil
}

func (e *LLMTurnExecutor) Resume(ctx context.Context, runID, answer string, emit LLMTurnEmitter) (LLMTurnOutcome, error) {
	checkpoint, err := e.checkpoints.FindPendingCheckpoint(ctx, runID)
	if err != nil {
		return LLMTurnOutcome{}, err
	}
	if checkpoint.Status != appharness.TurnAwaitingUser || checkpoint.Pending == nil || checkpoint.Snapshot == nil {
		return LLMTurnOutcome{}, fmt.Errorf("run %s has no resumable user-input checkpoint", runID)
	}
	return e.resumeControl(ctx, checkpoint, appharness.ResumeInput{
		ToolCallID: checkpoint.Pending.ToolCallID, ToolName: checkpoint.Pending.ToolName,
		Response: askUserResponse(answer),
	}, emit)
}

func (e *LLMTurnExecutor) Continue(
	ctx context.Context,
	turnID string,
	response map[string]any,
	emit LLMTurnEmitter,
) (LLMTurnOutcome, error) {
	checkpoint, err := e.checkpoints.GetCheckpoint(ctx, turnID)
	if err != nil {
		return LLMTurnOutcome{}, err
	}
	if checkpoint.Status != appharness.TurnAwaitingChild || checkpoint.Pending == nil || checkpoint.Snapshot == nil {
		return LLMTurnOutcome{}, fmt.Errorf("turn %s has no resumable child checkpoint", turnID)
	}
	return e.resumeControl(ctx, checkpoint, appharness.ResumeInput{
		ToolCallID: checkpoint.Pending.ToolCallID, ToolName: checkpoint.Pending.ToolName, Response: response,
	}, emit)
}

func (e *LLMTurnExecutor) resumeControl(
	ctx context.Context,
	checkpoint appharness.Checkpoint,
	resume appharness.ResumeInput,
	emit LLMTurnEmitter,
) (LLMTurnOutcome, error) {
	snapshot := checkpoint.Snapshot
	request := LLMTurnRequest{
		RunID: checkpoint.RunID, TurnID: checkpoint.TurnID, AgentID: checkpoint.AgentID,
		AgentName: snapshot.AgentName, Description: snapshot.Description, Instruction: snapshot.Instruction,
		DefinitionVersion: checkpoint.DefinitionVersion, DefinitionHash: checkpoint.DefinitionHash,
		PromptHash: checkpoint.PromptHash, ToolsetHash: checkpoint.ToolsetHash,
		ProviderID: snapshot.ProviderID, ModelID: snapshot.ModelID, ConversationSessionID: snapshot.ConversationSessionID, UserID: snapshot.UserID,
		SessionID: checkpoint.SessionID, Prompt: snapshot.Prompt,
		ConversationUserContent: snapshot.ConversationUserContent, PublishConversation: snapshot.PublishConversation,
		AllowedTools:    append([]string(nil), snapshot.AllowedTools...),
		ControlTools:    append([]string(nil), snapshot.ControlTools...),
		AllowedChildren: append([]appharness.ChildContract(nil), snapshot.AllowedChildren...),
		ToolInvocation: agentcore.ToolInvocation{
			RunID: checkpoint.RunID, TurnID: checkpoint.TurnID, WorkID: snapshot.WorkID,
			SkillID: snapshot.SkillID, SkillVersion: snapshot.SkillVersion,
		},
		Budget: snapshot.Budget, Context: snapshot.Context, Memory: snapshot.Memory, Output: snapshot.Output,
		Resume: &resume,
	}
	return e.Run(ctx, request, emit)
}

func validateSessionIdentity(current session.Session, request LLMTurnRequest) error {
	if current == nil {
		return errors.New("session service returned an empty session")
	}
	expected := map[string]string{
		"warmmo:agent_id":           request.AgentID,
		"warmmo:definition_version": request.DefinitionVersion,
		"warmmo:definition_hash":    request.DefinitionHash,
		"warmmo:prompt_hash":        request.PromptHash,
		"warmmo:toolset_hash":       request.ToolsetHash,
	}
	for key, expectedValue := range expected {
		actual, err := current.State().Get(key)
		if err != nil {
			return fmt.Errorf("agent session %s is missing frozen state %q: %w", current.ID(), key, err)
		}
		actualValue, ok := actual.(string)
		if !ok || actualValue != expectedValue {
			return fmt.Errorf("agent session %s frozen state %q does not match the requested definition", current.ID(), key)
		}
	}
	return nil
}

func isContextCancellation(ctx context.Context, err error) bool {
	return ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

func isBudgetDeadline(parent, run context.Context, err error) bool {
	return parent.Err() == nil && run.Err() != nil && errors.Is(err, context.DeadlineExceeded)
}

func eventNeedsControl(event *session.Event) bool {
	return len(event.LongRunningToolIDs) > 0 || len(event.Actions.RequestedToolConfirmations) > 0 || event.Actions.SkipSummarization
}

func eventStoppedForBudget(event *session.Event) bool {
	if event == nil || event.Actions.StateDelta == nil {
		return false
	}
	value, ok := event.Actions.StateDelta[stopReasonStateKey].(string)
	return ok && value == string(appharness.StopBudgetExceeded)
}

func eventStoppedForExecution(event *session.Event) bool {
	if event == nil || event.Actions.StateDelta == nil {
		return false
	}
	value, ok := event.Actions.StateDelta[stopReasonStateKey].(string)
	return ok && value == string(appharness.StopExecutionFailed)
}

func pendingAction(event *session.Event) *appharness.PendingAction {
	callIDs := append([]string(nil), event.LongRunningToolIDs...)
	for callID := range event.Actions.RequestedToolConfirmations {
		callIDs = append(callIDs, callID)
	}
	sort.Strings(callIDs)
	action := &appharness.PendingAction{Kind: "user_input"}
	if len(callIDs) > 0 {
		action.ToolCallID = callIDs[0]
	}
	if confirmation, ok := event.Actions.RequestedToolConfirmations[action.ToolCallID]; ok {
		payload, err := json.Marshal(map[string]any{"hint": confirmation.Hint, "payload": confirmation.Payload})
		if err == nil {
			action.Payload = payload
		}
	}
	if event.Content != nil {
		for _, part := range event.Content.Parts {
			if part == nil {
				continue
			}
			if part.FunctionCall != nil && (action.ToolCallID == "" || part.FunctionCall.ID == action.ToolCallID) {
				action.ToolName = part.FunctionCall.Name
				if action.ToolName == DelegateAgentToolName {
					action.Kind = "child"
				}
				if payload, err := json.Marshal(part.FunctionCall.Args); err == nil {
					action.Payload = payload
				}
				break
			}
			if part.FunctionResponse != nil && (action.ToolCallID == "" || part.FunctionResponse.ID == action.ToolCallID) {
				action.ToolName = part.FunctionResponse.Name
				break
			}
		}
	}
	if action.ToolName == DelegateAgentToolName {
		action.Kind = "child"
	}
	return action
}

func emitTerminalOutcome(emit LLMTurnEmitter, eventID, agentName string, outcome LLMTurnOutcome) error {
	eventType := LLMEventTurnCompleted
	if outcome.Status == appharness.TurnAwaitingUser || outcome.Status == appharness.TurnAwaitingChild {
		eventType = LLMEventTurnPaused
	} else if outcome.Status == appharness.TurnStoppedBudget || outcome.Status == appharness.TurnStoppedNoProgress {
		eventType = LLMEventTurnStopped
	} else if outcome.Status == appharness.TurnCancelled {
		eventType = LLMEventTurnCancelled
	}
	text := ""
	if outcome.Final != nil {
		text = outcome.Final.Content
	}
	toolName, toolCallID, summary := "", "", ""
	var payload json.RawMessage
	if outcome.Pending != nil {
		toolName = outcome.Pending.ToolName
		toolCallID = outcome.Pending.ToolCallID
		summary = pendingQuestion(outcome.Pending.Payload)
		payload = append(json.RawMessage(nil), outcome.Pending.Payload...)
	}
	return emit(LLMTurnEvent{
		Type: eventType, EventID: eventID, AgentName: agentName, Text: text,
		ToolName: toolName, ToolCallID: toolCallID, Summary: summary,
		Payload: payload,
		Usage:   outcome.Usage, Budget: outcome.Budget,
	})
}

func (e *LLMTurnExecutor) finishExpected(
	ctx context.Context,
	request LLMTurnRequest,
	outcome LLMTurnOutcome,
	lastEventID string,
	emit LLMTurnEmitter,
	policy *turnPolicy,
) (LLMTurnOutcome, error) {
	outcome, err := e.finish(ctx, request, outcome, lastEventID, nil)
	if err != nil {
		return outcome, err
	}
	if err := emitTerminalOutcome(emit, lastEventID, request.AgentName, outcome); err != nil {
		return outcome, fmt.Errorf("%w: %v", appharness.ErrProjectionFailed, err)
	}
	if err := policy.ProjectionError(); err != nil {
		return outcome, fmt.Errorf("%w: %v", appharness.ErrProjectionFailed, err)
	}
	return outcome, nil
}

func projectToolResponse(event *session.Event, response *genai.FunctionResponse) LLMTurnEvent {
	projected := LLMTurnEvent{
		Type: LLMEventToolCompleted, EventID: event.ID, AgentName: event.Author,
		ToolName: response.Name, ToolCallID: response.ID,
	}
	metadata, _ := response.Response[toolMetadataKey].(map[string]any)
	if metadata != nil {
		projected.Summary, _ = metadata["summary"].(string)
		projected.ResultBytes = numberAsInt(metadata["resultBytes"])
		projected.Truncated, _ = metadata["truncated"].(bool)
	}
	errorValue, failed := response.Response["error"]
	if !failed {
		return projected
	}
	projected.Type = LLMEventToolFailed
	switch value := errorValue.(type) {
	case map[string]any:
		projected.ErrorCode, _ = value["code"].(string)
		projected.Retryable, _ = value["retryable"].(bool)
		if projected.Summary == "" {
			projected.Summary, _ = value["message"].(string)
		}
	case string:
		projected.ErrorCode = string(agentcore.ToolErrorInternal)
		projected.Summary = value
	}
	return projected
}

func numberAsInt(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	default:
		return 0
	}
}

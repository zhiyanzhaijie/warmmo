package adk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"warmmo/core/internal/adapter/agent/provider"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	adktool "google.golang.org/adk/tool"
	"google.golang.org/genai"

	appharness "warmmo/core/internal/application/harness"
)

type LLMTurnStatus = appharness.TurnStatus

const appName = "warmmo"

const (
	LLMTurnCompleted LLMTurnStatus = appharness.TurnCompleted
	LLMTurnPaused    LLMTurnStatus = appharness.TurnAwaitingUser
)

type LLMTurnEventType = appharness.RuntimeEventType

const (
	LLMEventMessageDelta       = appharness.RuntimeEventMessageDelta
	LLMEventReasoningStarted   = appharness.RuntimeEventReasoningStarted
	LLMEventReasoningDelta     = appharness.RuntimeEventReasoningDelta
	LLMEventReasoningCompleted = appharness.RuntimeEventReasoningCompleted
	LLMEventToolRequested      = appharness.RuntimeEventToolRequested
	LLMEventToolStarted        = appharness.RuntimeEventToolStarted
	LLMEventToolCompleted      = appharness.RuntimeEventToolCompleted
	LLMEventToolFailed         = appharness.RuntimeEventToolFailed
	LLMEventMemoryFailed       = appharness.RuntimeEventMemoryFailed
	LLMEventConversationFailed = appharness.RuntimeEventConversationFailed
	LLMEventTurnCompleted      = appharness.RuntimeEventCompleted
	LLMEventTurnPaused         = appharness.RuntimeEventPaused
	LLMEventTurnStopped        = appharness.RuntimeEventStopped
	LLMEventTurnCancelled      = appharness.RuntimeEventCancelled
)

type (
	LLMTurnRequest      = appharness.RuntimeRequest
	LLMTurnEvent        = appharness.RuntimeEvent
	LLMTurnOutcome      = appharness.TurnOutcome
	LLMTurnEmitter      = appharness.RuntimeEmitter
	DynamicToolProvider = appharness.DynamicToolProvider
)

type ModelResolver func(context.Context, string, string) (model.LLM, error)

type TurnConsumer func(context.Context, LLMTurnRequest, LLMTurnOutcome, LLMTurnEmitter)

type LLMTurnExecutor struct {
	tools        appharness.ToolCatalog
	resolve      ModelResolver
	sessions     session.Service
	checkpoints  appharness.CheckpointStore
	toolCalls    appharness.ToolCallStore
	memories     appharness.MemoryStore
	conversation appharness.ConversationStore
	context      appharness.ContextProvider
	dynamicTools DynamicToolProvider
	consume      TurnConsumer
}

var _ appharness.AgentRuntime = (*LLMTurnExecutor)(nil)

type LLMTurnDependencies struct {
	Tools          appharness.ToolCatalog
	Resolve        ModelResolver
	Sessions       session.Service
	Checkpoints    appharness.CheckpointStore
	ToolCalls      appharness.ToolCallStore
	Memories       appharness.MemoryStore
	Conversation   appharness.ConversationStore
	AmbientContext appharness.ContextProvider
	DynamicTools   DynamicToolProvider
	Consume        TurnConsumer
}

func NewLLMTurnExecutor(dependencies LLMTurnDependencies) *LLMTurnExecutor {
	return &LLMTurnExecutor{
		tools: dependencies.Tools, resolve: dependencies.Resolve, sessions: dependencies.Sessions,
		checkpoints: dependencies.Checkpoints,
		toolCalls:   dependencies.ToolCalls, memories: dependencies.Memories,
		conversation: dependencies.Conversation, context: dependencies.AmbientContext,
		dynamicTools: dependencies.DynamicTools,
		consume:      dependencies.Consume,
	}
}

func (e *LLMTurnExecutor) Run(ctx context.Context, request LLMTurnRequest, emit LLMTurnEmitter) (LLMTurnOutcome, error) {
	if e == nil {
		return LLMTurnOutcome{}, errors.New("LLM turn executor is not configured")
	}
	additionalTools, err := e.additionalTools(request, emit)
	if err != nil {
		return LLMTurnOutcome{}, err
	}
	prepared, err := e.prepareRequest(request, additionalTools)
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
			e.consumeOutcome(ctx, request, outcome, emit)
			return outcome, nil
		}
	}
	checkpointVersion := int64(0)
	initialBudget := appharness.BudgetUsage{}
	initialUsage := appharness.Usage{}
	var checkpointMu sync.Mutex
	if prior != nil {
		checkpointVersion = prior.Version
		initialBudget = prior.Budget
		initialUsage = prior.Usage
	}
	saveRunningCheckpoint := func(saveCtx context.Context, usage appharness.BudgetUsage) error {
		checkpointMu.Lock()
		defer checkpointMu.Unlock()
		checkpoint := appharness.Checkpoint{
			RunID: request.RunID, TurnID: request.TurnID, SessionID: request.SessionID, AgentID: request.AgentID,
			DefinitionVersion: request.DefinitionVersion, DefinitionHash: request.DefinitionHash,
			PromptHash: request.PromptHash, ToolsetHash: request.ToolsetHash,
			Status: appharness.TurnRunning, Budget: usage, Version: checkpointVersion,
			Snapshot: snapshotFromRequest(request), Usage: initialUsage,
		}
		stored, err := e.checkpoints.SaveCheckpoint(saveCtx, checkpoint)
		if err != nil {
			return err
		}
		checkpointVersion = stored.Version
		return nil
	}
	reserveBudget := func(reserveCtx context.Context, usage appharness.BudgetUsage) error {
		return saveRunningCheckpoint(reserveCtx, usage)
	}
	budget, err := newTurnBudget(turnBudgetConfig{
		Budget: request.Budget, Initial: initialBudget, Reserve: reserveBudget,
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
	runCtx, cancel := context.WithTimeout(ctx, budget.budget.MaxDuration)
	defer cancel()
	if err := e.ensureSession(runCtx, request); err != nil {
		return LLMTurnOutcome{}, err
	}
	llm, err := e.resolve(runCtx, request.ProviderID, request.ModelID)
	if err != nil {
		return LLMTurnOutcome{}, fmt.Errorf("resolve model: %w", err)
	}
	events := &eventSink{emit: emit}
	codec := toolResultCodec{maxBytes: request.Context.MaxToolResultBytes}
	tools, err := adaptTools(e.tools, request.AllowedTools, request.ToolInvocation, budget, codec, events, e.toolCalls, additionalTools...)
	if err != nil {
		return LLMTurnOutcome{}, err
	}
	requestContext := requestContext{
		policy: request.Context, memory: request.Memory, memories: e.memories,
		conversation: e.conversation, context: e.context,
		workID: request.ToolInvocation.WorkID, query: recallQuery(request),
		conversationSessionID: request.ConversationSessionID,
	}
	responseSchema, err := genAISchema(request.ResponseSchema)
	if err != nil {
		return LLMTurnOutcome{}, fmt.Errorf("build response schema: %w", err)
	}
	configuredAgent, err := llmagent.New(llmagent.Config{
		Name: request.AgentName, Description: request.Description,
		Instruction: request.Instruction, Model: llm, Tools: tools,
		OutputSchema:         responseSchema,
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{budget.beforeModel, requestContext.beforeModel},
		OnToolErrorCallbacks: []llmagent.OnToolErrorCallback{recoverInvalidToolArguments},
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
	outcome := LLMTurnOutcome{
		TurnID: request.TurnID, SessionID: request.SessionID, Usage: initialUsage,
	}
	lastEventID := ""
	projector := newLLMEventProjector(events.project)
	var terminalErr error

repairLoop:
	for {
		finalSeen := false
		outcome.Final = nil
		outcome.Status = ""
		outcome.StopReason = ""
	runLoop:
		for event, runErr := range adkRunner.Run(runCtx, request.UserID, request.SessionID, message, agent.RunConfig{
			StreamingMode: agent.StreamingModeSSE,
		}) {
			if runErr != nil {
				if err := projector.completeReasoning(lastEventID, request.AgentName); err != nil {
					return e.finish(ctx, request, outcome, lastEventID, fmt.Errorf("complete reasoning phase: %w", err))
				}
				outcome.Budget = budget.Usage()
				if errors.Is(runErr, appharness.ErrBudgetExceeded) || isBudgetDeadline(ctx, runCtx, runErr) {
					outcome.Status = appharness.TurnStoppedBudget
					outcome.StopReason = appharness.StopBudgetExceeded
					outcome.BudgetDimension = budgetDimension(runErr, runCtx)
					return e.finishExpected(ctx, request, outcome, lastEventID, emit, events)
				}
				if isContextCancellation(ctx, runErr) {
					outcome.Status = appharness.TurnCancelled
					outcome.StopReason = appharness.StopContextCancelled
					return e.finishExpected(ctx, request, outcome, lastEventID, emit, events)
				}
				if len(request.ResponseSchema) > 0 && errors.Is(runErr, provider.ErrInvalidStructuredOutput) {
					message = structuredOutputCorrection(runErr)
					continue repairLoop
				}
				outcome.Status = appharness.TurnFailed
				outcome.StopReason = appharness.StopExecutionFailed
				return e.finish(ctx, request, outcome, lastEventID, fmt.Errorf("run LLM agent: %w", runErr))
			}
			if event == nil {
				continue
			}
			if err := projector.project(event, &outcome); err != nil {
				outcome.Budget = budget.Usage()
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
			if toolResults, resultErr := completedToolResults(event); resultErr != nil {
				return e.finish(ctx, request, outcome, lastEventID, resultErr)
			} else if len(toolResults) > 0 {
				outcome.ToolResults = append(outcome.ToolResults, toolResults...)
			}
			if event.IsFinalResponse() {
				finalSeen = true
				if eventStoppedForBudget(event) {
					outcome.Status = appharness.TurnStoppedBudget
					outcome.StopReason = appharness.StopBudgetExceeded
					outcome.BudgetDimension = eventBudgetDimension(event)
					outcome.Pending = nil
				} else if eventStoppedForExecution(event) {
					outcome.Status = appharness.TurnFailed
					outcome.StopReason = appharness.StopExecutionFailed
					outcome.Pending = nil
					terminalErr = budget.ExecutionError()
				} else if eventNeedsControl(event) {
					if err := budget.reserveControlTool(runCtx); err != nil {
						if errors.Is(err, appharness.ErrBudgetExceeded) {
							outcome.Status = appharness.TurnStoppedBudget
							outcome.StopReason = appharness.StopBudgetExceeded
							outcome.BudgetDimension = "control_tools"
							return e.finishExpected(ctx, request, outcome, lastEventID, emit, events)
						}
						outcome.Status = appharness.TurnFailed
						outcome.StopReason = appharness.StopExecutionFailed
						return e.finish(ctx, request, outcome, lastEventID, err)
					}
					outcome.Pending = pendingAction(event)
					outcome.Status = appharness.TurnAwaitingUser
					outcome.StopReason = appharness.StopUserInputRequired
				} else {
					outcome.Status = appharness.TurnCompleted
					outcome.StopReason = appharness.StopFinalResponse
				}
			}
			if outcome.Status == appharness.TurnAwaitingUser {
				break runLoop
			}
		}
		if !finalSeen {
			if err := projector.completeReasoning(lastEventID, request.AgentName); err != nil {
				return e.finish(ctx, request, outcome, lastEventID, fmt.Errorf("complete reasoning phase: %w", err))
			}
			outcome.Budget = budget.Usage()
			if runCtx.Err() != nil && ctx.Err() == nil {
				outcome.Status = appharness.TurnStoppedBudget
				outcome.StopReason = appharness.StopBudgetExceeded
				outcome.BudgetDimension = "duration"
				return e.finishExpected(ctx, request, outcome, lastEventID, emit, events)
			}
			if ctx.Err() != nil {
				outcome.Status = appharness.TurnCancelled
				outcome.StopReason = appharness.StopContextCancelled
				return e.finishExpected(ctx, request, outcome, lastEventID, emit, events)
			}
			outcome.Status = appharness.TurnFailed
			outcome.StopReason = appharness.StopExecutionFailed
			return e.finish(ctx, request, outcome, lastEventID, errors.New("LLM agent completed without a final event"))
		}
		if len(request.ResponseSchema) > 0 && outcome.Status == appharness.TurnCompleted {
			output, outputErr := validateStructuredOutput(request.ResponseSchema, outcome.Final)
			if outputErr != nil {
				message = structuredOutputCorrection(outputErr)
				continue repairLoop
			}
			outcome.Output = output
		}
		break
	}
	outcome.Budget = budget.Usage()
	outcome, err = e.finish(ctx, request, outcome, lastEventID, terminalErr)
	if err != nil {
		return outcome, err
	}
	e.consumeOutcome(ctx, request, outcome, emit)
	if err := emitTerminalOutcome(emit, lastEventID, request.AgentName, outcome); err != nil {
		return outcome, fmt.Errorf("%w: %v", appharness.ErrProjectionFailed, err)
	}
	if err := events.Error(); err != nil {
		return outcome, fmt.Errorf("%w: %v", appharness.ErrProjectionFailed, err)
	}
	return outcome, nil
}

func (e *LLMTurnExecutor) consumeOutcome(ctx context.Context, request LLMTurnRequest, outcome LLMTurnOutcome, emit LLMTurnEmitter) {
	if e != nil && e.consume != nil {
		e.consume(ctx, request, outcome, emit)
	}
}

func recoverInvalidToolArguments(
	_ agent.ToolContext,
	_ adktool.Tool,
	args map[string]any,
	err error,
) (map[string]any, error) {
	message, malformed := args[provider.MalformedArgumentsKey].(string)
	if !malformed {
		if !strings.HasPrefix(err.Error(), "validating root:") {
			return nil, err
		}
		message = err.Error()
	}
	return map[string]any{
		"error": map[string]any{
			"code":      string(appharness.ToolErrorInvalidArgument),
			"message":   message,
			"retryable": true,
		},
	}, nil
}

func (e *LLMTurnExecutor) validate(request LLMTurnRequest, emit LLMTurnEmitter) error {
	if e == nil || e.resolve == nil || e.sessions == nil || e.checkpoints == nil {
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
	if request.Context.MaxToolResultBytes < 1024 || request.Context.ReservedOutputTokens < 1024 {
		return errors.New("invalid context policy")
	}
	if request.Memory.Recall != (request.Context.Memory == "recall") {
		return errors.New("context and memory recall policies do not match")
	}
	return nil
}

func (e *LLMTurnExecutor) prepareRequest(request LLMTurnRequest, additionalTools []appharness.Tool) (LLMTurnRequest, error) {
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
		specs := make([]appharness.ToolSpec, 0, len(snapshot)+1)
		for _, tool := range snapshot {
			specs = append(specs, tool.Spec())
		}
		for _, tool := range additionalTools {
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

func (e *LLMTurnExecutor) additionalTools(request LLMTurnRequest, emit LLMTurnEmitter) ([]appharness.Tool, error) {
	tools := make([]appharness.Tool, 0, len(request.ControlTools)+1)
	for _, name := range request.ControlTools {
		switch name {
		case AskUserToolName:
			tools = append(tools, askUserTool{})
		default:
			return nil, fmt.Errorf("unsupported control tool %q", name)
		}
	}
	if e.dynamicTools != nil {
		dynamic, err := e.dynamicTools(request, emit)
		if err != nil {
			return nil, err
		}
		tools = append(tools, dynamic...)
	}
	return tools, nil
}

func completedToolResults(event *session.Event) ([]appharness.ToolResult, error) {
	if event == nil || event.Content == nil {
		return nil, nil
	}
	results := make([]appharness.ToolResult, 0)
	for _, part := range event.Content.Parts {
		if part == nil || part.FunctionResponse == nil {
			continue
		}
		output, err := json.Marshal(part.FunctionResponse.Response)
		if err != nil {
			return nil, fmt.Errorf("encode tool result %q: %w", part.FunctionResponse.Name, err)
		}
		results = append(results, appharness.ToolResult{
			CallID: part.FunctionResponse.ID, Name: part.FunctionResponse.Name, Output: output,
		})
	}
	return results, nil
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
		Extension: append(json.RawMessage(nil), request.Extension...),
		WorkID:    request.ToolInvocation.WorkID, SkillID: request.ToolInvocation.SkillID,
		SkillVersion: request.ToolInvocation.SkillVersion, Budget: request.Budget, ResponseSchema: cloneMap(request.ResponseSchema),
		Context: request.Context, Memory: request.Memory,
	}
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
		Output: append(json.RawMessage(nil), outcome.Output...), Usage: outcome.Usage, Budget: outcome.Budget,
		Snapshot: snapshotFromRequest(request),
	}
	stored, err := e.checkpoints.GetCheckpoint(persistCtx, request.TurnID)
	if err == nil {
		checkpoint.Version = stored.Version
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
	case appharness.TurnCompleted, appharness.TurnAwaitingUser,
		appharness.TurnStoppedBudget, appharness.TurnStoppedNoProgress, appharness.TurnCancelled, appharness.TurnFailed:
	default:
		return LLMTurnOutcome{}, false, fmt.Errorf("turn checkpoint %s has invalid status %q", request.TurnID, checkpoint.Status)
	}
	return LLMTurnOutcome{
		TurnID: checkpoint.TurnID,
		Status: checkpoint.Status, StopReason: checkpoint.StopReason, SessionID: checkpoint.SessionID,
		Final: checkpoint.Final, Pending: checkpoint.Pending, Output: append(json.RawMessage(nil), checkpoint.Output...),
		Usage: checkpoint.Usage, Budget: checkpoint.Budget,
	}, true, nil
}

// Restore reconstructs transient tool results from ADK's canonical session
// events. Checkpoints retain turn state only and do not duplicate the event log.
func (e *LLMTurnExecutor) Restore(ctx context.Context, checkpoint appharness.Checkpoint) (LLMTurnOutcome, error) {
	if e == nil || e.sessions == nil {
		return LLMTurnOutcome{}, errors.New("agent session service is not configured")
	}
	if checkpoint.Snapshot == nil {
		return LLMTurnOutcome{}, errors.New("turn checkpoint has no invocation snapshot")
	}
	loaded, err := e.sessions.Get(ctx, &session.GetRequest{
		AppName: appName, UserID: checkpoint.Snapshot.UserID, SessionID: checkpoint.SessionID,
	})
	if err != nil {
		return LLMTurnOutcome{}, fmt.Errorf("load canonical agent session: %w", err)
	}
	outcome := LLMTurnOutcome{
		TurnID: checkpoint.TurnID, SessionID: checkpoint.SessionID,
		Status: checkpoint.Status, StopReason: checkpoint.StopReason,
		Final: checkpoint.Final, Pending: checkpoint.Pending,
		Output: append(json.RawMessage(nil), checkpoint.Output...),
		Usage:  checkpoint.Usage, Budget: checkpoint.Budget,
	}
	for event := range loaded.Session.Events().All() {
		results, resultErr := completedToolResults(event)
		if resultErr != nil {
			return LLMTurnOutcome{}, resultErr
		}
		outcome.ToolResults = append(outcome.ToolResults, results...)
	}
	return outcome, nil
}

func (e *LLMTurnExecutor) resumeCheckpoint(ctx context.Context, request LLMTurnRequest) (appharness.Checkpoint, error) {
	checkpoint, err := e.checkpoints.GetCheckpoint(ctx, request.TurnID)
	if err != nil {
		return appharness.Checkpoint{}, fmt.Errorf("load paused turn checkpoint: %w", err)
	}
	if err := validateCheckpointIdentity(checkpoint, request); err != nil {
		return appharness.Checkpoint{}, err
	}
	if checkpoint.Status != appharness.TurnAwaitingUser || checkpoint.Pending == nil {
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
		AllowedTools: append([]string(nil), snapshot.AllowedTools...),
		ControlTools: append([]string(nil), snapshot.ControlTools...),
		Extension:    append(json.RawMessage(nil), snapshot.Extension...),
		ToolInvocation: appharness.ToolInvocation{
			RunID: checkpoint.RunID, TurnID: checkpoint.TurnID, WorkID: snapshot.WorkID,
			SkillID: snapshot.SkillID, SkillVersion: snapshot.SkillVersion,
		},
		Budget: snapshot.Budget, Context: snapshot.Context, Memory: snapshot.Memory, ResponseSchema: cloneMap(snapshot.ResponseSchema),
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
	return len(event.LongRunningToolIDs) > 0 || len(event.Actions.RequestedToolConfirmations) > 0
}

func eventStoppedForBudget(event *session.Event) bool {
	if event == nil || event.Actions.StateDelta == nil {
		return false
	}
	value, ok := event.Actions.StateDelta[stopReasonStateKey].(string)
	return ok && value == string(appharness.StopBudgetExceeded)
}

func eventBudgetDimension(event *session.Event) string {
	if event == nil || event.Actions.StateDelta == nil {
		return "unknown"
	}
	if value, ok := event.Actions.StateDelta[budgetDimensionStateKey].(string); ok && value != "" {
		return value
	}
	return "unknown"
}

func budgetDimension(err error, run context.Context) string {
	if run != nil && errors.Is(run.Err(), context.DeadlineExceeded) {
		return "duration"
	}
	message := strings.ToLower(err.Error())
	for phrase, dimension := range map[string]string{
		"model calls": "model_calls", "tool calls": "tool_calls", "side-effect calls": "side_effect_calls",
	} {
		if strings.Contains(message, phrase) {
			return dimension
		}
	}
	return "unknown"
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
	return action
}

func emitTerminalOutcome(emit LLMTurnEmitter, eventID, agentName string, outcome LLMTurnOutcome) error {
	eventType := LLMEventTurnCompleted
	if outcome.Status == appharness.TurnAwaitingUser {
		eventType = LLMEventTurnPaused
	} else if outcome.Status == appharness.TurnStoppedBudget || outcome.Status == appharness.TurnStoppedNoProgress {
		eventType = LLMEventTurnStopped
	} else if outcome.Status == appharness.TurnCancelled {
		eventType = LLMEventTurnCancelled
	} else if outcome.Status == appharness.TurnFailed {
		eventType = appharness.RuntimeEventFailed
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
	events *eventSink,
) (LLMTurnOutcome, error) {
	outcome, err := e.finish(ctx, request, outcome, lastEventID, nil)
	if err != nil {
		return outcome, err
	}
	if err := emitTerminalOutcome(emit, lastEventID, request.AgentName, outcome); err != nil {
		return outcome, fmt.Errorf("%w: %v", appharness.ErrProjectionFailed, err)
	}
	if err := events.Error(); err != nil {
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
		projected.ErrorCode = string(appharness.ToolErrorInternal)
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

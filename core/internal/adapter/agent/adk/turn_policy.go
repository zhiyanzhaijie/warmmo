package adk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"

	agentcore "warmmo/core/internal/adapter/agent/core"
	appharness "warmmo/core/internal/application/harness"
	"warmmo/core/internal/domain/canvas"
)

const (
	toolMetadataKey         = "_warmmo"
	stopReasonStateKey      = "warmmo:stop_reason"
	budgetDimensionStateKey = "warmmo:budget_dimension"
	defaultToolSummaryBytes = 4096
	toolResultArtifactKind  = "tool_result_v1"
)

type turnPolicy struct {
	budget             appharness.BudgetPolicy
	maxToolResultBytes int
	emit               LLMTurnEmitter
	reserve            func(context.Context, appharness.BudgetUsage) error
	artifacts          appharness.ArtifactStore
	runID              string
	turnID             string
	agentID            string

	mu            sync.Mutex
	usage         appharness.BudgetUsage
	projectionErr error
	executionErr  error
	emitMu        sync.Mutex
}

type turnPolicyConfig struct {
	Budget             appharness.BudgetPolicy
	MaxToolResultBytes int
	Initial            appharness.BudgetUsage
	Emit               LLMTurnEmitter
	Reserve            func(context.Context, appharness.BudgetUsage) error
	Artifacts          appharness.ArtifactStore
	RunID              string
	TurnID             string
	AgentID            string
}

func newTurnPolicy(config turnPolicyConfig) (*turnPolicy, error) {
	budget := config.Budget
	if budget == (appharness.BudgetPolicy{}) {
		budget = appharness.DefaultBudgetPolicy()
	}
	if budget.MaxModelCalls <= 0 || budget.MaxToolCalls < 0 || budget.MaxSideEffectCalls < 0 ||
		budget.MaxDuration <= 0 || budget.MaxToolResultBytes < 1024 {
		return nil, errors.New("invalid agent turn budget")
	}
	if config.Reserve == nil {
		return nil, errors.New("turn budget reservation store is required")
	}
	if config.Initial.ModelCalls < 0 || config.Initial.ToolCalls < 0 || config.Initial.SideEffectCalls < 0 ||
		config.Initial.ModelCalls > budget.MaxModelCalls || config.Initial.ToolCalls > budget.MaxToolCalls ||
		config.Initial.SideEffectCalls > budget.MaxSideEffectCalls {
		return nil, errors.New("invalid initial agent turn budget usage")
	}
	resultLimit := config.MaxToolResultBytes
	if resultLimit <= 0 || resultLimit > budget.MaxToolResultBytes {
		resultLimit = budget.MaxToolResultBytes
	}
	return &turnPolicy{
		budget: budget, maxToolResultBytes: resultLimit, usage: config.Initial,
		emit: config.Emit, reserve: config.Reserve, artifacts: config.Artifacts,
		runID: config.RunID, turnID: config.TurnID, agentID: config.AgentID,
	}, nil
}

func (p *turnPolicy) reserveControlTool(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.usage.ToolCalls >= p.budget.MaxToolCalls {
		return fmt.Errorf("%w: tool calls (%d)", appharness.ErrBudgetExceeded, p.budget.MaxToolCalls)
	}
	p.usage.ToolCalls++
	if err := p.reserve(ctx, p.usage); err != nil {
		p.usage.ToolCalls--
		return fmt.Errorf("persist control tool budget reservation: %w", err)
	}
	return nil
}

func (p *turnPolicy) beforeModel(ctx agent.CallbackContext, _ *model.LLMRequest) (*model.LLMResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.usage.ModelCalls >= p.budget.MaxModelCalls {
		return nil, fmt.Errorf("%w: model calls (%d)", appharness.ErrBudgetExceeded, p.budget.MaxModelCalls)
	}
	p.usage.ModelCalls++
	if err := p.reserve(ctx, p.usage); err != nil {
		p.usage.ModelCalls--
		return nil, fmt.Errorf("persist model budget reservation: %w", err)
	}
	return nil, nil
}

func (p *turnPolicy) beforeTool(ctx agent.ToolContext, spec agentcore.ToolSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	if p.usage.ToolCalls >= p.budget.MaxToolCalls {
		p.mu.Unlock()
		markBudgetStop(ctx, "tool_calls")
		return fmt.Errorf("%w: tool calls (%d)", appharness.ErrBudgetExceeded, p.budget.MaxToolCalls)
	}
	if spec.SideEffect.Mutates() && p.usage.SideEffectCalls >= p.budget.MaxSideEffectCalls {
		p.mu.Unlock()
		markBudgetStop(ctx, "side_effect_calls")
		return fmt.Errorf("%w: side-effect calls (%d)", appharness.ErrBudgetExceeded, p.budget.MaxSideEffectCalls)
	}
	p.usage.ToolCalls++
	if spec.SideEffect.Mutates() {
		p.usage.SideEffectCalls++
	}
	if err := p.reserve(ctx, p.usage); err != nil {
		p.usage.ToolCalls--
		if spec.SideEffect.Mutates() {
			p.usage.SideEffectCalls--
		}
		p.executionErr = fmt.Errorf("persist tool budget reservation: %w", err)
		p.mu.Unlock()
		markExecutionStop(ctx)
		return p.executionErr
	}
	p.mu.Unlock()

	_ = p.project(LLMTurnEvent{
		Type: LLMEventToolStarted, AgentName: ctx.AgentName(),
		ToolName: spec.Name, ToolCallID: ctx.FunctionCallID(),
	})
	return nil
}

func (p *turnPolicy) shapeResult(
	ctx agent.ToolContext,
	spec agentcore.ToolSpec,
	outputSchema *jsonschema.Resolved,
	value any,
) (map[string]any, error) {
	normalized, err := normalizeToolResult(value)
	if err != nil {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInternal, false, err)
	}
	if outputSchema != nil {
		if err := outputSchema.Validate(normalized); err != nil {
			return nil, agentcore.NewToolError(
				agentcore.ToolErrorInternal, false, fmt.Errorf("tool %q returned an invalid result: %w", spec.Name, err),
			)
		}
	}
	redacted, ok := redactToolValue(normalized).(map[string]any)
	if !ok {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInternal, false, errors.New("normalized tool result is not an object"))
	}
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInternal, false, fmt.Errorf("encode redacted tool result: %w", err))
	}
	limit := spec.MaxResultBytes
	if limit <= 0 || limit > p.maxToolResultBytes {
		limit = p.maxToolResultBytes
	}
	summary := truncateUTF8(string(encoded), min(defaultToolSummaryBytes, max(limit/4, 256)))
	metadata := map[string]any{
		"summary": summary, "resultBytes": len(encoded), "truncated": len(encoded) > limit,
	}
	if len(encoded) > limit {
		artifact, err := p.offloadToolResult(ctx, encoded)
		if err != nil {
			return nil, agentcore.NewToolError(agentcore.ToolErrorInternal, false, err)
		}
		metadata["artifact"] = artifact.Ref
		return map[string]any{
			"result": map[string]any{
				"truncated": true, "summary": summary, "originalBytes": len(encoded), "artifact": artifact.Ref,
			},
			toolMetadataKey: metadata,
		}, nil
	}
	redacted[toolMetadataKey] = metadata
	return redacted, nil
}

func (p *turnPolicy) offloadToolResult(ctx agent.ToolContext, payload json.RawMessage) (appharness.Artifact, error) {
	if p.artifacts == nil {
		return appharness.Artifact{}, errors.New("artifact store is required for oversized tool results")
	}
	callID := ctx.FunctionCallID()
	if strings.TrimSpace(callID) == "" {
		return appharness.Artifact{}, errors.New("tool call ID is required for result offload")
	}
	digest := sha256.Sum256([]byte(callID))
	artifactID := p.turnID + "-tool-" + hex.EncodeToString(digest[:8])
	artifact, err := p.artifacts.SaveArtifact(ctx, appharness.Artifact{
		Ref:   appharness.ArtifactRef{ID: artifactID, Kind: toolResultArtifactKind, SchemaVersion: "1"},
		RunID: p.runID, TurnID: p.turnID, AgentID: p.agentID, Payload: payload,
	})
	if err != nil {
		return appharness.Artifact{}, fmt.Errorf("offload oversized tool result: %w", err)
	}
	return artifact, nil
}

func (p *turnPolicy) errorResult(err error) map[string]any {
	code, retryable := classifyToolError(err)
	message := truncateUTF8(err.Error(), 2048)
	return map[string]any{
		"error": map[string]any{
			"code": string(code), "message": message, "retryable": retryable,
		},
		toolMetadataKey: map[string]any{"summary": message, "truncated": false},
	}
}

func (p *turnPolicy) project(event LLMTurnEvent) error {
	if p.emit == nil {
		return nil
	}
	p.emitMu.Lock()
	defer p.emitMu.Unlock()
	if err := p.emit(event); err != nil {
		p.mu.Lock()
		if p.projectionErr == nil {
			p.projectionErr = err
		}
		p.mu.Unlock()
	}
	return nil
}

func (p *turnPolicy) Usage() appharness.BudgetUsage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.usage
}

func (p *turnPolicy) ProjectionError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.projectionErr
}

func (p *turnPolicy) ExecutionError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.executionErr
}

func (p *turnPolicy) haltExecution(ctx agent.ToolContext, err error) {
	p.mu.Lock()
	if p.executionErr == nil {
		p.executionErr = err
	}
	p.mu.Unlock()
	markExecutionStop(ctx)
}

func markBudgetStop(ctx agent.ToolContext, dimension string) {
	actions := ctx.Actions()
	if actions == nil {
		return
	}
	if actions.StateDelta == nil {
		actions.StateDelta = make(map[string]any)
	}
	actions.StateDelta[stopReasonStateKey] = string(appharness.StopBudgetExceeded)
	actions.StateDelta[budgetDimensionStateKey] = dimension
	actions.SkipSummarization = true
}

func markExecutionStop(ctx agent.ToolContext) {
	actions := ctx.Actions()
	if actions == nil {
		return
	}
	if actions.StateDelta == nil {
		actions.StateDelta = make(map[string]any)
	}
	actions.StateDelta[stopReasonStateKey] = string(appharness.StopExecutionFailed)
	actions.SkipSummarization = true
}

func classifyToolError(err error) (agentcore.ToolErrorCode, bool) {
	var typed *agentcore.ToolError
	if errors.As(err, &typed) {
		return typed.Code, typed.Retryable
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return agentcore.ToolErrorCancelled, false
	case errors.Is(err, appharness.ErrBudgetExceeded):
		return agentcore.ToolErrorBudget, false
	case errors.Is(err, appharness.ErrToolCallInDoubt), errors.Is(err, appharness.ErrToolCallConflict):
		return agentcore.ToolErrorConflict, false
	case errors.Is(err, agentcore.ErrInvalidToolArguments):
		return agentcore.ToolErrorInvalidArgument, false
	case errors.Is(err, agentcore.ErrToolNotFound):
		return agentcore.ToolErrorNotFound, false
	case errors.Is(err, agentcore.ErrToolNotAllowed):
		return agentcore.ToolErrorPermission, false
	case errors.Is(err, agentcore.ErrToolCapability):
		return agentcore.ToolErrorCapability, false
	case errors.Is(err, canvas.ErrNodeNotFound), errors.Is(err, canvas.ErrCandidateNotFound),
		errors.Is(err, canvas.ErrChapterArchiveNotFound):
		return agentcore.ToolErrorNotFound, false
	case errors.Is(err, canvas.ErrRevisionConflict), errors.Is(err, canvas.ErrCandidateResolved),
		errors.Is(err, canvas.ErrDerivationExists), errors.Is(err, canvas.ErrArchivedNodeLocked):
		return agentcore.ToolErrorConflict, false
	case errors.Is(err, canvas.ErrInvalidNode), errors.Is(err, canvas.ErrInvalidChapterArchive),
		errors.Is(err, canvas.ErrInvalidSectionOutline):
		return agentcore.ToolErrorInvalidArgument, false
	default:
		return agentcore.ToolErrorInternal, true
	}
}

func redactToolValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, child := range current {
			if sensitiveToolKey(key) {
				result[key] = "[REDACTED]"
				continue
			}
			result[key] = redactToolValue(child)
		}
		return result
	case []any:
		result := make([]any, len(current))
		for index, child := range current {
			result[index] = redactToolValue(child)
		}
		return result
	default:
		return value
	}
}

func sensitiveToolKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	for _, marker := range []string{"api_key", "apikey", "authorization", "password", "secret", "access_token", "refresh_token"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func truncateUTF8(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	end := limit
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end] + "..."
}

package adk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/agent"
	adktool "google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	appharness "warmmo/core/internal/application/harness"
)

func adaptTools(
	registry appharness.ToolCatalog,
	allowed []string,
	base appharness.ToolInvocation,
	budget *turnBudget,
	codec toolResultCodec,
	events *eventSink,
	toolCalls appharness.ToolCallStore,
	additional ...appharness.Tool,
) ([]adktool.Tool, error) {
	if registry == nil {
		if len(allowed) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("tool registry is required")
	}
	if budget == nil {
		return nil, fmt.Errorf("turn budget is required")
	}
	allAllowed := append([]string(nil), allowed...)
	for _, tool := range additional {
		if tool != nil {
			allAllowed = append(allAllowed, tool.Spec().Name)
		}
	}
	base.AllowedTools = allAllowed
	snapshot, err := registry.StrictSnapshot(allowed)
	if err != nil {
		return nil, err
	}
	snapshot = append(snapshot, additional...)
	tools := make([]adktool.Tool, 0, len(snapshot))
	for _, currentTool := range snapshot {
		spec := currentTool.Spec()
		if err := validateToolSpec(spec); err != nil {
			return nil, err
		}
		inputSchema, err := schemaFromMap(spec.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("build tool %q schema: %w", spec.Name, err)
		}
		outputSchema, err := resolvedSchemaFromMap(spec.OutputSchema)
		if err != nil {
			return nil, fmt.Errorf("build tool %q output schema: %w", spec.Name, err)
		}
		wrapped, err := functiontool.New(
			functiontool.Config{
				Name: spec.Name, Description: spec.Description, InputSchema: inputSchema,
				IsLongRunning:       spec.LongRunning,
				RequireConfirmation: spec.Approval == appharness.ApprovalAlways,
			},
			func(ctx agent.ToolContext, args map[string]any) (map[string]any, error) {
				encoded, err := json.Marshal(args)
				if err != nil {
					return toolErrorResult(appharness.NewToolError(
						appharness.ToolErrorInvalidArgument, false, fmt.Errorf("encode tool arguments: %w", err),
					)), nil
				}
				argsHash, err := appharness.StableHash(args)
				if err != nil {
					return toolErrorResult(appharness.NewToolError(appharness.ToolErrorInternal, false, fmt.Errorf("hash tool arguments: %w", err))), nil
				}
				claimed := false
				if spec.SideEffect.RequiresIdempotency() {
					if toolCalls == nil {
						return nil, errors.New("side-effect tool call store is required")
					}
					record, acquired, claimErr := toolCalls.ClaimToolCall(ctx, appharness.ToolCallRecord{
						CallID: ctx.FunctionCallID(), RunID: base.RunID, TurnID: base.TurnID,
						ToolName: spec.Name, ArgsHash: argsHash, SideEffect: string(spec.SideEffect),
					})
					if claimErr != nil {
						if errors.Is(claimErr, appharness.ErrToolCallInDoubt) || errors.Is(claimErr, appharness.ErrToolCallConflict) {
							budget.haltExecution(ctx, claimErr)
						}
						return toolErrorResult(claimErr), nil
					}
					if !acquired {
						var replay map[string]any
						if err := json.Unmarshal(record.Result, &replay); err != nil {
							return toolErrorResult(appharness.NewToolError(appharness.ToolErrorInternal, false, fmt.Errorf("decode replayed tool result: %w", err))), nil
						}
						if spec.Terminal && replay["error"] == nil {
							ctx.Actions().SkipSummarization = true
						}
						return replay, nil
					}
					claimed = true
				}
				if err := budget.beforeTool(ctx, spec); err != nil {
					result := toolErrorResult(err)
					if claimed {
						if completeErr := completeToolCall(ctx, toolCalls, ctx.FunctionCallID(), argsHash, result); completeErr != nil {
							budget.haltExecution(ctx, completeErr)
							return nil, completeErr
						}
					}
					if !errors.Is(err, appharness.ErrBudgetExceeded) {
						return nil, err
					}
					return result, nil
				}
				_ = events.project(LLMTurnEvent{
					Type: LLMEventToolStarted, AgentName: ctx.AgentName(),
					ToolName: spec.Name, ToolCallID: ctx.FunctionCallID(),
				})
				invocation := base
				invocation.CallID = ctx.FunctionCallID()
				invocation.Args = encoded
				result, err := currentTool.Call(ctx, invocation)
				if err != nil {
					failed := toolErrorResult(err)
					if claimed {
						if completeErr := completeToolCall(ctx, toolCalls, ctx.FunctionCallID(), argsHash, failed); completeErr != nil {
							budget.haltExecution(ctx, completeErr)
							return nil, completeErr
						}
					}
					return failed, nil
				}
				shaped, shapeErr := codec.encode(spec, outputSchema, result)
				if shapeErr != nil {
					shaped = toolErrorResult(shapeErr)
				}
				if claimed {
					if completeErr := completeToolCall(ctx, toolCalls, ctx.FunctionCallID(), argsHash, shaped); completeErr != nil {
						budget.haltExecution(ctx, completeErr)
						return nil, completeErr
					}
				}
				if spec.Terminal && shapeErr == nil {
					ctx.Actions().SkipSummarization = true
				}
				return shaped, nil
			},
		)
		if err != nil {
			return nil, fmt.Errorf("adapt tool %q: %w", spec.Name, err)
		}
		tools = append(tools, wrapped)
	}
	return tools, nil
}

func completeToolCall(
	ctx context.Context,
	store appharness.ToolCallStore,
	callID string,
	argsHash string,
	result map[string]any,
) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode durable tool result: %w", err)
	}
	if _, err := store.CompleteToolCall(ctx, callID, argsHash, encoded); err != nil {
		return fmt.Errorf("persist durable tool result: %w", err)
	}
	return nil
}

func validateToolSpec(spec appharness.ToolSpec) error {
	if spec.Name == "" || spec.Description == "" {
		return fmt.Errorf("tool name and description are required")
	}
	if spec.SideEffect == "" {
		return fmt.Errorf("tool %q side-effect class is required", spec.Name)
	}
	switch spec.SideEffect {
	case appharness.SideEffectNone, appharness.SideEffectRead, appharness.SideEffectDelegate, appharness.SideEffectWrite, appharness.SideEffectExternal:
	default:
		return fmt.Errorf("tool %q has invalid side-effect class %q", spec.Name, spec.SideEffect)
	}
	if spec.Approval == "" {
		return fmt.Errorf("tool %q approval policy is required", spec.Name)
	}
	switch spec.Approval {
	case appharness.ApprovalNever, appharness.ApprovalAlways:
	default:
		return fmt.Errorf("tool %q has invalid approval policy %q", spec.Name, spec.Approval)
	}
	if spec.Approval == appharness.ApprovalAlways && !spec.SideEffect.Mutates() {
		return fmt.Errorf("read-only tool %q cannot require mutation approval", spec.Name)
	}
	return nil
}

func schemaFromMap(value map[string]any) (*jsonschema.Schema, error) {
	if len(value) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(encoded, &schema); err != nil {
		return nil, err
	}
	return &schema, nil
}

func resolvedSchemaFromMap(value map[string]any) (*jsonschema.Resolved, error) {
	schema, err := schemaFromMap(value)
	if err != nil || schema == nil {
		return nil, err
	}
	return schema.Resolve(nil)
}

func normalizeToolResult(value any) (map[string]any, error) {
	if result, ok := value.(map[string]any); ok {
		return result, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode tool result: %w", err)
	}
	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, fmt.Errorf("normalize tool result: %w", err)
	}
	return map[string]any{"result": normalized}, nil
}

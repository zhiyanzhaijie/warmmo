package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	appharness "warmmo/core/internal/application/harness"
)

type DelegationTools struct {
	port appharness.DelegationPort
}

func NewDelegationTools(port appharness.DelegationPort) (*DelegationTools, error) {
	if port == nil {
		return nil, errors.New("delegation port is required")
	}
	return &DelegationTools{port: port}, nil
}

func (p *DelegationTools) Provider(request appharness.RuntimeRequest, _ appharness.RuntimeEmitter) ([]appharness.Tool, error) {
	capabilities, err := p.port.Capabilities(request)
	if err != nil {
		return nil, err
	}
	tools := make([]appharness.Tool, 0, len(capabilities))
	for _, capability := range capabilities {
		if strings.TrimSpace(capability.Name) == "" || strings.TrimSpace(capability.Description) == "" {
			return nil, errors.New("delegation capability name and description are required")
		}
		tools = append(tools, &delegationTool{runtime: request, capability: capability, port: p.port})
	}
	return tools, nil
}

type delegationTool struct {
	runtime    appharness.RuntimeRequest
	capability appharness.DelegationCapability
	port       appharness.DelegationPort
}

func (t delegationTool) Spec() appharness.ToolSpec {
	return appharness.ToolSpec{
		Name: t.capability.Name, Description: t.capability.Description,
		InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{
				"task": map[string]any{"type": "string", "minLength": 1},
			}, "required": []string{"task"}, "additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object", "properties": map[string]any{
				"artifactId": map[string]any{"type": "string"}, "artifactKind": map[string]any{"type": "string"},
				"artifactSchemaVersion": map[string]any{"type": "string"},
			}, "required": []string{"artifactId", "artifactKind", "artifactSchemaVersion"}, "additionalProperties": false,
		},
		SideEffect: appharness.SideEffectDelegate, Approval: appharness.ApprovalNever,
		MaxResultBytes: 32 * 1024, Terminal: true, ModelCallable: true,
	}
}

func (t delegationTool) Call(ctx context.Context, invocation appharness.ToolInvocation) (any, error) {
	var args struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(invocation.Args, &args); err != nil {
		return nil, appharness.NewToolError(appharness.ToolErrorInvalidArgument, true, err)
	}
	args.Task = strings.TrimSpace(args.Task)
	if args.Task == "" {
		return nil, appharness.NewToolError(appharness.ToolErrorInvalidArgument, true, errors.New("specialist task is required"))
	}
	ref, err := t.port.Delegate(ctx, appharness.DelegationRequest{Runtime: t.runtime, Target: t.capability.Name, Task: args.Task})
	if err != nil {
		wrapped := fmt.Errorf("run specialist %q: %w", t.capability.Name, err)
		if errors.Is(err, appharness.ErrInvalidOutput) {
			return nil, appharness.NewToolError(appharness.ToolErrorInvalidArgument, false, wrapped)
		}
		return nil, wrapped
	}
	return map[string]any{"artifactId": ref.ID, "artifactKind": ref.Kind, "artifactSchemaVersion": ref.SchemaVersion}, nil
}

var _ appharness.Tool = (*delegationTool)(nil)

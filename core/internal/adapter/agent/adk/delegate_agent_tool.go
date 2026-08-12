package adk

import (
	"context"
	"errors"
	"strings"

	agentcore "warmmo/core/internal/adapter/agent/core"
	appharness "warmmo/core/internal/application/harness"
)

const DelegateAgentToolName = "delegate_agent"

type delegateAgentTool struct {
	children []appharness.ChildContract
}

func newDelegateAgentTool(allowedChildren []appharness.ChildContract) (delegateAgentTool, error) {
	children := make([]appharness.ChildContract, 0, len(allowedChildren))
	seen := make(map[string]struct{}, len(allowedChildren))
	for _, contract := range allowedChildren {
		contract.AgentID = strings.TrimSpace(contract.AgentID)
		if contract.AgentID == "" || len(contract.InputSchema) == 0 {
			continue
		}
		if _, exists := seen[contract.AgentID]; exists {
			continue
		}
		seen[contract.AgentID] = struct{}{}
		children = append(children, contract)
	}
	if len(children) == 0 {
		return delegateAgentTool{}, errors.New("delegate_agent requires at least one allowed child")
	}
	return delegateAgentTool{children: children}, nil
}

func (t delegateAgentTool) Spec() agentcore.ToolSpec {
	variants := make([]any, 0, len(t.children))
	for _, child := range t.children {
		variants = append(variants, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agentId": map[string]any{"type": "string", "const": child.AgentID},
				"task":    map[string]any{"type": "string", "minLength": 1},
				"reason":  map[string]any{"type": "string", "minLength": 1},
				"input":   child.InputSchema,
			},
			"required": []string{"agentId", "task", "reason", "input"}, "additionalProperties": false,
		})
	}
	return agentcore.ToolSpec{
		Name:        DelegateAgentToolName,
		Description: "Delegate one concrete task to an allowed child agent. The child has its own context, tools, budget, checkpoint, and output contract.",
		InputSchema: map[string]any{"type": "object", "oneOf": variants},
		OutputSchema: map[string]any{
			"type": "object", "properties": map[string]any{
				"status":                map[string]any{"type": "string"},
				"agentId":               map[string]any{"type": "string"},
				"artifactId":            map[string]any{"type": "string"},
				"artifactKind":          map[string]any{"type": "string"},
				"artifactSchemaVersion": map[string]any{"type": "string"},
				"receipt": map[string]any{
					"type": "object", "additionalProperties": true,
				},
				"error": map[string]any{
					"type": "object", "additionalProperties": false,
					"properties": map[string]any{
						"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"},
					},
					"required": []string{"code", "message"},
				},
			},
			"required": []string{"status"}, "additionalProperties": false,
		},
		SideEffect: agentcore.SideEffectNone, Approval: agentcore.ApprovalNever,
		MaxResultBytes: 1024, LongRunning: true, ModelCallable: true,
	}
}

func (delegateAgentTool) Call(context.Context, agentcore.ToolInvocation) (any, error) {
	return map[string]any{"status": "awaiting_child"}, nil
}

var _ agentcore.Tool = delegateAgentTool{}

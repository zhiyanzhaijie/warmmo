package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentcore "warmmo/core/internal/adapter/agent/core"
	"warmmo/core/internal/domain/canvas"
)

type NodeReader interface {
	GetNodes(context.Context, string, []string) ([]canvas.Node, error)
}

type CandidateCreator interface {
	CreateCandidate(context.Context, canvas.Candidate) (canvas.Candidate, error)
}

type GetNodesTool struct {
	store NodeReader
}

func NewGetNodesTool(store NodeReader) *GetNodesTool {
	return &GetNodesTool{store: store}
}

func (t *GetNodesTool) Spec() agentcore.ToolSpec {
	return agentcore.ToolSpec{
		Name: "canvas.get_nodes", Description: `Read up to 64 canvas nodes from the current work. Batch all currently required IDs into one call instead of calling once per node.`, ModelCallable: true,
		SideEffect: agentcore.SideEffectRead, Approval: agentcore.ApprovalNever, MaxResultBytes: 64 * 1024,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"nodeIds": map[string]any{
					"type": "array", "items": map[string]any{"type": "string"},
					"minItems": 1, "maxItems": 64,
				},
			},
			"required": []string{"nodeIds"}, "additionalProperties": false,
		},
		OutputSchema: arrayToolResultSchema(),
	}
}

func (t *GetNodesTool) Call(ctx context.Context, invocation agentcore.ToolInvocation) (any, error) {
	var input struct {
		NodeIDs []string `json:"nodeIds"`
		IDs     []string `json:"ids"`
	}
	if err := decodeToolArgs(invocation.Args, &input); err != nil {
		return nil, err
	}
	if len(input.NodeIDs) == 0 {
		input.NodeIDs = input.IDs
	}
	if len(input.NodeIDs) == 0 {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInvalidArgument, false, errors.New("nodeIds is required"))
	}
	return t.store.GetNodes(ctx, invocation.WorkID, input.NodeIDs)
}

type CreateCandidateTool struct {
	store CandidateCreator
}

func NewCreateCandidateTool(store CandidateCreator) *CreateCandidateTool {
	return &CreateCandidateTool{store: store}
}

func (t *CreateCandidateTool) Spec() agentcore.ToolSpec {
	return agentcore.ToolSpec{
		Name: "canvas.create_candidate", Description: "Create a candidate canvas revision without overwriting accepted content.",
		SideEffect: agentcore.SideEffectWrite, Approval: agentcore.ApprovalAlways, MaxResultBytes: 16 * 1024,
		OutputSchema: objectToolResultSchema(),
	}
}

func (t *CreateCandidateTool) Call(ctx context.Context, invocation agentcore.ToolInvocation) (any, error) {
	var input struct {
		Content string `json:"content"`
	}
	if err := decodeToolArgs(invocation.Args, &input); err != nil {
		return nil, err
	}
	input.Content = strings.TrimSpace(input.Content)
	if input.Content == "" {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInvalidArgument, false, errors.New("candidate content is required"))
	}
	candidate := canvas.Candidate{
		RunID: invocation.RunID, WorkID: invocation.WorkID,
		SkillID: invocation.SkillID, SkillVersion: invocation.SkillVersion,
		Content: input.Content, CreatedAt: time.Now().UTC(),
	}
	return t.store.CreateCandidate(ctx, candidate)
}

func decodeToolArgs(data json.RawMessage, target any) error {
	if len(data) == 0 {
		return errors.New("tool arguments are required")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return agentcore.NewToolError(agentcore.ToolErrorInvalidArgument, false, fmt.Errorf("decode tool arguments: %w", err))
	}
	return nil
}

func arrayToolResultSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"result": map[string]any{
				"type": "array", "items": map[string]any{"type": "object"},
			},
		},
		"required": []string{"result"}, "additionalProperties": false,
	}
}

func objectToolResultSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"result": map[string]any{"type": "object"},
		},
		"required": []string{"result"}, "additionalProperties": false,
	}
}

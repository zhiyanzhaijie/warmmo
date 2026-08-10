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
		Name: "canvas.get_nodes", Description: `Read up to 64 canvas nodes from the current work. Batch all currently required IDs into one call instead of calling once per node. Arguments: {"nodeIds":["node-id-1","node-id-2"]}.`, ModelCallable: true,
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
		return nil, errors.New("nodeIds is required")
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
		return nil, errors.New("candidate content is required")
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
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	return nil
}

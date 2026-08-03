package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"warmnote/core/internal/agent"
)

type GetNodesTool struct {
	store Store
}

func NewGetNodesTool(store Store) *GetNodesTool {
	return &GetNodesTool{store: store}
}

func (t *GetNodesTool) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name: "canvas.get_nodes", Description: "Read canvas nodes from the current work by node ID.", ModelCallable: true,
	}
}

func (t *GetNodesTool) Call(ctx context.Context, invocation agent.ToolInvocation) (any, error) {
	var input struct {
		NodeIDs []string `json:"nodeIds"`
	}
	if err := decodeToolArgs(invocation.Args, &input); err != nil {
		return nil, err
	}
	if len(input.NodeIDs) == 0 {
		return nil, errors.New("nodeIds is required")
	}
	return t.store.GetNodes(ctx, invocation.WorkID, input.NodeIDs)
}

type CreateCandidateTool struct {
	store Store
}

func NewCreateCandidateTool(store Store) *CreateCandidateTool {
	return &CreateCandidateTool{store: store}
}

func (t *CreateCandidateTool) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name: "canvas.create_candidate", Description: "Create a candidate canvas revision without overwriting accepted content.",
	}
}

func (t *CreateCandidateTool) Call(ctx context.Context, invocation agent.ToolInvocation) (any, error) {
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
	candidate := agent.Candidate{
		RunID: invocation.RunID, WorkID: invocation.WorkID,
		SkillID: invocation.Skill.ID, SkillVersion: invocation.Skill.Version,
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

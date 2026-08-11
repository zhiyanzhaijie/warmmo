package adk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	agentcore "warmmo/core/internal/adapter/agent/core"
	appharness "warmmo/core/internal/application/harness"
)

const SubmitArtifactToolName = "submit_artifact"

type submitArtifactTool struct {
	contract appharness.OutputContract
	store    appharness.ArtifactStore
	runID    string
	turnID   string
	agentID  string

	mu        sync.Mutex
	submitted *appharness.ArtifactRef
}

func newSubmitArtifactTool(
	contract appharness.OutputContract,
	store appharness.ArtifactStore,
	runID string,
	turnID string,
	agentID string,
) (*submitArtifactTool, error) {
	if contract.Kind != appharness.OutputKindArtifact || len(contract.Artifacts) == 0 {
		return nil, errors.New("artifact output contract is required")
	}
	if store == nil {
		return nil, errors.New("artifact store is required")
	}
	return &submitArtifactTool{contract: contract, store: store, runID: runID, turnID: turnID, agentID: agentID}, nil
}

func (t *submitArtifactTool) Spec() agentcore.ToolSpec {
	kinds := make([]string, 0, len(t.contract.Artifacts))
	artifactSchemas := make([]any, 0, len(t.contract.Artifacts))
	for _, artifact := range t.contract.Artifacts {
		kinds = append(kinds, artifact.Kind)
		artifactSchemas = append(artifactSchemas, artifact.Schema)
	}
	sort.Strings(kinds)
	return agentcore.ToolSpec{
		Name:        SubmitArtifactToolName,
		Description: "Submit the final typed artifact for this agent turn. Call exactly once when the artifact is complete.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"kind":     map[string]any{"type": "string", "enum": kinds},
				"artifact": map[string]any{"oneOf": artifactSchemas},
			},
			"required":             []string{"kind", "artifact"},
			"additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"artifactId":    map[string]any{"type": "string"},
				"kind":          map[string]any{"type": "string"},
				"schemaVersion": map[string]any{"type": "string"},
			},
			"required":             []string{"artifactId", "kind", "schemaVersion"},
			"additionalProperties": false,
		},
		SideEffect: agentcore.SideEffectWrite, Approval: agentcore.ApprovalNever,
		MaxResultBytes: 4096, ModelCallable: true,
	}
}

func (t *submitArtifactTool) Call(ctx context.Context, invocation agentcore.ToolInvocation) (any, error) {
	var submission struct {
		Kind     string          `json:"kind"`
		Artifact json.RawMessage `json:"artifact"`
	}
	decoder := json.NewDecoder(bytes.NewReader(invocation.Args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&submission); err != nil {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInvalidArgument, false, fmt.Errorf("decode artifact submission: %w", err))
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInvalidArgument, false, errors.New("artifact submission must contain exactly one JSON object"))
	}
	contract, ok := t.contract.Artifact(submission.Kind)
	if !ok {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInvalidArgument, false, fmt.Errorf("artifact kind %q is not allowed", submission.Kind))
	}
	resolved, err := resolvedSchemaFromMap(contract.Schema)
	if err != nil {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInternal, false, fmt.Errorf("resolve artifact schema: %w", err))
	}
	var payload any
	if err := json.Unmarshal(submission.Artifact, &payload); err != nil {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInvalidArgument, false, fmt.Errorf("decode artifact payload: %w", err))
	}
	if resolved == nil {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInternal, false, errors.New("artifact schema is empty"))
	}
	if err := resolved.Validate(payload); err != nil {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInvalidArgument, false, fmt.Errorf("artifact does not match %s: %w", contract.Kind, err))
	}
	artifact := appharness.Artifact{
		Ref:   appharness.ArtifactRef{ID: t.turnID, Kind: contract.Kind, SchemaVersion: contract.SchemaVersion},
		RunID: t.runID, TurnID: t.turnID, AgentID: t.agentID,
		Payload: append(json.RawMessage(nil), submission.Artifact...),
	}
	stored, err := t.store.SaveArtifact(ctx, artifact)
	if err != nil {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInternal, false, fmt.Errorf("persist artifact: %w", err))
	}
	t.mu.Lock()
	ref := stored.Ref
	t.submitted = &ref
	t.mu.Unlock()
	return map[string]any{
		"artifactId":    stored.Ref.ID,
		"kind":          stored.Ref.Kind,
		"schemaVersion": stored.Ref.SchemaVersion,
	}, nil
}

func (t *submitArtifactTool) Submitted() *appharness.ArtifactRef {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.submitted == nil {
		return nil
	}
	ref := *t.submitted
	return &ref
}

func (t *submitArtifactTool) Replayed(result map[string]any) {
	id, _ := result["artifactId"].(string)
	kind, _ := result["kind"].(string)
	version, _ := result["schemaVersion"].(string)
	if id == "" || kind == "" || version == "" {
		return
	}
	t.mu.Lock()
	t.submitted = &appharness.ArtifactRef{ID: id, Kind: kind, SchemaVersion: version}
	t.mu.Unlock()
}

var _ agentcore.Tool = (*submitArtifactTool)(nil)

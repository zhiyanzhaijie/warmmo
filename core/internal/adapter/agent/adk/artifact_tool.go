package adk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	agentcore "warmmo/core/internal/adapter/agent/core"
	appharness "warmmo/core/internal/application/harness"
)

const SubmitArtifactToolName = "submit_artifact"

type submitArtifactTool struct {
	artifact appharness.ArtifactSchema
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
	if contract.Kind != appharness.OutputKindArtifact || len(contract.Artifacts) != 1 {
		return nil, errors.New("artifact turn requires exactly one output schema")
	}
	if store == nil {
		return nil, errors.New("artifact store is required")
	}
	return &submitArtifactTool{artifact: contract.Artifacts[0], store: store, runID: runID, turnID: turnID, agentID: agentID}, nil
}

func (t *submitArtifactTool) Spec() agentcore.ToolSpec {
	return agentcore.ToolSpec{
		Name:        SubmitArtifactToolName,
		Description: "Submit the complete final artifact for this turn. The artifact kind is fixed by the agent contract; pass the artifact fields directly and call exactly once.",
		InputSchema: submitArtifactInputSchema(t.artifact.Schema),
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
	payload, encoded, err := decodeSubmittedArtifact(invocation.Args, t.artifact.Schema)
	if err != nil {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInvalidArgument, false, err)
	}
	resolved, err := resolvedSchemaFromMap(t.artifact.Schema)
	if err != nil {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInternal, false, fmt.Errorf("resolve artifact schema: %w", err))
	}
	if resolved == nil {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInternal, false, errors.New("artifact schema is empty"))
	}
	if err := resolved.Validate(payload); err != nil {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInvalidArgument, false, fmt.Errorf("artifact does not match %s: %w", t.artifact.Kind, err))
	}
	artifact := appharness.Artifact{
		Ref:   appharness.ArtifactRef{ID: t.turnID, Kind: t.artifact.Kind, SchemaVersion: t.artifact.SchemaVersion},
		RunID: t.runID, TurnID: t.turnID, AgentID: t.agentID,
		Payload: append(json.RawMessage(nil), encoded...),
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

func submitArtifactInputSchema(schema map[string]any) map[string]any {
	if schema["type"] == "object" {
		return schema
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"value": schema,
		},
		"required":             []string{"value"},
		"additionalProperties": false,
	}
}

func decodeSubmittedArtifact(arguments json.RawMessage, schema map[string]any) (any, json.RawMessage, error) {
	var submitted any
	if err := json.Unmarshal(arguments, &submitted); err != nil {
		return nil, nil, fmt.Errorf("decode artifact submission: %w", err)
	}
	if schema["type"] == "object" {
		object, ok := submitted.(map[string]any)
		if !ok {
			return nil, nil, errors.New("artifact submission must be an object")
		}
		encoded, err := json.Marshal(object)
		if err != nil {
			return nil, nil, fmt.Errorf("encode artifact submission: %w", err)
		}
		return object, encoded, nil
	}
	wrapper, ok := submitted.(map[string]any)
	if !ok || len(wrapper) != 1 {
		return nil, nil, errors.New("artifact submission requires only the value field")
	}
	value, exists := wrapper["value"]
	if !exists {
		return nil, nil, errors.New("artifact submission requires the value field")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, nil, fmt.Errorf("encode artifact submission: %w", err)
	}
	return value, encoded, nil
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

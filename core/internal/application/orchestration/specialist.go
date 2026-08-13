package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	appharness "warmmo/core/internal/application/harness"
	"warmmo/core/internal/application/writing"
)

// SpecialistToolContext is the application payload used to expose
// invocation-scoped specialist capabilities through a runtime adapter.
type SpecialistToolContext struct {
	Input writing.RunInput `json:"input"`
}

type SpecialistDelegator struct {
	chain *WritingCollaborationChain
}

func NewSpecialistDelegator(chain *WritingCollaborationChain) (*SpecialistDelegator, error) {
	if chain == nil {
		return nil, errors.New("writing collaboration chain is required")
	}
	return &SpecialistDelegator{chain: chain}, nil
}

func (d *SpecialistDelegator) Capabilities(request appharness.RuntimeRequest) ([]appharness.DelegationCapability, error) {
	if request.AgentID != CanvasOrchestratorDefinitionID || len(request.Extension) == 0 {
		return nil, nil
	}
	extension, err := decodeSpecialistContext(request.Extension)
	if err != nil {
		return nil, err
	}
	children, err := RootChildren(extension.Input.Target)
	if err != nil {
		return nil, err
	}
	capabilities := make([]appharness.DelegationCapability, 0, len(children))
	for _, child := range children {
		capabilities = append(capabilities, appharness.DelegationCapability{Name: child, Description: "Delegate one complete task to this specialist agent."})
	}
	return capabilities, nil
}

func (d *SpecialistDelegator) Delegate(ctx context.Context, request appharness.DelegationRequest) (appharness.ArtifactRef, error) {
	if d == nil || d.chain == nil {
		return appharness.ArtifactRef{}, errors.New("specialist delegator is not configured")
	}
	extension, err := decodeSpecialistContext(request.Runtime.Extension)
	if err != nil {
		return appharness.ArtifactRef{}, err
	}
	allowed, err := RootChildren(extension.Input.Target)
	if err != nil {
		return appharness.ArtifactRef{}, err
	}
	if !containsString(allowed, request.Target) {
		return appharness.ArtifactRef{}, fmt.Errorf("specialist %q is not allowed for target %q", request.Target, extension.Input.Target)
	}
	task := strings.TrimSpace(request.Task)
	if task == "" {
		return appharness.ArtifactRef{}, errors.New("specialist task is required")
	}
	emit := writing.Emitter(func(writing.EventType, any) error { return nil })
	var result writing.RunResult
	if request.Target == PlannerDefinitionID {
		result, err = d.chain.RunDelegated(ctx, extension.Input, request.Runtime.TurnID, task, emit)
	} else {
		result, err = d.chain.RunCreator(ctx, extension.Input, request.Runtime.TurnID, task, request.Target, emit)
	}
	if err != nil {
		return appharness.ArtifactRef{}, err
	}
	return appharness.ArtifactRef{ID: result.ArtifactID, Kind: result.ArtifactKind, SchemaVersion: "1"}, nil
}

func decodeSpecialistContext(raw json.RawMessage) (SpecialistToolContext, error) {
	var extension SpecialistToolContext
	if err := json.Unmarshal(raw, &extension); err != nil {
		return SpecialistToolContext{}, fmt.Errorf("decode agent tool context: %w", err)
	}
	return extension, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

var _ appharness.DelegationPort = (*SpecialistDelegator)(nil)

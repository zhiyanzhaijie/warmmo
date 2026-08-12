package collaboration

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"warmmo/core/internal/adapter/agent/adk"
	appharness "warmmo/core/internal/application/harness"
)

type DurableChildRunner struct {
	base        TurnRunner
	definitions *appharness.DefinitionRegistry
	checkpoints appharness.CheckpointStore
	children    appharness.ChildRunStore
}

func NewDurableChildRunner(
	base TurnRunner,
	definitions *appharness.DefinitionRegistry,
	checkpoints appharness.CheckpointStore,
	children appharness.ChildRunStore,
) (*DurableChildRunner, error) {
	if base == nil || definitions == nil || checkpoints == nil || children == nil {
		return nil, errors.New("durable child runner dependencies are required")
	}
	return &DurableChildRunner{base: base, definitions: definitions, checkpoints: checkpoints, children: children}, nil
}

func (r *DurableChildRunner) Run(
	ctx context.Context,
	request adk.LLMTurnRequest,
	emit adk.LLMTurnEmitter,
) (adk.LLMTurnOutcome, error) {
	if request.ParentTurnID == "" && request.ParentAgentID == "" {
		return r.base.Run(ctx, request, emit)
	}
	if request.ParentTurnID == "" || request.ParentAgentID == "" {
		return adk.LLMTurnOutcome{}, errors.New("child turn requires complete parent identity")
	}
	parent, err := r.definitions.Resolve(request.ParentAgentID)
	if err != nil {
		return adk.LLMTurnOutcome{}, err
	}
	if !containsAgent(parent.Definition.AllowedChildren, request.AgentID) {
		return adk.LLMTurnOutcome{}, fmt.Errorf("agent %q cannot delegate to %q", request.ParentAgentID, request.AgentID)
	}
	child := appharness.ChildRun{
		ID: uuid.NewString(), RunID: request.RunID,
		ParentTurnID: request.ParentTurnID, ParentAgentID: request.ParentAgentID,
		ChildTurnID: request.TurnID, ChildAgentID: request.AgentID, SessionID: request.SessionID,
	}
	stored, err := r.children.StartChildRun(ctx, child)
	if err != nil {
		return adk.LLMTurnOutcome{}, err
	}
	if _, err := r.checkpoints.AttachChildRun(ctx, request.ParentTurnID, stored.ID); err != nil {
		return adk.LLMTurnOutcome{}, err
	}
	outcome, runErr := r.base.Run(ctx, request, emit)
	if runErr != nil && outcome.Status == "" {
		outcome.Status = appharness.TurnFailed
		outcome.StopReason = appharness.StopExecutionFailed
		outcome.SessionID = request.SessionID
	}
	if _, err := r.children.FinishChildRun(context.WithoutCancel(ctx), request.TurnID, outcome); err != nil {
		return outcome, errors.Join(runErr, err)
	}
	return outcome, runErr
}

func (r *DurableChildRunner) Resume(
	ctx context.Context,
	runID string,
	answer string,
	emit adk.LLMTurnEmitter,
) (adk.LLMTurnOutcome, error) {
	pending, err := r.checkpoints.FindPendingCheckpoint(ctx, runID)
	if err != nil {
		return adk.LLMTurnOutcome{}, err
	}
	outcome, runErr := r.base.Resume(ctx, runID, answer, emit)
	if runErr != nil && outcome.Status == "" {
		outcome.Status = appharness.TurnFailed
		outcome.StopReason = appharness.StopExecutionFailed
		outcome.SessionID = pending.SessionID
	}
	if _, err := r.children.GetChildRunByTurn(ctx, pending.TurnID); errors.Is(err, appharness.ErrChildRunNotFound) {
		return outcome, runErr
	} else if err != nil {
		return outcome, errors.Join(runErr, err)
	}
	if _, err := r.children.FinishChildRun(context.WithoutCancel(ctx), pending.TurnID, outcome); err != nil {
		return outcome, errors.Join(runErr, err)
	}
	return outcome, runErr
}

func (r *DurableChildRunner) Continue(
	ctx context.Context,
	turnID string,
	response map[string]any,
	emit adk.LLMTurnEmitter,
) (adk.LLMTurnOutcome, error) {
	return r.base.Continue(ctx, turnID, response, emit)
}

func containsAgent(values []appharness.ChildContract, target string) bool {
	for _, value := range values {
		if value.AgentID == target {
			return true
		}
	}
	return false
}

var _ TurnRunner = (*DurableChildRunner)(nil)

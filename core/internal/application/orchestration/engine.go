package orchestration

import (
	"context"
	"errors"

	"warmmo/core/internal/application/writing"
)

type Engine struct {
	orchestrator *CanvasOrchestrator
	noncollab    *NonCollaborativeChain
}

func NewEngine(orchestrator *CanvasOrchestrator, noncollab *NonCollaborativeChain) (*Engine, error) {
	if orchestrator == nil {
		return nil, errors.New("canvas orchestrator is required")
	}
	if noncollab == nil {
		return nil, errors.New("non-collaborative chain is required")
	}
	return &Engine{orchestrator: orchestrator, noncollab: noncollab}, nil
}

func (e *Engine) Run(ctx context.Context, input writing.RunInput, emit writing.Emitter) (writing.RunResult, error) {
	if writing.IsCollaborativeTarget(input.Target) {
		return e.orchestrator.Run(ctx, input, emit)
	}
	return e.noncollab.Run(ctx, input, emit)
}

func (e *Engine) Resume(ctx context.Context, input writing.RunInput, answer string, emit writing.Emitter) (writing.RunResult, error) {
	if writing.IsCollaborativeTarget(input.Target) {
		return e.orchestrator.Resume(ctx, input, answer, emit)
	}
	return e.noncollab.Resume(ctx, input, answer, emit)
}

func (e *Engine) Recover(ctx context.Context, input writing.RunInput, emit writing.Emitter) (writing.RunResult, error) {
	if writing.IsCollaborativeTarget(input.Target) {
		return e.orchestrator.Recover(ctx, input, emit)
	}
	return e.noncollab.Recover(ctx, input, emit)
}

var _ writing.Engine = (*Engine)(nil)
var _ writing.ResumableEngine = (*Engine)(nil)

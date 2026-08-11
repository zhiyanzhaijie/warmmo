package collaboration

import (
	"context"
	"errors"

	writing "warmmo/core/internal/adapter/agent/writing"
)

type Engine struct {
	chain     *WritingCollaborationChain
	noncollab *NonCollaborativeChain
}

func NewEngine(chain *WritingCollaborationChain, noncollab *NonCollaborativeChain) (*Engine, error) {
	if chain == nil {
		return nil, errors.New("writing collaboration chain is required")
	}
	if noncollab == nil {
		return nil, errors.New("non-collaborative chain is required")
	}
	return &Engine{chain: chain, noncollab: noncollab}, nil
}

func (e *Engine) Run(ctx context.Context, input writing.RunInput, emit writing.Emitter) (writing.RunResult, error) {
	if writing.IsCollaborativeTarget(input.Target) {
		return e.chain.Run(ctx, input, emit)
	}
	return e.noncollab.Run(ctx, input, emit)
}

func (e *Engine) Resume(ctx context.Context, input writing.RunInput, answer string, emit writing.Emitter) (writing.RunResult, error) {
	if writing.IsCollaborativeTarget(input.Target) {
		return e.chain.Resume(ctx, input, answer, emit)
	}
	return e.noncollab.Resume(ctx, input, answer, emit)
}

func (e *Engine) Recover(ctx context.Context, input writing.RunInput, emit writing.Emitter) (writing.RunResult, error) {
	if writing.IsCollaborativeTarget(input.Target) {
		return e.chain.Recover(ctx, input, emit)
	}
	return e.noncollab.Recover(ctx, input, emit)
}

var _ writing.Engine = (*Engine)(nil)
var _ writing.ResumableEngine = (*Engine)(nil)

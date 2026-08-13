package adk

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"

	appharness "warmmo/core/internal/application/harness"
)

const (
	stopReasonStateKey      = "warmmo:stop_reason"
	budgetDimensionStateKey = "warmmo:budget_dimension"
)

type turnBudget struct {
	budget  appharness.BudgetPolicy
	reserve func(context.Context, appharness.BudgetUsage) error

	mu           sync.Mutex
	usage        appharness.BudgetUsage
	executionErr error
}

type turnBudgetConfig struct {
	Budget  appharness.BudgetPolicy
	Initial appharness.BudgetUsage
	Reserve func(context.Context, appharness.BudgetUsage) error
}

func newTurnBudget(config turnBudgetConfig) (*turnBudget, error) {
	budget := config.Budget
	if budget == (appharness.BudgetPolicy{}) {
		budget = appharness.DefaultBudgetPolicy()
	}
	if budget.MaxModelCalls <= 0 || budget.MaxToolCalls < 0 || budget.MaxSideEffectCalls < 0 ||
		budget.MaxDuration <= 0 || budget.MaxToolResultBytes < 1024 {
		return nil, errors.New("invalid agent turn budget")
	}
	if config.Reserve == nil {
		return nil, errors.New("turn budget reservation store is required")
	}
	if config.Initial.ModelCalls < 0 || config.Initial.ToolCalls < 0 || config.Initial.SideEffectCalls < 0 ||
		config.Initial.ModelCalls > budget.MaxModelCalls || config.Initial.ToolCalls > budget.MaxToolCalls ||
		config.Initial.SideEffectCalls > budget.MaxSideEffectCalls {
		return nil, errors.New("invalid initial agent turn budget usage")
	}
	return &turnBudget{
		budget: budget, usage: config.Initial, reserve: config.Reserve,
	}, nil
}

func (p *turnBudget) reserveControlTool(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.usage.ToolCalls >= p.budget.MaxToolCalls {
		return fmt.Errorf("%w: tool calls (%d)", appharness.ErrBudgetExceeded, p.budget.MaxToolCalls)
	}
	p.usage.ToolCalls++
	if err := p.reserve(ctx, p.usage); err != nil {
		p.usage.ToolCalls--
		return fmt.Errorf("persist control tool budget reservation: %w", err)
	}
	return nil
}

func (p *turnBudget) beforeModel(ctx agent.CallbackContext, _ *model.LLMRequest) (*model.LLMResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.usage.ModelCalls >= p.budget.MaxModelCalls {
		return nil, fmt.Errorf("%w: model calls (%d)", appharness.ErrBudgetExceeded, p.budget.MaxModelCalls)
	}
	p.usage.ModelCalls++
	if err := p.reserve(ctx, p.usage); err != nil {
		p.usage.ModelCalls--
		return nil, fmt.Errorf("persist model budget reservation: %w", err)
	}
	return nil, nil
}

func (p *turnBudget) beforeTool(ctx agent.ToolContext, spec appharness.ToolSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	if p.usage.ToolCalls >= p.budget.MaxToolCalls {
		p.mu.Unlock()
		markBudgetStop(ctx, "tool_calls")
		return fmt.Errorf("%w: tool calls (%d)", appharness.ErrBudgetExceeded, p.budget.MaxToolCalls)
	}
	if spec.SideEffect.Mutates() && p.usage.SideEffectCalls >= p.budget.MaxSideEffectCalls {
		p.mu.Unlock()
		markBudgetStop(ctx, "side_effect_calls")
		return fmt.Errorf("%w: side-effect calls (%d)", appharness.ErrBudgetExceeded, p.budget.MaxSideEffectCalls)
	}
	p.usage.ToolCalls++
	if spec.SideEffect.Mutates() {
		p.usage.SideEffectCalls++
	}
	if err := p.reserve(ctx, p.usage); err != nil {
		p.usage.ToolCalls--
		if spec.SideEffect.Mutates() {
			p.usage.SideEffectCalls--
		}
		p.executionErr = fmt.Errorf("persist tool budget reservation: %w", err)
		p.mu.Unlock()
		markExecutionStop(ctx)
		return p.executionErr
	}
	p.mu.Unlock()

	return nil
}

func (p *turnBudget) Usage() appharness.BudgetUsage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.usage
}

func (p *turnBudget) ExecutionError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.executionErr
}

func (p *turnBudget) haltExecution(ctx agent.ToolContext, err error) {
	p.mu.Lock()
	if p.executionErr == nil {
		p.executionErr = err
	}
	p.mu.Unlock()
	markExecutionStop(ctx)
}

func markBudgetStop(ctx agent.ToolContext, dimension string) {
	actions := ctx.Actions()
	if actions == nil {
		return
	}
	if actions.StateDelta == nil {
		actions.StateDelta = make(map[string]any)
	}
	actions.StateDelta[stopReasonStateKey] = string(appharness.StopBudgetExceeded)
	actions.StateDelta[budgetDimensionStateKey] = dimension
	actions.SkipSummarization = true
}

func markExecutionStop(ctx agent.ToolContext) {
	actions := ctx.Actions()
	if actions == nil {
		return
	}
	if actions.StateDelta == nil {
		actions.StateDelta = make(map[string]any)
	}
	actions.StateDelta[stopReasonStateKey] = string(appharness.StopExecutionFailed)
	actions.SkipSummarization = true
}

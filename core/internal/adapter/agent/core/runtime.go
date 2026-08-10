package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Budget struct {
	MaxSteps      int
	MaxModelCalls int
	MaxToolCalls  int
	MaxDuration   time.Duration
}

func DefaultBudget() Budget {
	return Budget{MaxSteps: 16, MaxModelCalls: 16, MaxToolCalls: 16, MaxDuration: 3 * time.Minute}
}

// Runtime owns the mechanics shared by every agent workflow. Domain policy
// remains in Loop and its collaborators.
type Runtime struct {
	tools  *ToolRegistry
	budget Budget
}

type RuntimeWorkflow func(context.Context, *Execution) error

type TurnHandler func(context.Context, int) (bool, error)

type Execution struct {
	tools *ToolRegistry
	emit  Emitter
	model *meteredModel

	budget Budget

	toolMutex sync.Mutex
	toolCalls int
}

func NewRuntime(tools *ToolRegistry, budget Budget) *Runtime {
	return &Runtime{tools: tools, budget: budget}
}

func (r *Runtime) Run(ctx context.Context, model TextModel, emit Emitter, workflow RuntimeWorkflow) error {
	if r == nil || r.tools == nil || model == nil || emit == nil || workflow == nil {
		return errors.New("agent runtime dependencies are not configured")
	}
	if r.budget.MaxSteps <= 0 || r.budget.MaxModelCalls <= 0 || r.budget.MaxToolCalls < 0 || r.budget.MaxDuration <= 0 {
		return errors.New("invalid agent budget")
	}

	runCtx, cancel := context.WithTimeout(ctx, r.budget.MaxDuration)
	defer cancel()
	execution := &Execution{
		tools:  r.tools,
		emit:   emit,
		model:  &meteredModel{delegate: model, limit: r.budget.MaxModelCalls},
		budget: r.budget,
	}
	return workflow(runCtx, execution)
}

func (e *Execution) Model() TextModel {
	return e.model
}

func (e *Execution) RemainingModelCalls() int {
	return e.model.remaining()
}

func (e *Execution) RunTurns(ctx context.Context, handler TurnHandler) error {
	for step := 1; step <= e.budget.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.RemainingModelCalls() == 0 {
			return fmt.Errorf("model call budget exceeded: %d", e.budget.MaxModelCalls)
		}
		done, err := handler(ctx, step)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return fmt.Errorf("agent step budget exceeded: %d", e.budget.MaxSteps)
}

// CallTool applies the common lifecycle and budget contract. Workflows decide
// whether a returned tool error is recoverable and how it affects their state.
func (e *Execution) CallTool(ctx context.Context, name string, invocation ToolInvocation, attributes map[string]any) (any, error) {
	if err := e.reserveToolCall(); err != nil {
		return nil, err
	}

	requested := cloneAttributes(attributes)
	requested["name"] = name
	requested["arguments"] = invocation.Args
	if err := e.emit(EventToolRequested, requested); err != nil {
		return nil, err
	}
	started := cloneAttributes(attributes)
	started["name"] = name
	if err := e.emit(EventToolStarted, started); err != nil {
		return nil, err
	}

	result, err := e.tools.Call(ctx, name, invocation)
	if err != nil {
		failed := cloneAttributes(attributes)
		failed["name"] = name
		failed["message"] = err.Error()
		_ = e.emit(EventToolFailed, failed)
		return nil, err
	}
	completed := cloneAttributes(attributes)
	completed["name"] = name
	completed["summary"] = summarizeToolResult(result)
	if err := e.emit(EventToolCompleted, completed); err != nil {
		return nil, err
	}
	return result, nil
}

func summarizeToolResult(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	const maxSummaryBytes = 4096
	if len(encoded) > maxSummaryBytes {
		return string(encoded[:maxSummaryBytes]) + "..."
	}
	return string(encoded)
}

func (e *Execution) reserveToolCall() error {
	e.toolMutex.Lock()
	defer e.toolMutex.Unlock()
	if e.toolCalls >= e.budget.MaxToolCalls {
		return fmt.Errorf("tool call budget exceeded: %d", e.budget.MaxToolCalls)
	}
	e.toolCalls++
	return nil
}

func cloneAttributes(attributes map[string]any) map[string]any {
	cloned := make(map[string]any, len(attributes)+2)
	for key, value := range attributes {
		cloned[key] = value
	}
	return cloned
}

type meteredModel struct {
	delegate TextModel
	limit    int

	mutex sync.Mutex
	calls int
}

func (m *meteredModel) Complete(ctx context.Context, request ModelRequest) (string, ModelUsage, error) {
	if err := m.reserve(); err != nil {
		return "", ModelUsage{}, err
	}
	return m.delegate.Complete(ctx, request)
}

func (m *meteredModel) Stream(ctx context.Context, request ModelRequest, onDelta func(string) error) (ModelUsage, error) {
	if err := m.reserve(); err != nil {
		return ModelUsage{}, err
	}
	return m.delegate.Stream(ctx, request, onDelta)
}

func (m *meteredModel) reserve() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.calls >= m.limit {
		return fmt.Errorf("model call budget exceeded: %d", m.limit)
	}
	m.calls++
	return nil
}

func (m *meteredModel) remaining() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return max(m.limit-m.calls, 0)
}

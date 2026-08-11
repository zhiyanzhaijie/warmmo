package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	ErrToolNotFound         = errors.New("tool not found")
	ErrToolNotAllowed       = errors.New("tool is not allowed by active skill")
	ErrInvalidToolArguments = errors.New("invalid tool arguments")
	ErrToolCapability       = errors.New("tool capability is unavailable")
)

type SideEffectClass string

const (
	SideEffectNone     SideEffectClass = "none"
	SideEffectRead     SideEffectClass = "read"
	SideEffectWrite    SideEffectClass = "write"
	SideEffectExternal SideEffectClass = "external"
)

func (s SideEffectClass) Mutates() bool {
	return s == SideEffectWrite || s == SideEffectExternal
}

type ApprovalPolicy string

const (
	ApprovalNever  ApprovalPolicy = "never"
	ApprovalAlways ApprovalPolicy = "always"
)

type ToolErrorCode string

const (
	ToolErrorInvalidArgument ToolErrorCode = "invalid_argument"
	ToolErrorNotFound        ToolErrorCode = "not_found"
	ToolErrorConflict        ToolErrorCode = "conflict"
	ToolErrorPermission      ToolErrorCode = "permission_denied"
	ToolErrorCapability      ToolErrorCode = "capability_unavailable"
	ToolErrorBudget          ToolErrorCode = "budget_exceeded"
	ToolErrorCancelled       ToolErrorCode = "cancelled"
	ToolErrorInternal        ToolErrorCode = "internal"
)

type ToolError struct {
	Code      ToolErrorCode
	Retryable bool
	Err       error
}

func (e *ToolError) Error() string {
	if e == nil || e.Err == nil {
		return "tool execution failed"
	}
	return e.Err.Error()
}

func (e *ToolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewToolError(code ToolErrorCode, retryable bool, err error) error {
	if err == nil {
		return nil
	}
	return &ToolError{Code: code, Retryable: retryable, Err: err}
}

type ToolSpec struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	InputSchema    map[string]any  `json:"inputSchema,omitempty"`
	OutputSchema   map[string]any  `json:"outputSchema,omitempty"`
	SideEffect     SideEffectClass `json:"sideEffect"`
	Approval       ApprovalPolicy  `json:"approval"`
	MaxResultBytes int             `json:"maxResultBytes,omitempty"`
	LongRunning    bool            `json:"longRunning,omitempty"`
	ModelCallable  bool            `json:"-"`
}

type ToolInvocation struct {
	RunID        string
	TurnID       string
	CallID       string
	WorkID       string
	SkillID      string
	SkillVersion string
	AllowedTools []string
	Args         json.RawMessage
}

type Tool interface {
	Spec() ToolSpec
	Call(context.Context, ToolInvocation) (any, error)
}

type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewToolRegistry(tools ...Tool) *ToolRegistry {
	registry := &ToolRegistry{tools: make(map[string]Tool, len(tools))}
	for _, tool := range tools {
		registry.Register(tool)
	}
	return registry
}

func (r *ToolRegistry) Register(tool Tool) {
	if tool == nil || tool.Spec().Name == "" {
		return
	}
	r.mu.Lock()
	r.tools[tool.Spec().Name] = tool
	r.mu.Unlock()
}

func (r *ToolRegistry) Specs(allowed []string) []ToolSpec {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}
	r.mu.RLock()
	specs := make([]ToolSpec, 0, len(allowedSet))
	for name := range allowedSet {
		if tool, ok := r.tools[name]; ok {
			spec := tool.Spec()
			if spec.ModelCallable {
				specs = append(specs, spec)
			}
		}
	}
	r.mu.RUnlock()
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

func (r *ToolRegistry) Snapshot(allowed []string) []Tool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}
	r.mu.RLock()
	tools := make([]Tool, 0, len(allowedSet))
	for name := range allowedSet {
		tool, ok := r.tools[name]
		if ok && tool.Spec().ModelCallable {
			tools = append(tools, tool)
		}
	}
	r.mu.RUnlock()
	sort.Slice(tools, func(i, j int) bool { return tools[i].Spec().Name < tools[j].Spec().Name })
	return tools
}

func (r *ToolRegistry) StrictSnapshot(allowed []string) ([]Tool, error) {
	tools := r.Snapshot(allowed)
	found := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		found[tool.Spec().Name] = struct{}{}
	}
	for _, name := range allowed {
		if _, ok := found[name]; !ok {
			return nil, fmt.Errorf("%w or not model-callable: %s", ErrToolNotFound, name)
		}
	}
	return tools, nil
}

func (r *ToolRegistry) Call(ctx context.Context, name string, invocation ToolInvocation) (any, error) {
	if !containsString(invocation.AllowedTools, name) {
		return nil, fmt.Errorf("%w: %s", ErrToolNotAllowed, name)
	}
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	return tool.Call(ctx, invocation)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

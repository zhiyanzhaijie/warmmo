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
	ErrToolNotFound   = errors.New("tool not found")
	ErrToolNotAllowed = errors.New("tool is not allowed by active skill")
)

type ToolSpec struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	ModelCallable bool   `json:"-"`
}

type ToolInvocation struct {
	RunID        string
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

package agenttool

import (
	"fmt"
	"sort"
	"sync"

	appharness "warmmo/core/internal/application/harness"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]appharness.Tool
}

func NewRegistry(tools ...appharness.Tool) *Registry {
	registry := &Registry{tools: make(map[string]appharness.Tool, len(tools))}
	for _, tool := range tools {
		registry.Register(tool)
	}
	return registry
}

func (r *Registry) Register(tool appharness.Tool) {
	if tool == nil || tool.Spec().Name == "" {
		return
	}
	r.mu.Lock()
	r.tools[tool.Spec().Name] = tool
	r.mu.Unlock()
}

func (r *Registry) StrictSnapshot(allowed []string) ([]appharness.Tool, error) {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = struct{}{}
	}
	r.mu.RLock()
	tools := make([]appharness.Tool, 0, len(allowedSet))
	for name := range allowedSet {
		tool, ok := r.tools[name]
		if ok && tool.Spec().ModelCallable {
			tools = append(tools, tool)
		}
	}
	r.mu.RUnlock()
	sort.Slice(tools, func(i, j int) bool { return tools[i].Spec().Name < tools[j].Spec().Name })
	found := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		found[tool.Spec().Name] = struct{}{}
	}
	for _, name := range allowed {
		if _, ok := found[name]; !ok {
			return nil, fmt.Errorf("%w or not model-callable: %s", appharness.ErrToolNotFound, name)
		}
	}
	return tools, nil
}

var _ appharness.ToolCatalog = (*Registry)(nil)

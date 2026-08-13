package agenttool

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appagent "warmmo/core/internal/application/agent"
	appharness "warmmo/core/internal/application/harness"
)

type ContextSearcher interface {
	SearchContext(context.Context, string, appagent.ContextSearchInput) ([]appagent.ContextSearchResult, error)
}

type SearchContextTool struct {
	searcher ContextSearcher
}

func NewSearchContextTool(searcher ContextSearcher) *SearchContextTool {
	return &SearchContextTool{searcher: searcher}
}

func (t *SearchContextTool) Spec() appharness.ToolSpec {
	return appharness.ToolSpec{
		Name:        "canvas.search_context",
		Description: `Search the local graph/vector context index. Returns versioned node/archive candidates with revision, scope, source, and evidence. Use canvas.get_nodes for authoritative current-node content.`,
		SideEffect:  appharness.SideEffectRead, Approval: appharness.ApprovalNever, MaxResultBytes: 48 * 1024,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":             map[string]any{"type": "string"},
				"kinds":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"scope":             map[string]any{"type": "string", "enum": []string{"current", "archive", "all"}},
				"includeStorySpine": map[string]any{"type": "boolean"},
				"seedNodeIds":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"maxRelationHops":   map[string]any{"type": "integer", "minimum": 1, "maximum": 2},
				"limit":             map[string]any{"type": "integer", "minimum": 1, "maximum": 64},
			},
			"required": []string{"query"}, "additionalProperties": false,
		},
		OutputSchema:  arrayToolResultSchema(),
		ModelCallable: true,
	}
}

func (t *SearchContextTool) Call(ctx context.Context, invocation appharness.ToolInvocation) (any, error) {
	if t.searcher == nil {
		return nil, appharness.NewToolError(appharness.ToolErrorCapability, false, errors.New("canvas context search is not configured"))
	}
	var input appagent.ContextSearchInput
	if err := decodeToolArgs(invocation.Args, &input); err != nil {
		return nil, err
	}
	input.Query = strings.TrimSpace(input.Query)
	input.Scope = strings.ToLower(strings.TrimSpace(input.Scope))
	if input.Query == "" {
		return nil, invalidContextSearchArgument("query is required")
	}
	if input.Scope == "" {
		input.Scope = "current"
	}
	if input.Scope != "current" && input.Scope != "archive" && input.Scope != "all" {
		return nil, invalidContextSearchArgument("scope must be current, archive, or all")
	}
	input.Kinds = cleanStrings(input.Kinds)
	input.SeedNodeIDs = cleanStrings(input.SeedNodeIDs)
	if input.MaxRelationHops == 0 {
		input.MaxRelationHops = 1
	}
	if input.MaxRelationHops < 1 || input.MaxRelationHops > 2 {
		return nil, invalidContextSearchArgument("maxRelationHops must be between 1 and 2")
	}
	if input.Limit == 0 {
		input.Limit = 12
	}
	if input.Limit < 1 || input.Limit > 64 {
		return nil, invalidContextSearchArgument("limit must be between 1 and 64")
	}
	results, err := t.searcher.SearchContext(ctx, invocation.WorkID, input)
	if err != nil {
		return nil, canvasToolError(err)
	}
	if len(results) > input.Limit {
		return nil, fmt.Errorf("context search returned %d results, exceeding requested limit %d", len(results), input.Limit)
	}
	for _, result := range results {
		if strings.TrimSpace(result.NodeID) == "" && strings.TrimSpace(result.ArchiveID) == "" {
			return nil, errors.New("context search result must identify a node or archive")
		}
		if strings.TrimSpace(result.NodeID) != "" &&
			(strings.TrimSpace(result.VersionID) == "" || result.Revision < 1) {
			return nil, errors.New("node context search result must include versionId and revision")
		}
	}
	return results, nil
}

func cleanStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func invalidContextSearchArgument(message string) error {
	return appharness.NewToolError(appharness.ToolErrorInvalidArgument, false, errors.New(message))
}

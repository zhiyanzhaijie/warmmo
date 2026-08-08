package canvas

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"warmnote/core/internal/agent"
)

// ContextSearchInput is the stable contract between Agent orchestration and
// the local graph/vector retrieval implementation.
type ContextSearchInput struct {
	Query           string   `json:"query"`
	Kinds           []string `json:"kinds"`
	Scope           string   `json:"scope"`
	IncludeSpine    bool     `json:"includeStorySpine"`
	SeedNodeIDs     []string `json:"seedNodeIds"`
	MaxRelationHops int      `json:"maxRelationHops"`
	Limit           int      `json:"limit"`
}

type ContextSearchResult struct {
	NodeID    string   `json:"nodeId,omitempty"`
	VersionID string   `json:"versionId,omitempty"`
	ArchiveID string   `json:"archiveId,omitempty"`
	Revision  int64    `json:"revision,omitempty"`
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	Score     float64  `json:"score"`
	Scope     string   `json:"scope"`
	Source    string   `json:"source"`
	Evidence  []string `json:"evidence,omitempty"`
}

type ContextSearcher interface {
	SearchContext(context.Context, string, ContextSearchInput) ([]ContextSearchResult, error)
}

type SearchContextTool struct {
	searcher ContextSearcher
}

func NewSearchContextTool(searcher ContextSearcher) *SearchContextTool {
	return &SearchContextTool{searcher: searcher}
}

func (t *SearchContextTool) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:          "canvas.search_context",
		Description:   `Search the local graph/vector context index. Requires an enabled embedding provider; otherwise returns a capability error. Arguments: {"query":"...","kinds":["character"],"scope":"current|archive|all","includeStorySpine":true,"seedNodeIds":["node-id"],"maxRelationHops":1,"limit":12}. Returns versioned node/archive candidates with revision, scope, source, and evidence. Use canvas.get_nodes for authoritative current-node content.`,
		ModelCallable: true,
	}
}

func (t *SearchContextTool) Call(ctx context.Context, invocation agent.ToolInvocation) (any, error) {
	if t.searcher == nil {
		return nil, errors.New("canvas context search is not configured")
	}
	var input ContextSearchInput
	if err := decodeToolArgs(invocation.Args, &input); err != nil {
		return nil, err
	}
	input.Query = strings.TrimSpace(input.Query)
	input.Scope = strings.ToLower(strings.TrimSpace(input.Scope))
	if input.Query == "" {
		return nil, errors.New("query is required")
	}
	if input.Scope == "" {
		input.Scope = "current"
	}
	if input.Scope != "current" && input.Scope != "archive" && input.Scope != "all" {
		return nil, errors.New("scope must be current, archive, or all")
	}
	input.Kinds = cleanStrings(input.Kinds)
	input.SeedNodeIDs = cleanStrings(input.SeedNodeIDs)
	if input.MaxRelationHops == 0 {
		input.MaxRelationHops = 1
	}
	if input.MaxRelationHops < 1 || input.MaxRelationHops > 2 {
		return nil, errors.New("maxRelationHops must be between 1 and 2")
	}
	if input.Limit == 0 {
		input.Limit = 12
	}
	if input.Limit < 1 || input.Limit > 64 {
		return nil, errors.New("limit must be between 1 and 64")
	}
	results, err := t.searcher.SearchContext(ctx, invocation.WorkID, input)
	if err != nil {
		return nil, err
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

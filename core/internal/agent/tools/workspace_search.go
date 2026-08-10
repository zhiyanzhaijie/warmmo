package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentcore "warmnote/core/internal/agent/core"
	"warmnote/core/internal/domain/workspace"
)

type SearchTextTool struct {
	searcher workspace.TextSearcher
}

func NewSearchTextTool(searcher workspace.TextSearcher) *SearchTextTool {
	return &SearchTextTool{searcher: searcher}
}

func (t *SearchTextTool) Spec() agentcore.ToolSpec {
	return agentcore.ToolSpec{
		Name:          "workspace.search",
		Description:   `Search text files inside the current work sandbox. Arguments: {"scope":"story-spine","query":"optional keywords","limit":8}. Scope is a server-controlled capability, never a filesystem path.`,
		ModelCallable: true,
	}
}

func (t *SearchTextTool) Call(ctx context.Context, invocation agentcore.ToolInvocation) (any, error) {
	var input struct {
		Scope string `json:"scope"`
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if len(invocation.Args) == 0 {
		invocation.Args = json.RawMessage(`{}`)
	}
	if err := decodeSearchArgs(invocation.Args, &input); err != nil {
		return nil, err
	}
	input.Scope = strings.TrimSpace(input.Scope)
	input.Query = strings.TrimSpace(input.Query)
	if input.Scope == "" {
		return nil, fmt.Errorf("scope is required")
	}
	if input.Limit == 0 {
		input.Limit = workspace.DefaultLimit
	}
	if input.Limit < 1 || input.Limit > workspace.MaxLimit {
		return nil, fmt.Errorf("limit must be between 1 and %d", workspace.MaxLimit)
	}
	return t.searcher.SearchText(ctx, invocation.WorkID, input.Scope, input.Query, input.Limit)
}

func decodeSearchArgs(data json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode workspace search arguments: %w", err)
	}
	return nil
}

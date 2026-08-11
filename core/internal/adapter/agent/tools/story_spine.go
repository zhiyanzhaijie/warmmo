package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	agentcore "warmmo/core/internal/adapter/agent/core"
	appagent "warmmo/core/internal/application/agent"
	"warmmo/core/internal/domain/workspace"
)

const (
	defaultStorySpineSearchLimit = 8
	maxStorySpineSearchLimit     = 20
)

type StorySpineDatabase interface {
	SearchStorySpineDatabase(context.Context, string, string, int) ([]appagent.StorySpineSearchResult, error)
}

type SearchStorySpineTool struct {
	files    workspace.TextSearcher
	database StorySpineDatabase
}

func NewSearchStorySpineTool(files workspace.TextSearcher, database StorySpineDatabase) *SearchStorySpineTool {
	return &SearchStorySpineTool{files: files, database: database}
}

func (t *SearchStorySpineTool) Spec() agentcore.ToolSpec {
	return agentcore.ToolSpec{
		Name:        "story_spine.context",
		Description: `Retrieve compact archived story context. Uses the sandboxed workspace text search first and queries the story-spine database only after zero file matches.`,
		SideEffect:  agentcore.SideEffectRead, Approval: agentcore.ApprovalNever, MaxResultBytes: 32 * 1024,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": maxStorySpineSearchLimit},
			},
			"additionalProperties": false,
		},
		OutputSchema:  arrayToolResultSchema(),
		ModelCallable: true,
	}
}

func (t *SearchStorySpineTool) Call(ctx context.Context, invocation agentcore.ToolInvocation) (any, error) {
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if len(invocation.Args) == 0 {
		invocation.Args = json.RawMessage(`{}`)
	}
	if err := decodeToolArgs(invocation.Args, &input); err != nil {
		return nil, err
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Limit == 0 {
		input.Limit = defaultStorySpineSearchLimit
	}
	if input.Limit < 1 || input.Limit > maxStorySpineSearchLimit {
		return nil, agentcore.NewToolError(agentcore.ToolErrorInvalidArgument, false, fmt.Errorf("limit must be between 1 and %d", maxStorySpineSearchLimit))
	}
	fileResults, err := t.files.SearchText(ctx, invocation.WorkID, workspace.ScopeStorySpine, input.Query, input.Limit)
	if err == nil && len(fileResults) > 0 {
		results := make([]appagent.StorySpineSearchResult, 0, len(fileResults))
		for index, result := range fileResults {
			results = append(results, appagent.StorySpineSearchResult{
				ChapterOutlineNodeID: strings.TrimSuffix(filepath.Base(result.Path), filepath.Ext(result.Path)),
				Snippet:              result.Snippet,
				Source:               result.Source,
				Path:                 result.Path,
				ContextRole:          "completed-chapter",
				RecencyRank:          index + 1,
			})
		}
		return results, nil
	}
	return t.database.SearchStorySpineDatabase(ctx, invocation.WorkID, input.Query, input.Limit)
}

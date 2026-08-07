package canvas

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"warmnote/core/internal/agent"
	"warmnote/core/internal/workspace"
)

const (
	defaultStorySpineSearchLimit = 8
	maxStorySpineSearchLimit     = 20
)

type StorySpineSearchResult struct {
	ArchiveID            string `json:"archiveId"`
	ChapterOutlineNodeID string `json:"chapterOutlineNodeId"`
	Title                string `json:"title"`
	Snippet              string `json:"snippet"`
	Source               string `json:"source"`
	Path                 string `json:"path,omitempty"`
	ContextRole          string `json:"contextRole"`
	RecencyRank          int    `json:"recencyRank"`
}

type StorySpineDatabase interface {
	SearchStorySpineDatabase(context.Context, string, string, int) ([]StorySpineSearchResult, error)
}

type SearchStorySpineTool struct {
	files    workspace.TextSearcher
	database StorySpineDatabase
}

func NewSearchStorySpineTool(files workspace.TextSearcher, database StorySpineDatabase) *SearchStorySpineTool {
	return &SearchStorySpineTool{files: files, database: database}
}

func (t *SearchStorySpineTool) Spec() agent.ToolSpec {
	return agent.ToolSpec{
		Name:          "story_spine.context",
		Description:   `Retrieve compact archived story context. Arguments: {"query":"optional keywords","limit":8}. Uses the sandboxed workspace text search first and queries the story-spine database only after zero file matches.`,
		ModelCallable: true,
	}
}

func (t *SearchStorySpineTool) Call(ctx context.Context, invocation agent.ToolInvocation) (any, error) {
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
		return nil, fmt.Errorf("limit must be between 1 and %d", maxStorySpineSearchLimit)
	}
	fileResults, err := t.files.SearchText(ctx, invocation.WorkID, workspace.ScopeStorySpine, input.Query, input.Limit)
	if err == nil && len(fileResults) > 0 {
		results := make([]StorySpineSearchResult, 0, len(fileResults))
		for index, result := range fileResults {
			results = append(results, StorySpineSearchResult{
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

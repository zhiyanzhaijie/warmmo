package workspace

import "context"

const (
	ScopeStorySpine = "story-spine"
	DefaultLimit    = 8
	MaxLimit        = 20
)

type SearchResult struct {
	Path    string `json:"path"`
	Snippet string `json:"snippet"`
	Source  string `json:"source"`
}

type TextSearcher interface {
	SearchText(context.Context, string, string, string, int) ([]SearchResult, error)
}

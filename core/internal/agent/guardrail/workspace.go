package guardrail

import (
	"fmt"
	"path/filepath"
	"strings"

	"warmnote/core/internal/shared/safepath"
	"warmnote/core/internal/workspace"
)

type WorkSearchBoundary struct {
	WorkRoot   string
	SearchRoot string
	FileGlob   string
}

type workSearchPolicy struct {
	relativeDirectory string
	fileGlob          string
}

var workSearchPolicies = map[string]workSearchPolicy{
	workspace.ScopeStorySpine: {
		relativeDirectory: filepath.Join("story-spine", "chapters"),
		fileGlob:          "*.md",
	},
}

func ResolveWorkSearchBoundary(dataDirectory, workID, scope string) (WorkSearchBoundary, error) {
	policy, exists := workSearchPolicies[scope]
	if !exists {
		return WorkSearchBoundary{}, fmt.Errorf("unsupported workspace search scope %q", scope)
	}
	workRoot := filepath.Join(dataDirectory, "works", safepath.Component(workID))
	return WorkSearchBoundary{
		WorkRoot:   workRoot,
		SearchRoot: filepath.Join(workRoot, policy.relativeDirectory),
		FileGlob:   policy.fileGlob,
	}, nil
}

func PathWithinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

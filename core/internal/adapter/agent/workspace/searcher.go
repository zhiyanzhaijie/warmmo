package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"warmmo/core/internal/adapter/agent/guardrail"
	domainworkspace "warmmo/core/internal/domain/workspace"
)

type Searcher struct {
	dataDirectory string
}

func NewSearcher(dataDirectory string) *Searcher {
	return &Searcher{dataDirectory: dataDirectory}
}

func (s *Searcher) SearchText(ctx context.Context, workID, scope, query string, limit int) ([]domainworkspace.SearchResult, error) {
	boundary, err := guardrail.ResolveWorkSearchBoundary(s.dataDirectory, workID, scope)
	if err != nil {
		return nil, err
	}
	if rgPath, err := resolveRGPath(); err == nil {
		if results, searchErr := searchTextWithRG(ctx, rgPath, boundary.WorkRoot, boundary.SearchRoot, boundary.FileGlob, query, limit); searchErr == nil {
			return results, nil
		}
	}
	return searchTextWithGo(boundary.WorkRoot, boundary.SearchRoot, boundary.FileGlob, query, limit)
}

func resolveRGPath() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("WARMMO_RG_PATH")); configured != "" {
		return configured, nil
	}
	return exec.LookPath("rg")
}

func searchTextWithRG(ctx context.Context, rgPath, workRoot, searchRoot, fileGlob, query string, limit int) ([]domainworkspace.SearchResult, error) {
	args := []string{"--hidden", "--glob", fileGlob}
	if query == "" {
		args = append(args, "--files", searchRoot)
	} else {
		args = append(args, "--fixed-strings", "--ignore-case", "--files-with-matches", "--", query, searchRoot)
	}
	output, err := exec.CommandContext(ctx, rgPath, args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return []domainworkspace.SearchResult{}, nil
		}
		return nil, err
	}
	return readSearchResults(workRoot, strings.Split(strings.TrimSpace(string(output)), "\n"), query, limit, "rg")
}

func searchTextWithGo(workRoot, searchRoot, fileGlob, query string, limit int) ([]domainworkspace.SearchResult, error) {
	entries, err := os.ReadDir(searchRoot)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		matched, matchErr := filepath.Match(fileGlob, entry.Name())
		if matchErr == nil && matched && !entry.IsDir() {
			paths = append(paths, filepath.Join(searchRoot, entry.Name()))
		}
	}
	return readSearchResults(workRoot, paths, query, limit, "go-fallback")
}

func readSearchResults(workRoot string, paths []string, query string, limit int, source string) ([]domainworkspace.SearchResult, error) {
	type searchFile struct {
		path    string
		modTime time.Time
	}
	resolvedWorkRoot, err := filepath.EvalSymlinks(workRoot)
	if err != nil {
		return nil, err
	}
	files := make([]searchFile, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		resolvedPath, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil || !guardrail.PathWithinRoot(resolvedWorkRoot, resolvedPath) {
			continue
		}
		info, statErr := os.Stat(resolvedPath)
		if statErr == nil {
			files = append(files, searchFile{path: resolvedPath, modTime: info.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	normalizedQuery := strings.ToLower(strings.TrimSpace(query))
	results := make([]domainworkspace.SearchResult, 0, min(limit, len(files)))
	for _, file := range files {
		content, err := os.ReadFile(file.path)
		if err != nil {
			continue
		}
		text := string(content)
		if source == "go-fallback" && normalizedQuery != "" && !strings.Contains(strings.ToLower(text), normalizedQuery) {
			continue
		}
		relativePath, err := filepath.Rel(resolvedWorkRoot, file.path)
		if err != nil || strings.HasPrefix(relativePath, "..") {
			continue
		}
		results = append(results, domainworkspace.SearchResult{
			Path:    filepath.ToSlash(relativePath),
			Snippet: textSnippet(text, normalizedQuery, 900),
			Source:  source,
		})
		if len(results) == limit {
			break
		}
	}
	return results, nil
}

func textSnippet(content, normalizedQuery string, limit int) string {
	content = strings.TrimSpace(content)
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	start := 0
	if normalizedQuery != "" {
		if index := strings.Index(strings.ToLower(content), normalizedQuery); index >= 0 {
			start = max(0, utf8.RuneCountInString(content[:index])-limit/3)
		}
	}
	end := min(len(runes), start+limit)
	return strings.TrimSpace(string(runes[start:end])) + "…"
}

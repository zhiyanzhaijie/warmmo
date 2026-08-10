package writing

import (
	"context"
	"testing"
)

func TestBuiltInCatalogSupportsChapterSectionNodeLifecycle(t *testing.T) {
	t.Parallel()

	catalog, err := LoadCatalog("../../../skills")
	if err != nil {
		t.Fatalf("load built-in skill catalog: %v", err)
	}
	assertSingleSkillMatch(t, catalog, "chapter-section", "chapter-section-writing")
	assertSingleSkillMatch(t, catalog, "section-outline-batch", "chapter-section-planning")
	assertSingleSkillMatch(t, catalog, "node-update:section-outline", "section-outline-update")
	assertSingleSkillMatch(t, catalog, "node-update:chapter-section", "chapter-section-update")
}

func assertSingleSkillMatch(t *testing.T, catalog *Catalog, target, expectedID string) {
	t.Helper()
	matches, err := catalog.Search(context.Background(), target, "")
	if err != nil {
		t.Fatalf("search target %q: %v", target, err)
	}
	if len(matches) != 1 || matches[0].ID != expectedID {
		t.Fatalf("target %q matches = %+v, want %q", target, matches, expectedID)
	}
}

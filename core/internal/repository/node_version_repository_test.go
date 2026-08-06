package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"warmnote/core/internal/agent"
	"warmnote/core/internal/canvas"
)

func TestChapterArchiveCandidateCreatesNodeVersion(t *testing.T) {
	dataDirectory := t.TempDir()
	provider, err := NewProviderRepository(dataDirectory)
	if err != nil {
		t.Fatalf("create provider repository: %v", err)
	}
	t.Cleanup(func() {
		if err := provider.Close(); err != nil {
			t.Errorf("close provider repository: %v", err)
		}
	})
	ctx := context.Background()
	canvasRepository := NewCanvasRepository(provider)
	character, err := canvasRepository.CreateNode(ctx, canvas.CreateNodeInput{WorkID: "work-version", Kind: canvas.NodeKindCharacter, Title: "角色 A", Content: "初始状态"})
	if err != nil {
		t.Fatalf("create character: %v", err)
	}
	chapter, err := canvasRepository.CreateNode(ctx, canvas.CreateNodeInput{WorkID: "work-version", Kind: canvas.NodeKindChapterOutline, Title: "第一章", Content: "角色 A 经历事件", ContextNodeIDs: []string{character.ID}})
	if err != nil {
		t.Fatalf("create chapter: %v", err)
	}
	sectionOutline, err := canvasRepository.CreateNode(ctx, canvas.CreateNodeInput{WorkID: "work-version", Kind: canvas.NodeKindSectionOutline, Title: "小节规划", Content: "规划", ContextNodeIDs: []string{chapter.ID}})
	if err != nil {
		t.Fatalf("create section outline: %v", err)
	}
	section, err := canvasRepository.CreateNode(ctx, canvas.CreateNodeInput{WorkID: "work-version", Kind: canvas.NodeKindChapterSection, Title: "小节正文", Content: "正文", ContextNodeIDs: []string{sectionOutline.ID, chapter.ID, character.ID}})
	if err != nil {
		t.Fatalf("create chapter section: %v", err)
	}
	agentRepository := NewAgentRepository(provider)
	archiveContext, err := agentRepository.GetChapterArchiveContext(character.WorkID, chapter.ID)
	if err != nil {
		t.Fatalf("resolve chapter archive context: %v", err)
	}
	for _, expectedID := range []string{chapter.ID, character.ID, sectionOutline.ID, section.ID} {
		if !containsString(archiveContext, expectedID) {
			t.Fatalf("archive context %v does not contain %s", archiveContext, expectedID)
		}
	}
	updated, err := canvasRepository.UpdateNode(ctx, canvas.UpdateNodeInput{WorkID: character.WorkID, NodeID: character.ID, Title: character.Title, Content: "直接更新", ExpectedRevision: character.Revision})
	if err != nil {
		t.Fatalf("direct node update: %v", err)
	}
	versions, err := canvasRepository.ListNodeVersions(ctx, character.WorkID, character.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("direct update versions = %d, err = %v", len(versions), err)
	}

	run, err := agentRepository.CreateRun(agent.RunInput{RunID: "archive-run", WorkID: character.WorkID, Prompt: "归档", Target: agent.TargetChapterArchive, TargetNodeID: chapter.ID, ProviderID: "provider", ModelID: "model", ContextNodeIDs: []string{chapter.ID, character.ID}})
	if err != nil {
		t.Fatalf("create archive run: %v", err)
	}
	if err := agentRepository.MarkStarted(run.ID); err != nil {
		t.Fatalf("start archive run: %v", err)
	}
	result := agent.RunResult{SkillID: "chapter-archive", SkillVersion: "1.0.0", ExpectedRevision: chapter.Revision, Content: `{"archive":{"summary":"角色 A 经历了本章事件","sections":[{"sectionOutlineNodeId":"` + sectionOutline.ID + `","chapterSectionNodeId":"` + section.ID + `","nodeRevision":1,"ordinal":1,"summary":"角色 A 经历事件"}]},"proposals":[{"nodeId":"` + character.ID + `","kind":"character","title":"角色 A","content":"事件后的状态","changeScore":8,"reason":"本章事件改变认知"}]}`}
	if err := agentRepository.CompleteChapterArchive(ctx, run, result); err != nil {
		t.Fatalf("complete archive: %v", err)
	}
	archives, err := canvasRepository.ListCurrentChapterArchives(ctx, character.WorkID)
	if err != nil || len(archives) != 1 {
		t.Fatalf("current archives = %d, err = %v", len(archives), err)
	}
	firstArchive := archives[0]
	if firstArchive.Revision != 1 || firstArchive.OutlineContent != chapter.Content || firstArchive.ProjectionStatus != "ready" {
		t.Fatalf("first archive = %+v", firstArchive)
	}
	if len(firstArchive.Sections) != 1 || firstArchive.Sections[0].Content != section.Content || firstArchive.Sections[0].ChapterSectionVersionID == "" {
		t.Fatalf("first archive sections = %+v", firstArchive.Sections)
	}
	projectionPath := filepath.Join(dataDirectory, "works", character.WorkID, "story-spine", "chapters", chapter.ID+".md")
	projection, err := os.ReadFile(projectionPath)
	if err != nil {
		t.Fatalf("read chapter projection: %v", err)
	}
	if !strings.Contains(string(projection), "archiveRevision: 1") || !strings.Contains(string(projection), firstArchive.SourceDigest) {
		t.Fatalf("unexpected chapter projection:\n%s", projection)
	}
	candidates, err := canvasRepository.ListCandidates(ctx, character.WorkID)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("archive candidates = %d, err = %v", len(candidates), err)
	}
	accepted, err := canvasRepository.AcceptCandidate(ctx, canvas.AcceptCandidateInput{WorkID: character.WorkID, CandidateID: candidates[0].ID})
	if err != nil {
		t.Fatalf("accept version candidate: %v", err)
	}
	if accepted.ID != character.ID || accepted.Content != "事件后的状态" {
		t.Fatalf("accepted node = %+v", accepted)
	}
	versions, err = canvasRepository.ListNodeVersions(ctx, character.WorkID, character.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("accepted versions = %d, err = %v", len(versions), err)
	}
	if updated.ID != accepted.ID {
		t.Fatalf("node identity changed: %s != %s", updated.ID, accepted.ID)
	}

	updatedChapter, err := canvasRepository.UpdateNode(ctx, canvas.UpdateNodeInput{
		WorkID: chapter.WorkID, NodeID: chapter.ID, Title: chapter.Title,
		Content: "重写后的章节规划", ExpectedRevision: chapter.Revision,
	})
	if err != nil {
		t.Fatalf("update chapter outline: %v", err)
	}
	updatedSection, err := canvasRepository.UpdateNode(ctx, canvas.UpdateNodeInput{
		WorkID: section.WorkID, NodeID: section.ID, Title: section.Title,
		Content: "重写后的正文", ExpectedRevision: section.Revision,
	})
	if err != nil {
		t.Fatalf("update chapter section: %v", err)
	}
	secondRun, err := agentRepository.CreateRun(agent.RunInput{
		RunID: "archive-run-2", WorkID: character.WorkID, Prompt: "重新归档",
		Target: agent.TargetChapterArchive, TargetNodeID: chapter.ID,
		ProviderID: "provider", ModelID: "model", ContextNodeIDs: archiveContext,
	})
	if err != nil {
		t.Fatalf("create second archive run: %v", err)
	}
	if err := agentRepository.MarkStarted(secondRun.ID); err != nil {
		t.Fatalf("start second archive run: %v", err)
	}
	secondResult := agent.RunResult{SkillID: "chapter-archive", SkillVersion: "1.0.0", ExpectedRevision: updatedChapter.Revision, Content: `{"archive":{"summary":"重写后的章节事实","sections":[{"sectionOutlineNodeId":"` + sectionOutline.ID + `","chapterSectionNodeId":"` + section.ID + `","nodeRevision":2,"ordinal":1,"summary":"重写后的小节事实"}]},"proposals":[]}`}
	if err := agentRepository.CompleteChapterArchive(ctx, secondRun, secondResult); err != nil {
		t.Fatalf("complete second archive: %v", err)
	}

	history, err := canvasRepository.ListChapterArchiveHistory(ctx, character.WorkID, chapter.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("archive history = %d, err = %v", len(history), err)
	}
	if history[0].IsCurrent || history[0].Revision != 1 || history[0].OutlineContent != chapter.Content || history[0].Sections[0].Content != section.Content {
		t.Fatalf("immutable first archive changed: %+v", history[0])
	}
	if !history[1].IsCurrent || history[1].Revision != 2 || history[1].OutlineContent != updatedChapter.Content || history[1].Sections[0].Content != updatedSection.Content {
		t.Fatalf("second archive = %+v", history[1])
	}
	archives, err = canvasRepository.ListCurrentChapterArchives(ctx, character.WorkID)
	if err != nil || len(archives) != 1 || archives[0].ID != history[1].ID {
		t.Fatalf("latest current archives = %+v, err = %v", archives, err)
	}
	projection, err = os.ReadFile(projectionPath)
	if err != nil {
		t.Fatalf("read updated chapter projection: %v", err)
	}
	if !strings.Contains(string(projection), "archiveRevision: 2") || !strings.Contains(string(projection), "重写后的章节事实") {
		t.Fatalf("chapter projection was not replaced:\n%s", projection)
	}
	if err := os.WriteFile(projectionPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("make chapter projection stale: %v", err)
	}
	archives, err = canvasRepository.ListCurrentChapterArchives(ctx, character.WorkID)
	if err != nil || len(archives) != 1 || archives[0].ProjectionStatus != "ready" {
		t.Fatalf("repair current archive projection: archives = %+v, err = %v", archives, err)
	}
	projection, err = os.ReadFile(projectionPath)
	if err != nil {
		t.Fatalf("read repaired chapter projection: %v", err)
	}
	if string(projection) == "stale" || !strings.Contains(string(projection), "archiveRevision: 2") {
		t.Fatalf("stale chapter projection was not repaired:\n%s", projection)
	}
}

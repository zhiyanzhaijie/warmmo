package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agent "warmmo/core/internal/agent/writing"
	"warmmo/core/internal/domain/canvas"
	"warmmo/core/internal/shared/pagination"
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
	historyState, err := canvasRepository.GetHistoryState(ctx, character.WorkID)
	if err != nil || historyState.CanUndo || historyState.CanRedo {
		t.Fatalf("archive history checkpoint = %+v, err = %v", historyState, err)
	}
	archives, err := canvasRepository.ListCurrentChapterArchives(ctx, character.WorkID)
	if err != nil || len(archives) != 1 {
		t.Fatalf("current archives = %d, err = %v", len(archives), err)
	}
	pageRequest, err := pagination.New(1, 1)
	if err != nil {
		t.Fatalf("create archive page request: %v", err)
	}
	archivePage, err := canvasRepository.ListCurrentChapterArchivesPage(ctx, character.WorkID, pageRequest)
	if err != nil {
		t.Fatalf("list current archive page: %v", err)
	}
	if len(archivePage.Items) != 1 || archivePage.Pagination.Total != 1 || archivePage.Pagination.TotalPages != 1 {
		t.Fatalf("current archive page = %+v", archivePage)
	}
	firstArchive := archives[0]
	if firstArchive.Revision != 1 || firstArchive.OutlineContent != chapter.Content || firstArchive.ProjectionStatus != "ready" {
		t.Fatalf("first archive = %+v", firstArchive)
	}
	if len(firstArchive.Sections) != 1 || firstArchive.Sections[0].Content != section.Content || firstArchive.Sections[0].ChapterSectionVersionID == "" {
		t.Fatalf("first archive sections = %+v", firstArchive.Sections)
	}
	layoutChapter, err := canvasRepository.GetNode(ctx, character.WorkID, chapter.ID)
	if err != nil {
		t.Fatalf("read archived chapter layout origin: %v", err)
	}
	layoutSectionOutline, err := canvasRepository.GetNode(ctx, character.WorkID, sectionOutline.ID)
	if err != nil {
		t.Fatalf("read archived section outline layout: %v", err)
	}
	layoutSection, err := canvasRepository.GetNode(ctx, character.WorkID, section.ID)
	if err != nil {
		t.Fatalf("read archived chapter section layout: %v", err)
	}
	if layoutChapter.X != chapter.X || layoutChapter.Y != chapter.Y {
		t.Fatalf("chapter outline moved during archive: got (%v,%v), want (%v,%v)", layoutChapter.X, layoutChapter.Y, chapter.X, chapter.Y)
	}
	if layoutSectionOutline.X != chapter.X+chapterLayoutColumnGap || layoutSectionOutline.Y != chapter.Y {
		t.Fatalf("section outline layout = (%v,%v)", layoutSectionOutline.X, layoutSectionOutline.Y)
	}
	if layoutSection.X != chapter.X+2*chapterLayoutColumnGap || layoutSection.Y != chapter.Y {
		t.Fatalf("chapter section layout = (%v,%v)", layoutSection.X, layoutSection.Y)
	}
	if err := canvasRepository.UpdateNodePositions(ctx, character.WorkID, []canvas.NodePosition{
		{NodeID: sectionOutline.ID, X: -10, Y: -20},
		{NodeID: section.ID, X: -30, Y: -40},
	}); err != nil {
		t.Fatalf("move archived chapter layout nodes: %v", err)
	}
	layoutPositions, err := canvasRepository.LayoutChapter(ctx, character.WorkID, chapter.ID)
	if err != nil || len(layoutPositions) != 2 {
		t.Fatalf("layout archived chapter positions = %+v, err = %v", layoutPositions, err)
	}
	layoutPositions, err = canvasRepository.LayoutChapter(ctx, character.WorkID, chapter.ID)
	if err != nil || len(layoutPositions) != 0 {
		t.Fatalf("idempotent archived chapter layout positions = %+v, err = %v", layoutPositions, err)
	}
	layoutHistory, err := canvasRepository.ListChapterArchiveHistory(ctx, character.WorkID, chapter.ID)
	if err != nil || len(layoutHistory) != 1 || layoutHistory[0].OutlineContent != chapter.Content || layoutHistory[0].Sections[0].Content != section.Content {
		t.Fatalf("layout changed immutable archive: %+v, err = %v", layoutHistory, err)
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

	_, err = canvasRepository.UpdateNode(ctx, canvas.UpdateNodeInput{
		WorkID: chapter.WorkID, NodeID: chapter.ID, Title: chapter.Title,
		Content: "重写后的章节规划", ExpectedRevision: chapter.Revision,
	})
	if !errors.Is(err, canvas.ErrArchivedNodeLocked) {
		t.Fatalf("update archived chapter outline error = %v", err)
	}
	_, err = canvasRepository.UpdateNode(ctx, canvas.UpdateNodeInput{
		WorkID: section.WorkID, NodeID: section.ID, Title: section.Title,
		Content: "重写后的正文", ExpectedRevision: section.Revision,
	})
	if !errors.Is(err, canvas.ErrArchivedNodeLocked) {
		t.Fatalf("update archived chapter section error = %v", err)
	}
	_, err = canvasRepository.SwitchNodeVersion(ctx, section.WorkID, section.ID, firstArchive.Sections[0].ChapterSectionVersionID)
	if !errors.Is(err, canvas.ErrArchivedNodeLocked) {
		t.Fatalf("switch archived chapter section version error = %v", err)
	}
	if err := canvasRepository.DeleteNodes(ctx, chapter.WorkID, []string{section.ID}); !errors.Is(err, canvas.ErrArchivedNodeLocked) {
		t.Fatalf("delete archived chapter section error = %v", err)
	}
	outgoingEdge, err := canvasRepository.CreateEdge(ctx, canvas.CreateEdgeInput{
		WorkID: chapter.WorkID, SourceNodeID: chapter.ID, TargetNodeID: character.ID,
	})
	if err != nil {
		t.Fatalf("create edge from archived chapter: %v", err)
	}
	if err := canvasRepository.DeleteEdges(ctx, chapter.WorkID, []string{outgoingEdge.ID}); err != nil {
		t.Fatalf("delete edge from archived chapter: %v", err)
	}
	edges, err := canvasRepository.ListEdges(ctx, chapter.WorkID)
	if err != nil {
		t.Fatalf("list archived chapter edges: %v", err)
	}
	var archivedEdgeID string
	for _, edge := range edges {
		if edge.TargetNodeID == chapter.ID || edge.TargetNodeID == sectionOutline.ID || edge.TargetNodeID == section.ID {
			archivedEdgeID = edge.ID
			break
		}
	}
	if archivedEdgeID == "" {
		t.Fatal("archived chapter has no edge to verify deletion lock")
	}
	if err := canvasRepository.DeleteEdges(ctx, chapter.WorkID, []string{archivedEdgeID}); !errors.Is(err, canvas.ErrArchivedNodeLocked) {
		t.Fatalf("delete archived chapter edge error = %v", err)
	}
	if err := agentRepository.CompleteNodeUpdate(ctx, agent.Run{WorkID: chapter.WorkID}, section.ID, agent.RunResult{
		ExpectedRevision: section.Revision, Title: section.Title, Content: "Agent 重写后的正文",
	}); !errors.Is(err, canvas.ErrArchivedNodeLocked) {
		t.Fatalf("agent update archived chapter section error = %v", err)
	}
	if err := agentRepository.CompleteChapterArchive(ctx, run, result); !errors.Is(err, canvas.ErrArchivedNodeLocked) {
		t.Fatalf("repeat chapter archive error = %v", err)
	}

	history, err := canvasRepository.ListChapterArchiveHistory(ctx, character.WorkID, chapter.ID)
	if err != nil || len(history) != 1 {
		t.Fatalf("archive history = %d, err = %v", len(history), err)
	}
	if !history[0].IsCurrent || history[0].Revision != 1 || history[0].OutlineContent != chapter.Content || history[0].Sections[0].Content != section.Content {
		t.Fatalf("immutable archive changed: %+v", history[0])
	}
	archives, err = canvasRepository.ListCurrentChapterArchives(ctx, character.WorkID)
	if err != nil || len(archives) != 1 || archives[0].ID != history[0].ID {
		t.Fatalf("latest current archives = %+v, err = %v", archives, err)
	}
	projection, err = os.ReadFile(projectionPath)
	if err != nil {
		t.Fatalf("read updated chapter projection: %v", err)
	}
	if !strings.Contains(string(projection), "archiveRevision: 1") || !strings.Contains(string(projection), "角色 A 经历了本章事件") {
		t.Fatalf("chapter projection changed unexpectedly:\n%s", projection)
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
	if string(projection) == "stale" || !strings.Contains(string(projection), "archiveRevision: 1") {
		t.Fatalf("stale chapter projection was not repaired:\n%s", projection)
	}
}

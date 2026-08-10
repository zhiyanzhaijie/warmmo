package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	agent "warmnote/core/internal/agent/writing"
	"warmnote/core/internal/domain/canvas"
)

func TestCompleteSectionOutlineBatchCreatesUndoableNodesAndEdges(t *testing.T) {
	t.Parallel()

	providerRepository, err := NewProviderRepository(t.TempDir())
	if err != nil {
		t.Fatalf("create provider repository: %v", err)
	}
	t.Cleanup(func() {
		if err := providerRepository.Close(); err != nil {
			t.Errorf("close provider repository: %v", err)
		}
	})

	const (
		workID    = "work-derive"
		runID     = "run-derive"
		chapterID = "chapter-outline-1"
		worldID   = "world-1"
	)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, node := range []struct {
		id, kind, title string
	}{
		{id: chapterID, kind: string(canvas.NodeKindChapterOutline), title: "第一章"},
		{id: worldID, kind: string(canvas.NodeKindWorld), title: "世界观"},
	} {
		if _, err := providerRepository.database.Exec(`
INSERT INTO canvas_nodes (id, work_id, revision, kind, title, content, x, y, created_at, updated_at)
VALUES (?, ?, 1, ?, ?, '', 100, 200, ?, ?)`, node.id, workID, node.kind, node.title, now, now); err != nil {
			t.Fatalf("insert canvas node: %v", err)
		}
	}
	contextNodeIDs, err := json.Marshal([]string{chapterID, worldID})
	if err != nil {
		t.Fatalf("encode context node ids: %v", err)
	}
	if _, err := providerRepository.database.Exec(`
INSERT INTO agent_runs (
    id, work_id, status, prompt, target, target_node_id, provider_id, model_id,
    context_node_ids_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, workID, agent.RunStatusRunning, "拆分章节", agent.TargetSectionOutlineBatch,
		chapterID, "provider-1", "model-1", string(contextNodeIDs), now, now); err != nil {
		t.Fatalf("insert agent run: %v", err)
	}
	batch := canvas.SectionOutlineBatch{
		ChapterOutlineNodeID: chapterID,
		Sections: []canvas.PlannedSection{
			{Title: "抵达", Outline: testSectionOutline(1)},
			{Title: "发现", Outline: testSectionOutline(2)},
		},
	}
	content, err := json.Marshal(batch)
	if err != nil {
		t.Fatalf("encode section outline batch: %v", err)
	}
	repository := NewAgentRepository(providerRepository)
	if err := repository.CompleteDerivation(context.Background(), agent.Run{
		ID: runID, WorkID: workID, Target: agent.TargetSectionOutlineBatch,
		ContextNodeIDs: []string{chapterID, worldID},
	}, chapterID, agent.RunResult{Content: string(content), ExpectedRevision: 1}); err != nil {
		t.Fatalf("complete derivation: %v", err)
	}

	assertTableCount(t, providerRepository, `SELECT COUNT(*) FROM canvas_nodes WHERE work_id = ? AND kind = ?`, 2, workID, canvas.NodeKindSectionOutline)
	assertTableCount(t, providerRepository, `SELECT COUNT(*) FROM canvas_edges WHERE work_id = ?`, 2, workID)
	if _, err := NewCanvasRepository(providerRepository).Undo(context.Background(), workID); err != nil {
		t.Fatalf("undo derivation: %v", err)
	}
	assertTableCount(t, providerRepository, `SELECT COUNT(*) FROM canvas_nodes WHERE work_id = ? AND kind = ?`, 0, workID, canvas.NodeKindSectionOutline)
	assertTableCount(t, providerRepository, `SELECT COUNT(*) FROM canvas_edges WHERE work_id = ?`, 0, workID)
}

func TestGetNodeAttachmentsReturnsDirectIncomingNodes(t *testing.T) {
	t.Parallel()

	providerRepository, err := NewProviderRepository(t.TempDir())
	if err != nil {
		t.Fatalf("create provider repository: %v", err)
	}
	t.Cleanup(func() {
		if err := providerRepository.Close(); err != nil {
			t.Errorf("close provider repository: %v", err)
		}
	})

	const workID = "work-attachments"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, node := range []struct {
		id, kind, title string
	}{
		{id: "target", kind: string(canvas.NodeKindCharacter), title: "目标角色"},
		{id: "world", kind: string(canvas.NodeKindWorld), title: "世界观"},
		{id: "location", kind: string(canvas.NodeKindLocation), title: "宴会厅"},
		{id: "child", kind: string(canvas.NodeKindEvent), title: "后续事件"},
	} {
		if _, err := providerRepository.database.Exec(`
INSERT INTO canvas_nodes (id, work_id, revision, kind, title, content, x, y, created_at, updated_at)
VALUES (?, ?, 1, ?, ?, '', 0, 0, ?, ?)`, node.id, workID, node.kind, node.title, now, now); err != nil {
			t.Fatalf("insert canvas node: %v", err)
		}
	}
	for _, edge := range []struct {
		id, sourceNodeID, targetNodeID string
	}{
		{id: "edge-world", sourceNodeID: "world", targetNodeID: "target"},
		{id: "edge-location", sourceNodeID: "location", targetNodeID: "target"},
		{id: "edge-child", sourceNodeID: "target", targetNodeID: "child"},
	} {
		if _, err := providerRepository.database.Exec(`
INSERT INTO canvas_edges (id, work_id, source_node_id, target_node_id, kind, created_at)
VALUES (?, ?, ?, ?, 'generated_from', ?)`, edge.id, workID, edge.sourceNodeID, edge.targetNodeID, now); err != nil {
			t.Fatalf("insert canvas edge: %v", err)
		}
	}

	attachments, err := NewAgentRepository(providerRepository).GetNodeAttachments(workID, "target")
	if err != nil {
		t.Fatalf("get node attachments: %v", err)
	}
	want := []agent.NodeReference{
		{ID: "location", Type: string(canvas.NodeKindLocation)},
		{ID: "world", Type: string(canvas.NodeKindWorld)},
	}
	if len(attachments) != len(want) {
		t.Fatalf("attachments = %+v, want %+v", attachments, want)
	}
	for index := range want {
		if attachments[index] != want[index] {
			t.Fatalf("attachments = %+v, want %+v", attachments, want)
		}
	}
}

func TestCompleteChapterSectionConnectsInheritedWritingContext(t *testing.T) {
	t.Parallel()

	providerRepository, err := NewProviderRepository(t.TempDir())
	if err != nil {
		t.Fatalf("create provider repository: %v", err)
	}
	t.Cleanup(func() {
		if err := providerRepository.Close(); err != nil {
			t.Errorf("close provider repository: %v", err)
		}
	})

	const (
		workID    = "work-writing"
		runID     = "run-writing"
		sectionID = "section-outline-1"
		chapterID = "chapter-outline-1"
		worldID   = "world-1"
	)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, node := range []struct {
		id, kind, title string
	}{
		{id: sectionID, kind: string(canvas.NodeKindSectionOutline), title: "宴会"},
		{id: chapterID, kind: string(canvas.NodeKindChapterOutline), title: "第一章"},
		{id: worldID, kind: string(canvas.NodeKindWorld), title: "世界观"},
	} {
		if _, err := providerRepository.database.Exec(`
INSERT INTO canvas_nodes (id, work_id, revision, kind, title, content, x, y, created_at, updated_at)
VALUES (?, ?, 1, ?, ?, '', 100, 200, ?, ?)`, node.id, workID, node.kind, node.title, now, now); err != nil {
			t.Fatalf("insert canvas node: %v", err)
		}
	}
	for _, edge := range []struct {
		id, sourceNodeID, targetNodeID string
	}{
		{id: "edge-world-chapter", sourceNodeID: worldID, targetNodeID: chapterID},
		{id: "edge-chapter-section", sourceNodeID: chapterID, targetNodeID: sectionID},
	} {
		if _, err := providerRepository.database.Exec(`
INSERT INTO canvas_edges (id, work_id, source_node_id, target_node_id, kind, created_at)
VALUES (?, ?, ?, ?, 'generated_from', ?)`, edge.id, workID, edge.sourceNodeID, edge.targetNodeID, now); err != nil {
			t.Fatalf("insert canvas edge: %v", err)
		}
	}
	repository := NewAgentRepository(providerRepository)
	contextNodeIDs, err := repository.GetChapterSectionContext(workID, sectionID)
	if err != nil {
		t.Fatalf("resolve chapter section context: %v", err)
	}
	wantContextNodeIDs := []string{sectionID, chapterID, worldID}
	if len(contextNodeIDs) != len(wantContextNodeIDs) {
		t.Fatalf("chapter section context = %v, want %v", contextNodeIDs, wantContextNodeIDs)
	}
	for index, nodeID := range wantContextNodeIDs {
		if contextNodeIDs[index] != nodeID {
			t.Fatalf("chapter section context = %v, want %v", contextNodeIDs, wantContextNodeIDs)
		}
	}
	encodedContextNodeIDs, err := json.Marshal(contextNodeIDs)
	if err != nil {
		t.Fatalf("encode context node ids: %v", err)
	}
	if _, err := providerRepository.database.Exec(`
INSERT INTO agent_runs (
    id, work_id, status, prompt, target, target_node_id, provider_id, model_id,
    context_node_ids_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, workID, agent.RunStatusRunning, "撰写正文", agent.TargetChapterSection,
		sectionID, "provider-1", "model-1", string(encodedContextNodeIDs), now, now); err != nil {
		t.Fatalf("insert agent run: %v", err)
	}
	content, err := json.Marshal(map[string]string{"title": "宴会失控", "content": "完整正文"})
	if err != nil {
		t.Fatalf("encode chapter section: %v", err)
	}
	if err := repository.CompleteDerivation(context.Background(), agent.Run{
		ID: runID, WorkID: workID, Target: agent.TargetChapterSection, ContextNodeIDs: contextNodeIDs,
	}, sectionID, agent.RunResult{Content: string(content), ExpectedRevision: 1}); err != nil {
		t.Fatalf("complete chapter section: %v", err)
	}

	assertTableCount(t, providerRepository, `SELECT COUNT(*) FROM canvas_nodes WHERE work_id = ? AND kind = ?`, 1, workID, canvas.NodeKindChapterSection)
	assertTableCount(t, providerRepository, `
SELECT COUNT(*) FROM canvas_edges e
JOIN canvas_nodes target ON target.work_id = e.work_id AND target.id = e.target_node_id
WHERE e.work_id = ? AND target.kind = ?`, 3, workID, canvas.NodeKindChapterSection)
	for _, sourceNodeID := range contextNodeIDs {
		assertTableCount(t, providerRepository, `
SELECT COUNT(*) FROM canvas_edges e
JOIN canvas_nodes target ON target.work_id = e.work_id AND target.id = e.target_node_id
WHERE e.work_id = ? AND e.source_node_id = ? AND target.kind = ?`,
			1, workID, sourceNodeID, canvas.NodeKindChapterSection)
	}
}

func testSectionOutline(ordinal int) canvas.SectionOutlineData {
	return canvas.SectionOutlineData{
		Ordinal: ordinal, Purpose: "推进章节", Viewpoint: "主角", TargetLength: 1200,
		OpeningState: "局势尚未明朗", Beats: []string{"采取行动", "获得线索"},
		Conflict: "行动受到阻碍", TurningPoint: "线索改变判断", EndingState: "局势发生变化",
		Hook: "新的问题出现",
	}
}

func assertTableCount(t *testing.T, repository *ProviderRepository, query string, want int, arguments ...any) {
	t.Helper()
	var got int
	if err := repository.database.QueryRow(query, arguments...).Scan(&got); err != nil {
		t.Fatalf("query table count: %v", err)
	}
	if got != want {
		t.Fatalf("table count = %d, want %d", got, want)
	}
}

func TestCompleteNodeUpdateRecordsUndoableCanvasHistory(t *testing.T) {
	t.Parallel()

	providerRepository, err := NewProviderRepository(t.TempDir())
	if err != nil {
		t.Fatalf("create provider repository: %v", err)
	}
	t.Cleanup(func() {
		if err := providerRepository.Close(); err != nil {
			t.Errorf("close provider repository: %v", err)
		}
	})

	const (
		workID          = "work-1"
		runID           = "run-1"
		nodeID          = "character-c"
		originalTitle   = "角色 C"
		originalContent = "角色 C 的原有完整设定"
	)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	contextNodeIDs, err := json.Marshal([]string{nodeID})
	if err != nil {
		t.Fatalf("encode context node ids: %v", err)
	}
	if _, err := providerRepository.database.Exec(`
INSERT INTO canvas_nodes (id, work_id, revision, kind, title, content, x, y, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nodeID, workID, 3, "character", originalTitle, originalContent, 0, 0, now, now); err != nil {
		t.Fatalf("insert canvas node: %v", err)
	}
	if _, err := providerRepository.database.Exec(`
INSERT INTO agent_runs (
    id, work_id, status, prompt, target, provider_id, model_id, context_node_ids_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, workID, agent.RunStatusRunning, "补充秘密", agent.NodeUpdateTarget("character"),
		"provider-1", "model-1", string(contextNodeIDs), now, now); err != nil {
		t.Fatalf("insert agent run: %v", err)
	}

	repository := NewAgentRepository(providerRepository)
	err = repository.CompleteNodeUpdate(context.Background(), agent.Run{ID: runID, WorkID: workID}, nodeID, agent.RunResult{
		Title: originalTitle, Content: originalContent + "\n新增秘密", ExpectedRevision: 3,
	})
	if err != nil {
		t.Fatalf("complete node update: %v", err)
	}

	var actionType, label, payloadJSON string
	if err := providerRepository.database.QueryRow(`
SELECT action_type, label, payload_json FROM canvas_actions WHERE work_id = ?`, workID).
		Scan(&actionType, &label, &payloadJSON); err != nil {
		t.Fatalf("read canvas action: %v", err)
	}
	if actionType != actionUpdateNode || label != "Agent 更新节点" {
		t.Fatalf("unexpected canvas action: type=%q label=%q", actionType, label)
	}
	var payload updateNodeActionPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode canvas action: %v", err)
	}
	if payload.Before.Content != originalContent || payload.After.Content != originalContent+"\n新增秘密" {
		t.Fatalf("unexpected canvas action payload: %+v", payload)
	}

	canvasRepository := NewCanvasRepository(providerRepository)
	if _, err := canvasRepository.Undo(context.Background(), workID); err != nil {
		t.Fatalf("undo agent update: %v", err)
	}
	var title, content string
	if err := providerRepository.database.QueryRow(`
SELECT title, content FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, nodeID).
		Scan(&title, &content); err != nil {
		t.Fatalf("read restored canvas node: %v", err)
	}
	if title != originalTitle || content != originalContent {
		t.Fatalf("restored node = (%q, %q)", title, content)
	}
}

func TestAgentRunWaitsForResponseAndResumes(t *testing.T) {
	t.Parallel()

	providerRepository, err := NewProviderRepository(t.TempDir())
	if err != nil {
		t.Fatalf("create provider repository: %v", err)
	}
	t.Cleanup(func() {
		if err := providerRepository.Close(); err != nil {
			t.Errorf("close provider repository: %v", err)
		}
	})

	repository := NewAgentRepository(providerRepository)
	run, err := repository.CreateRun(agent.RunInput{
		RunID: "run-waiting", WorkID: "work-1", Prompt: "创建新角色", Target: agent.NodeUpdateTarget("character"),
		TargetNodeID: "character-1", ProviderID: "provider-1", ModelID: "model-1", ContextNodeIDs: []string{"character-1"},
	})
	if err != nil {
		t.Fatalf("create agent run: %v", err)
	}
	if err := repository.MarkStarted(run.ID); err != nil {
		t.Fatalf("start agent run: %v", err)
	}
	approval, err := repository.AppendEvent(run.ID, agent.EventApprovalRequired, map[string]any{
		"question": "新角色与男主是什么关系？", "options": []string{"同伴", "对手"},
	})
	if err != nil {
		t.Fatalf("append approval event: %v", err)
	}
	waitingRun, err := repository.GetRun(run.ID)
	if err != nil {
		t.Fatalf("read waiting agent run: %v", err)
	}
	if waitingRun.Status != agent.RunStatusWaitingInput || waitingRun.TargetNodeID != "character-1" {
		t.Fatalf("unexpected waiting run: %+v", waitingRun)
	}
	queuedResponse, err := repository.QueueResponse(run.ID, approval.ID, "亦敌亦友")
	if err != nil {
		t.Fatalf("queue agent response: %v", err)
	}
	if queuedResponse.Question != "新角色与男主是什么关系？" || queuedResponse.Answer != "亦敌亦友" {
		t.Fatalf("unexpected queued response: %+v", queuedResponse)
	}
	responses, err := repository.ListUserResponses(run.ID)
	if err != nil {
		t.Fatalf("list agent responses: %v", err)
	}
	if len(responses) != 1 || responses[0].Question != "新角色与男主是什么关系？" || responses[0].Answer != "亦敌亦友" {
		t.Fatalf("unexpected agent responses: %+v", responses)
	}
	if err := repository.MarkResumed(run.ID); err != nil {
		t.Fatalf("resume agent run: %v", err)
	}
	resumedRun, err := repository.GetRun(run.ID)
	if err != nil {
		t.Fatalf("read resumed agent run: %v", err)
	}
	if resumedRun.Status != agent.RunStatusRunning {
		t.Fatalf("resumed status = %q", resumedRun.Status)
	}
}

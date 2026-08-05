package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"warmnote/core/internal/agent"
)

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
	if err := repository.QueueResponse(run.ID, approval.ID, "亦敌亦友"); err != nil {
		t.Fatalf("queue agent response: %v", err)
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

package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	agent "warmnote/core/internal/agent/writing"
	"warmnote/core/internal/domain/canvas"
	"warmnote/core/internal/storage"
)

func TestPublicAgentErrorForInvalidDecision(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("run invocation: %w", agent.ErrInvalidDecision)
	message := publicAgentError(err)
	if message != "模型未返回有效的 Agent 决策，请重试或切换模型" {
		t.Fatalf("publicAgentError() = %q", message)
	}
}

func TestHasOnlyAttachmentPriorityNodes(t *testing.T) {
	t.Parallel()

	attachments := []agent.NodeReference{
		{ID: "world-1", Type: "world"},
		{ID: "character-2", Type: "character"},
	}
	if !hasOnlyAttachmentPriorityNodes([]string{"character-1", "world-1"}, "character-1", attachments) {
		t.Fatal("expected target and direct attachment to be accepted")
	}
	if hasOnlyAttachmentPriorityNodes([]string{"character-1", "location-1"}, "character-1", attachments) {
		t.Fatal("expected non-attachment priority node to be rejected")
	}
}

func TestAttachmentPriorityContextNodeIDsDropsRemovedAttachments(t *testing.T) {
	t.Parallel()

	got := attachmentPriorityContextNodeIDs(
		[]string{"character-1", "world-1", "removed-1", "world-1"},
		"character-1",
		[]agent.NodeReference{{ID: "world-1", Type: "world"}},
	)
	want := []string{"character-1", "world-1"}
	if len(got) != len(want) {
		t.Fatalf("priority context = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("priority context = %v, want %v", got, want)
		}
	}
}

func TestRespondToRunDoesNotQueueWhenContextPreparationFails(t *testing.T) {
	t.Parallel()

	providerRepository, err := storage.NewProviderRepository(t.TempDir())
	if err != nil {
		t.Fatalf("create provider repository: %v", err)
	}
	t.Cleanup(func() {
		if err := providerRepository.Close(); err != nil {
			t.Errorf("close provider repository: %v", err)
		}
	})

	agentRepository := storage.NewAgentRepository(providerRepository)
	run, err := agentRepository.CreateRun(agent.RunInput{
		RunID: "waiting-run", WorkID: "work-1", Prompt: "补充设定",
		Target: agent.NodeUpdateTarget("character"), TargetNodeID: "missing-node",
		ProviderID: "provider-1", ModelID: "model-1", ContextNodeIDs: []string{"missing-node"},
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := agentRepository.MarkStarted(run.ID); err != nil {
		t.Fatalf("start run: %v", err)
	}
	approval, err := agentRepository.AppendEvent(run.ID, agent.EventApprovalRequired, map[string]any{
		"question": "补充什么信息？",
	})
	if err != nil {
		t.Fatalf("append approval: %v", err)
	}

	service := NewAgentService(context.Background(), agentRepository, nil, slog.Default())
	_, err = service.RespondToRun(run.ID, approval.ID, "补充答案")
	if !errors.Is(err, canvas.ErrNodeNotFound) {
		t.Fatalf("respond error = %v, want node not found", err)
	}
	persistedRun, err := agentRepository.GetRun(run.ID)
	if err != nil {
		t.Fatalf("read run: %v", err)
	}
	if persistedRun.Status != agent.RunStatusWaitingInput {
		t.Fatalf("run status = %q, want %q", persistedRun.Status, agent.RunStatusWaitingInput)
	}
	responses, err := agentRepository.ListUserResponses(run.ID)
	if err != nil {
		t.Fatalf("list responses: %v", err)
	}
	if len(responses) != 0 {
		t.Fatalf("responses = %+v, want none", responses)
	}
}

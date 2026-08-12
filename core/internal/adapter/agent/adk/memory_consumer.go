package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appharness "warmmo/core/internal/application/harness"
)

const memoryArtifactSummaryBytes = 3 * 1024

func memoryRecallQuery(request LLMTurnRequest) string {
	query := request.Prompt
	if request.Resume == nil {
		return query
	}
	if answer, ok := request.Resume.Response["answer"].(string); ok && strings.TrimSpace(answer) != "" {
		query += "\n" + answer
	}
	return query
}

func (e *LLMTurnExecutor) consumeMemoryBestEffort(
	ctx context.Context,
	request LLMTurnRequest,
	outcome LLMTurnOutcome,
	emit LLMTurnEmitter,
) {
	if !request.PublishConversation || !request.Memory.Remember || outcome.Status != appharness.TurnCompleted || outcome.Artifact == nil || e.memories == nil {
		return
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := e.rememberArtifactEpisode(persistCtx, request, *outcome.Artifact); err != nil && emit != nil {
		_ = emit(LLMTurnEvent{
			Type: LLMEventMemoryFailed, AgentName: request.AgentName,
			Summary: truncateUTF8(err.Error(), 2048),
		})
	}
}

func (e *LLMTurnExecutor) rememberArtifactEpisode(
	ctx context.Context,
	request LLMTurnRequest,
	ref appharness.ArtifactRef,
) error {
	workID := strings.TrimSpace(request.ToolInvocation.WorkID)
	if workID == "" {
		return nil
	}
	artifact, err := e.artifacts.GetArtifact(ctx, ref.ID)
	if err != nil {
		return fmt.Errorf("load artifact for memory: %w", err)
	}
	summary := summarizeArtifactForMemory(artifact.Payload)
	content := fmt.Sprintf(
		"Non-authoritative agent episode. Verify against current Canvas before use. Agent: %s. Artifact: %s (%s). Summary: %s",
		request.AgentID, artifact.Ref.ID, artifact.Ref.Kind, summary,
	)
	_, err = e.memories.Remember(ctx, appharness.MemoryRecord{
		WorkID: workID, Kind: "artifact_episode_v1", Content: content,
		SourceRunID: request.RunID, SourceArtifactID: artifact.Ref.ID,
	})
	if err != nil {
		return fmt.Errorf("remember artifact episode: %w", err)
	}
	return nil
}

func summarizeArtifactForMemory(payload json.RawMessage) string {
	var text string
	if err := json.Unmarshal(payload, &text); err == nil {
		return truncateUTF8(strings.TrimSpace(text), memoryArtifactSummaryBytes)
	}
	var compact any
	if err := json.Unmarshal(payload, &compact); err == nil {
		if encoded, encodeErr := json.Marshal(compact); encodeErr == nil {
			return truncateUTF8(string(encoded), memoryArtifactSummaryBytes)
		}
	}
	return "Artifact payload could not be summarized; load it by reference."
}

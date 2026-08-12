package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	appharness "warmmo/core/internal/application/harness"
)

func (e *LLMTurnExecutor) consumeConversationBestEffort(
	ctx context.Context,
	request LLMTurnRequest,
	outcome LLMTurnOutcome,
	emit LLMTurnEmitter,
) {
	if e == nil || e.conversation == nil || !request.PublishConversation || outcome.Status != appharness.TurnCompleted {
		return
	}
	workID := strings.TrimSpace(request.ToolInvocation.WorkID)
	if workID == "" {
		return
	}
	conversationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	assistant := ""
	if outcome.Final != nil {
		assistant = strings.TrimSpace(outcome.Final.Content)
	}
	if assistant == "" && outcome.Artifact != nil {
		artifact, err := e.artifacts.GetArtifact(conversationCtx, outcome.Artifact.ID)
		if err == nil {
			assistant = conversationArtifactContent(artifact.Payload)
		} else {
			assistant = fmt.Sprintf("Submitted artifact %s (%s). Read the current Canvas for authoritative content.", outcome.Artifact.ID, outcome.Artifact.Kind)
		}
	}
	if assistant == "" {
		return
	}
	userContent := strings.TrimSpace(request.ConversationUserContent)
	if userContent == "" {
		userContent = strings.TrimSpace(request.Prompt)
	}
	if err := e.conversation.AppendTurn(conversationCtx, appharness.ConversationTurn{
		ID: request.TurnID, WorkID: workID, SessionID: strings.TrimSpace(request.ConversationSessionID), RunID: request.RunID,
		AgentID: request.AgentID, AgentName: request.AgentName,
		ProviderID: request.ProviderID, ModelID: request.ModelID,
		UserContent: userContent, AssistantContent: assistant,
		Status: outcome.Status, Usage: outcome.Usage,
	}); err != nil && emit != nil {
		_ = emit(LLMTurnEvent{Type: LLMEventConversationFailed, AgentName: request.AgentName, Summary: "conversation projection failed: " + err.Error()})
	}
}

func conversationArtifactContent(payload json.RawMessage) string {
	var text string
	if err := json.Unmarshal(payload, &text); err == nil {
		return truncateUTF8(strings.TrimSpace(text), 32*1024)
	}
	var compact any
	if err := json.Unmarshal(payload, &compact); err == nil {
		if encoded, encodeErr := json.Marshal(compact); encodeErr == nil {
			return truncateUTF8(string(encoded), 32*1024)
		}
	}
	return "Agent 已完成创作；请以当前画布内容为准。"
}

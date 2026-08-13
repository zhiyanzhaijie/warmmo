package projection

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	appharness "warmmo/core/internal/application/harness"
)

const memorySummaryBytes = 3 * 1024

type OutcomeConsumer struct {
	conversation appharness.ConversationStore
	memory       appharness.MemoryStore
}

func NewOutcomeConsumer(conversation appharness.ConversationStore, memory appharness.MemoryStore) *OutcomeConsumer {
	return &OutcomeConsumer{conversation: conversation, memory: memory}
}

func (c *OutcomeConsumer) Consume(ctx context.Context, request appharness.RuntimeRequest, outcome appharness.TurnOutcome, emit appharness.RuntimeEmitter) {
	if c == nil || !request.PublishConversation || outcome.Status != appharness.TurnCompleted {
		return
	}
	workID := strings.TrimSpace(request.ToolInvocation.WorkID)
	if workID == "" {
		return
	}
	content := outcomeContent(outcome)
	if content == "" {
		return
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if c.conversation != nil {
		userContent := strings.TrimSpace(request.ConversationUserContent)
		if userContent == "" {
			userContent = strings.TrimSpace(request.Prompt)
		}
		if err := c.conversation.AppendTurn(persistCtx, appharness.ConversationTurn{
			ID: request.TurnID, WorkID: workID, SessionID: strings.TrimSpace(request.ConversationSessionID), RunID: request.RunID,
			AgentID: request.AgentID, AgentName: request.AgentName, ProviderID: request.ProviderID, ModelID: request.ModelID,
			UserContent: userContent, AssistantContent: content, Status: outcome.Status, Usage: outcome.Usage,
		}); err != nil && emit != nil {
			_ = emit(appharness.RuntimeEvent{Type: appharness.RuntimeEventConversationFailed, AgentName: request.AgentName, Summary: "conversation projection failed: " + err.Error()})
		}
	}
	if request.Memory.Remember && c.memory != nil {
		if _, err := c.memory.Remember(persistCtx, appharness.MemoryRecord{
			WorkID: workID, Kind: "agent_output_v1", Content: truncate(content, memorySummaryBytes), SourceRunID: request.RunID,
		}); err != nil && emit != nil {
			_ = emit(appharness.RuntimeEvent{Type: appharness.RuntimeEventMemoryFailed, AgentName: request.AgentName, Summary: truncate(err.Error(), 2048)})
		}
	}
}

func outcomeContent(outcome appharness.TurnOutcome) string {
	if len(outcome.Output) > 0 {
		var text string
		if err := json.Unmarshal(outcome.Output, &text); err == nil {
			return strings.TrimSpace(text)
		}
		var compact any
		if err := json.Unmarshal(outcome.Output, &compact); err == nil {
			if encoded, encodeErr := json.Marshal(compact); encodeErr == nil {
				return string(encoded)
			}
		}
	}
	if outcome.Final != nil {
		return strings.TrimSpace(outcome.Final.Content)
	}
	return ""
}

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return string([]rune(value)[:min(len([]rune(value)), limit)]) + "..."
}

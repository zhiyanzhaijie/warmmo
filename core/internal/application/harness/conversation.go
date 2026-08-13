package harness

import (
	"context"
	"time"
)

// ConversationTurn is the durable, work-scoped projection of one agent
// turn. It intentionally stores summaries rather than the full ADK event log;
// the latter remains owned by the turn session and checkpoint stores.
type ConversationTurn struct {
	ID               string     `json:"id"`
	WorkID           string     `json:"workId"`
	SessionID        string     `json:"sessionId"`
	RunID            string     `json:"runId"`
	AgentID          string     `json:"agentId"`
	AgentName        string     `json:"agentName"`
	ProviderID       string     `json:"providerId"`
	ModelID          string     `json:"modelId"`
	UserContent      string     `json:"userContent"`
	AssistantContent string     `json:"assistantContent"`
	Status           TurnStatus `json:"status"`
	Usage            Usage      `json:"usage"`
	CreatedAt        time.Time  `json:"createdAt"`
}

type ConversationSession struct {
	ID                  string             `json:"id"`
	WorkID              string             `json:"workId"`
	Title               string             `json:"title"`
	ProviderID          string             `json:"providerId"`
	ModelID             string             `json:"modelId"`
	ContextWindowTokens *int               `json:"contextWindowTokens"`
	LatestUsage         Usage              `json:"latestUsage"`
	TurnCount           int                `json:"turnCount"`
	CreatedAt           time.Time          `json:"createdAt"`
	UpdatedAt           time.Time          `json:"updatedAt"`
	Turns               []ConversationTurn `json:"turns"`
}

type ConversationSnapshot struct {
	WorkID   string                `json:"workId"`
	Sessions []ConversationSession `json:"sessions"`
}

// ConversationStore provides bounded conversation context to the
// model adapter and accepts idempotent completed-turn projections.
type ConversationStore interface {
	BuildContext(context.Context, string, int) (string, error)
	AppendTurn(context.Context, ConversationTurn) error
	ListSessions(context.Context, string, int) (ConversationSnapshot, error)
}

type SessionConversationStore interface {
	BuildSessionContext(context.Context, string, string, int) (string, error)
}

// ContextProvider supplies compact, authoritative context for the current work.
type ContextProvider interface {
	BuildContext(context.Context, string, int) (string, error)
}

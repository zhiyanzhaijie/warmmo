package harness

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrSessionNotFound      = errors.New("agent session not found")
	ErrCheckpointNotFound   = errors.New("agent turn checkpoint not found")
	ErrCheckpointConflict   = errors.New("agent turn checkpoint version conflict")
	ErrProjectionFailed     = errors.New("agent product event projection failed")
	ErrBudgetExceeded       = errors.New("agent turn budget exceeded")
	ErrTurnRecoveryRequired = errors.New("agent turn requires checkpoint recovery")
)

type TurnStatus string

const (
	TurnRunning           TurnStatus = "running"
	TurnCompleted         TurnStatus = "completed"
	TurnAwaitingUser      TurnStatus = "awaiting_user"
	TurnAwaitingChild     TurnStatus = "awaiting_child"
	TurnStoppedBudget     TurnStatus = "stopped_budget"
	TurnStoppedNoProgress TurnStatus = "stopped_no_progress"
	TurnCancelled         TurnStatus = "cancelled"
	TurnFailed            TurnStatus = "failed"
)

type StopReason string

const (
	StopFinalResponse     StopReason = "final_response"
	StopArtifactSubmitted StopReason = "artifact_submitted"
	StopUserInputRequired StopReason = "user_input_required"
	StopChildPending      StopReason = "child_pending"
	StopBudgetExceeded    StopReason = "budget_exceeded"
	StopNoProgress        StopReason = "no_progress"
	StopContextCancelled  StopReason = "context_cancelled"
	StopExecutionFailed   StopReason = "execution_failed"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Usage struct {
	InputTokens       int64 `json:"inputTokens"`
	CachedInputTokens int64 `json:"cachedInputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
}

type BudgetPolicy struct {
	MaxModelCalls      int
	MaxToolCalls       int
	MaxSideEffectCalls int
	MaxDuration        time.Duration
	MaxToolResultBytes int
}

func DefaultBudgetPolicy() BudgetPolicy {
	return BudgetPolicy{
		MaxModelCalls: 16, MaxToolCalls: 16, MaxSideEffectCalls: 4,
		MaxDuration: 3 * time.Minute, MaxToolResultBytes: 64 * 1024,
	}
}

type BudgetUsage struct {
	ModelCalls      int `json:"modelCalls"`
	ToolCalls       int `json:"toolCalls"`
	SideEffectCalls int `json:"sideEffectCalls"`
}

type PendingAction struct {
	Kind       string          `json:"kind"`
	ToolName   string          `json:"toolName,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type TurnCommand struct {
	RunID     string
	TurnID    string
	SessionID string
	AgentID   string
}

type TurnSnapshot struct {
	AgentName               string          `json:"agentName"`
	Description             string          `json:"description"`
	Instruction             string          `json:"instruction"`
	ProviderID              string          `json:"providerId"`
	ModelID                 string          `json:"modelId"`
	ConversationSessionID   string          `json:"conversationSessionId,omitempty"`
	UserID                  string          `json:"userId"`
	Prompt                  string          `json:"prompt"`
	ConversationUserContent string          `json:"conversationUserContent,omitempty"`
	PublishConversation     bool            `json:"publishConversation,omitempty"`
	AllowedTools            []string        `json:"allowedTools"`
	ControlTools            []string        `json:"controlTools,omitempty"`
	AllowedChildren         []ChildContract `json:"allowedChildren,omitempty"`
	WorkID                  string          `json:"workId"`
	SkillID                 string          `json:"skillId,omitempty"`
	SkillVersion            string          `json:"skillVersion,omitempty"`
	Budget                  BudgetPolicy    `json:"budget"`
	Context                 ContextPolicy   `json:"context"`
	Memory                  MemoryPolicy    `json:"memory"`
	Output                  OutputContract  `json:"output"`
}

type ResumeInput struct {
	ToolCallID string         `json:"toolCallId"`
	ToolName   string         `json:"toolName"`
	Response   map[string]any `json:"response"`
}

type TurnOutcome struct {
	Status     TurnStatus     `json:"status"`
	StopReason StopReason     `json:"stopReason"`
	SessionID  string         `json:"sessionId"`
	Final      *Message       `json:"final,omitempty"`
	Pending    *PendingAction `json:"pending,omitempty"`
	Artifact   *ArtifactRef   `json:"artifact,omitempty"`
	Usage      Usage          `json:"usage"`
	Budget     BudgetUsage    `json:"budget"`
}

type Checkpoint struct {
	RunID                string
	TurnID               string
	SessionID            string
	AgentID              string
	DefinitionVersion    string
	DefinitionHash       string
	PromptHash           string
	ToolsetHash          string
	Status               TurnStatus
	StopReason           StopReason
	Final                *Message
	Pending              *PendingAction
	Artifact             *ArtifactRef
	Usage                Usage
	Budget               BudgetUsage
	ChildRunIDs          []string
	CompactionManifest   json.RawMessage
	LastCanonicalEventID string
	Snapshot             *TurnSnapshot
	Version              int64
	UpdatedAt            time.Time
}

type CheckpointStore interface {
	GetCheckpoint(context.Context, string) (Checkpoint, error)
	FindLatestCheckpoint(context.Context, string, string) (Checkpoint, error)
	FindPendingCheckpoint(context.Context, string) (Checkpoint, error)
	AttachChildRun(context.Context, string, string) (Checkpoint, error)
	SaveCheckpoint(context.Context, Checkpoint) (Checkpoint, error)
}

type Harness interface {
	RunTurn(context.Context, TurnCommand) (TurnOutcome, error)
}

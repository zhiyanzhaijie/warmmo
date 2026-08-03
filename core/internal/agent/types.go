package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type RunStatus string

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

type EventType string

const (
	EventRunQueued           EventType = "run.queued"
	EventRunStarted          EventType = "run.started"
	EventContextPreparing    EventType = "context.preparing"
	EventContextReady        EventType = "context.ready"
	EventBrainstormStarted   EventType = "brainstorm.started"
	EventBrainstormCompleted EventType = "brainstorm.completed"
	EventPlanStarted         EventType = "plan.started"
	EventPlanCompleted       EventType = "plan.completed"
	EventSkillSearching      EventType = "skill.searching"
	EventSkillMatched        EventType = "skill.matched"
	EventSkillLoaded         EventType = "skill.loaded"
	EventSkillCompleted      EventType = "skill.completed"
	EventToolRequested       EventType = "tool.requested"
	EventToolStarted         EventType = "tool.started"
	EventToolCompleted       EventType = "tool.completed"
	EventToolFailed          EventType = "tool.failed"
	EventApprovalRequired    EventType = "approval.required"
	EventMessageDelta        EventType = "message.delta"
	EventValidationCompleted EventType = "validation.completed"
	EventCandidateCreated    EventType = "candidate.created"
	EventRunCompleted        EventType = "run.completed"
	EventRunFailed           EventType = "run.failed"
	EventRunCancelled        EventType = "run.cancelled"
)

var (
	ErrRunNotFound       = errors.New("agent run not found")
	ErrRunNotCancellable = errors.New("agent run cannot be cancelled")
	ErrCanvasUnavailable = errors.New("canvas context is not available")
)

type Run struct {
	ID             string    `json:"id"`
	WorkID         string    `json:"workId"`
	Status         RunStatus `json:"status"`
	Prompt         string    `json:"prompt"`
	Target         string    `json:"target"`
	ProviderID     string    `json:"providerId"`
	ModelID        string    `json:"modelId"`
	ContextNodeIDs []string  `json:"contextNodeIds"`
	ErrorMessage   string    `json:"errorMessage,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Event struct {
	ID        string          `json:"id"`
	RunID     string          `json:"runId"`
	Sequence  int64           `json:"sequence"`
	Type      EventType       `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type Candidate struct {
	ID           string    `json:"id"`
	RunID        string    `json:"runId"`
	WorkID       string    `json:"workId"`
	SkillID      string    `json:"skillId"`
	SkillVersion string    `json:"skillVersion"`
	Content      string    `json:"content"`
	CreatedAt    time.Time `json:"createdAt"`
}

type RunInput struct {
	RunID          string
	WorkID         string
	Prompt         string
	Target         string
	ProviderID     string
	ModelID        string
	ContextNodeIDs []string
}

type RunResult struct {
	Content      string
	SkillID      string
	SkillVersion string
	CandidateID  string
}

type NodeSnapshot struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Content  string `json:"content"`
}

type ContextSnapshot struct {
	ID     string         `json:"id"`
	WorkID string         `json:"workId"`
	Nodes  []NodeSnapshot `json:"nodes"`
}

type ModelRequest struct {
	ModelID string
	System  string
	Prompt  string
}

type ModelUsage struct {
	InputTokens  int64
	OutputTokens int64
}

type TextModel interface {
	Complete(context.Context, ModelRequest) (string, ModelUsage, error)
	Stream(context.Context, ModelRequest, func(string) error) (ModelUsage, error)
}

type ContextReader interface {
	BuildSnapshot(context.Context, string, []string) (ContextSnapshot, error)
}

type Emitter func(EventType, any) error

type Engine interface {
	Run(context.Context, RunInput, Emitter) (RunResult, error)
}

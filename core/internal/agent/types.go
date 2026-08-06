package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func NodeUpdateTarget(nodeKind string) string {
	return TargetNodeUpdate + ":" + strings.TrimSpace(nodeKind)
}

func IsNodeUpdateTarget(target string) bool {
	return strings.HasPrefix(strings.TrimSpace(target), TargetNodeUpdate+":")
}

type RunStatus string

type CandidateStatus string

const (
	TargetNodeUpdate          = "node-update"
	TargetSectionOutlineBatch = "section-outline-batch"
	TargetChapterSection      = "chapter-section"
	TargetChapterArchive      = "chapter-archive"

	RunStatusQueued       RunStatus = "queued"
	RunStatusRunning      RunStatus = "running"
	RunStatusWaitingInput RunStatus = "waiting_input"
	RunStatusCompleted    RunStatus = "completed"
	RunStatusFailed       RunStatus = "failed"
	RunStatusCancelled    RunStatus = "cancelled"

	CandidateStatusPending  CandidateStatus = "pending"
	CandidateStatusAccepted CandidateStatus = "accepted"
	CandidateStatusRejected CandidateStatus = "rejected"
)

type EventType string

const (
	EventRunQueued            EventType = "run.queued"
	EventRunStarted           EventType = "run.started"
	EventContextPreparing     EventType = "context.preparing"
	EventContextReady         EventType = "context.ready"
	EventBrainstormStarted    EventType = "brainstorm.started"
	EventBrainstormCompleted  EventType = "brainstorm.completed"
	EventPlanStarted          EventType = "plan.started"
	EventPlanCompleted        EventType = "plan.completed"
	EventSkillSearching       EventType = "skill.searching"
	EventSkillMatched         EventType = "skill.matched"
	EventSkillLoaded          EventType = "skill.loaded"
	EventSkillCompleted       EventType = "skill.completed"
	EventDecisionInvalid      EventType = "decision.invalid"
	EventToolRequested        EventType = "tool.requested"
	EventToolStarted          EventType = "tool.started"
	EventToolCompleted        EventType = "tool.completed"
	EventToolFailed           EventType = "tool.failed"
	EventApprovalRequired     EventType = "approval.required"
	EventUserResponseReceived EventType = "user.response.received"
	EventRunResumed           EventType = "run.resumed"
	EventGenerationStarted    EventType = "generation.started"
	EventMessageDelta         EventType = "message.delta"
	EventValidationCompleted  EventType = "validation.completed"
	EventCandidateCreated     EventType = "candidate.created"
	EventNodeUpdated          EventType = "node.updated"
	EventNodesCreated         EventType = "nodes.created"
	EventRunCompleted         EventType = "run.completed"
	EventRunFailed            EventType = "run.failed"
	EventRunCancelled         EventType = "run.cancelled"
)

var (
	ErrRunNotFound         = errors.New("agent run not found")
	ErrRunNotCancellable   = errors.New("agent run cannot be cancelled")
	ErrRunNotWaitingInput  = errors.New("agent run is not waiting for input")
	ErrInvalidUserResponse = errors.New("invalid agent user response")
	ErrCanvasUnavailable   = errors.New("canvas context is not available")
)

type Run struct {
	ID             string    `json:"id"`
	WorkID         string    `json:"workId"`
	Status         RunStatus `json:"status"`
	Prompt         string    `json:"prompt"`
	Target         string    `json:"target"`
	TargetNodeID   string    `json:"targetNodeId,omitempty"`
	ProviderID     string    `json:"providerId"`
	ModelID        string    `json:"modelId"`
	ContextNodeIDs []string  `json:"contextNodeIds"`
	ErrorMessage   string    `json:"errorMessage,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type ModelResponseFormat string

const (
	ModelResponseFormatText       ModelResponseFormat = ""
	ModelResponseFormatJSONObject ModelResponseFormat = "json_object"
)

type Event struct {
	ID        string          `json:"id"`
	RunID     string          `json:"runId"`
	Sequence  int64           `json:"sequence"`
	Type      EventType       `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type Candidate struct {
	ID             string          `json:"id"`
	RunID          string          `json:"runId"`
	WorkID         string          `json:"workId"`
	SkillID        string          `json:"skillId"`
	SkillVersion   string          `json:"skillVersion"`
	Status         CandidateStatus `json:"status"`
	Kind           string          `json:"kind"`
	CandidateType  string          `json:"candidateType,omitempty"`
	NodeID         string          `json:"nodeId,omitempty"`
	BaseVersionID  string          `json:"baseVersionId,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	ChangeScore    float64         `json:"changeScore,omitempty"`
	Title          string          `json:"title"`
	Content        string          `json:"content"`
	X              float64         `json:"x"`
	Y              float64         `json:"y"`
	ContextNodeIDs []string        `json:"contextNodeIds"`
	AcceptedNodeID string          `json:"acceptedNodeId,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	DecidedAt      *time.Time      `json:"decidedAt,omitempty"`
}

type RunInput struct {
	RunID              string
	WorkID             string
	Prompt             string
	Target             string
	TargetNodeID       string
	TargetNodeType     string
	TargetNodeRevision int64
	ProviderID         string
	ModelID            string
	ContextNodeIDs     []string
	ContextNodes       []NodeReference
	UserResponses      []UserResponse
}

type UserResponse struct {
	ApprovalEventID string `json:"approvalEventId"`
	Question        string `json:"question"`
	Answer          string `json:"answer"`
}

type RunResult struct {
	Title            string
	Content          string
	SkillID          string
	SkillVersion     string
	CandidateID      string
	ExpectedRevision int64
}

// NodeReference is intentionally limited to routing metadata. Canvas content
// is read on demand through canvas.get_nodes.
type NodeReference struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type ModelRequest struct {
	ModelID        string
	System         string
	Prompt         string
	ResponseFormat ModelResponseFormat
}

type ModelUsage struct {
	InputTokens  int64
	OutputTokens int64
}

type TextModel interface {
	Complete(context.Context, ModelRequest) (string, ModelUsage, error)
	Stream(context.Context, ModelRequest, func(string) error) (ModelUsage, error)
}

type Emitter func(EventType, any) error

type Engine interface {
	Run(context.Context, RunInput, Emitter) (RunResult, error)
}

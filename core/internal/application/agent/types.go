package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"warmmo/core/internal/domain/canvas"
)

func NodeUpdateTarget(nodeKind string) string {
	return TargetNodeUpdate + ":" + strings.TrimSpace(nodeKind)
}

func IsNodeUpdateTarget(target string) bool {
	return strings.HasPrefix(strings.TrimSpace(target), TargetNodeUpdate+":")
}

func IsCollaborativeTarget(target string) bool {
	target = strings.TrimSpace(target)
	return target == TargetCollaborativeTargeted || target == TargetCollaborativeExplore
}

type RunStatus string
type CandidateStatus = canvas.CandidateStatus
type AgentRole string
type EventType string
type ModelResponseFormat string

const (
	TargetNodeUpdate            = "node-update"
	TargetSectionOutlineBatch   = "section-outline-batch"
	TargetChapterSection        = "chapter-section"
	TargetChapterArchive        = "chapter-archive"
	TargetCollaborativeTargeted = "collaborative-targeted"
	TargetCollaborativeExplore  = "collaborative-explore"
	TargetCollaborativePlanner  = "collaborative-planner"
	TargetWritingPolish         = "writing-polish"

	RunStatusQueued       RunStatus = "queued"
	RunStatusRunning      RunStatus = "running"
	RunStatusWaitingInput RunStatus = "waiting_input"
	RunStatusCompleted    RunStatus = "completed"
	RunStatusFailed       RunStatus = "failed"
	RunStatusCancelled    RunStatus = "cancelled"

	CandidateStatusPending  = canvas.CandidateStatusPending
	CandidateStatusAccepted = canvas.CandidateStatusAccepted
	CandidateStatusRejected = canvas.CandidateStatusRejected

	RolePlanner AgentRole = "planner"
	RoleCreator AgentRole = "creator"
	RoleWriter  AgentRole = "writer"

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
	EventCandidateDecision    EventType = "candidate.decision"
	EventNodeUpdated          EventType = "node.updated"
	EventNodesCreated         EventType = "nodes.created"
	EventRunCompleted         EventType = "run.completed"
	EventRunFailed            EventType = "run.failed"
	EventRunCancelled         EventType = "run.cancelled"
	EventRoleStarted          EventType = "role.started"
	EventRoleHandoff          EventType = "role.handoff"
	EventRoleCompleted        EventType = "role.completed"

	ModelResponseFormatText       ModelResponseFormat = ""
	ModelResponseFormatJSONObject ModelResponseFormat = "json_object"
)

var (
	ErrRunNotFound         = errors.New("agent run not found")
	ErrRunNotCancellable   = errors.New("agent run cannot be cancelled")
	ErrRunNotWaitingInput  = errors.New("agent run is not waiting for input")
	ErrInvalidUserResponse = errors.New("invalid agent user response")
	ErrCanvasUnavailable   = errors.New("canvas context is not available")
	ErrApprovalRequired    = errors.New("agent requires user input")
	ErrInvalidDecision     = errors.New("invalid agent decision")
)

type CollaborationPlan struct {
	Intent            string `json:"intent"`
	Brief             string `json:"brief"`
	ContextQuery      string `json:"contextQuery"`
	CreatorTarget     string `json:"creatorTarget"`
	CreatorSkillID    string `json:"creatorSkillId"`
	OutputKind        string `json:"outputKind"`
	WriterRequired    bool   `json:"writerRequired"`
	WriterInstruction string `json:"writerInstruction"`
}

type ProposalSet struct {
	BaseRevisions map[string]int64 `json:"baseRevisions"`
	Nodes         []ProposalNode   `json:"nodes"`
	Updates       []ProposalUpdate `json:"updates"`
	Edges         []ProposalEdge   `json:"edges"`
	Reasons       []string         `json:"reasons"`
	Questions     []string         `json:"questions"`
}

type ProposalNode struct {
	ClientID string `json:"clientId"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Content  string `json:"content"`
}
type ProposalUpdate struct {
	NodeID       string `json:"nodeId"`
	BaseRevision int64  `json:"baseRevision"`
	Title        string `json:"title"`
	Content      string `json:"content"`
}
type ProposalEdge struct {
	SourceID string `json:"sourceId"`
	TargetID string `json:"targetId"`
	Kind     string `json:"kind"`
}

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

type Event struct {
	ID        string          `json:"id"`
	RunID     string          `json:"runId"`
	Sequence  int64           `json:"sequence"`
	Type      EventType       `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}
type Candidate = canvas.Candidate

type RunInput struct {
	RunID                   string
	WorkID                  string
	Prompt                  string
	Target                  string
	TargetNodeID            string
	TargetNodeType          string
	TargetNodeRevision      int64
	ProviderID              string
	ModelID                 string
	ContextNodeIDs          []string
	ContextNodes            []NodeReference
	UserResponses           []UserResponse
	CollaborativeCandidates []CollaborativeCandidate
}

type CollaborativeCandidate struct {
	CandidateID    string          `json:"candidateId"`
	Status         CandidateStatus `json:"status"`
	Kind           string          `json:"kind"`
	Title          string          `json:"title"`
	AcceptedNodeID string          `json:"acceptedNodeId,omitempty"`
}
type UserResponse struct {
	ApprovalEventID string `json:"approvalEventId"`
	Question        string `json:"question"`
	Answer          string `json:"answer"`
}
type RunResult struct {
	Title            string
	Content          string
	Message          string
	Role             AgentRole
	SkillID          string
	SkillVersion     string
	CandidateID      string
	ExpectedRevision int64
}
type NodeReference struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type StorySpineSearchResult struct {
	ArchiveID            string `json:"archiveId"`
	ChapterOutlineNodeID string `json:"chapterOutlineNodeId"`
	Title                string `json:"title"`
	Snippet              string `json:"snippet"`
	Source               string `json:"source"`
	Path                 string `json:"path,omitempty"`
	ContextRole          string `json:"contextRole"`
	RecencyRank          int    `json:"recencyRank"`
}

type ContextSearchInput struct {
	Query           string   `json:"query"`
	Kinds           []string `json:"kinds"`
	Scope           string   `json:"scope"`
	IncludeSpine    bool     `json:"includeStorySpine"`
	SeedNodeIDs     []string `json:"seedNodeIds"`
	MaxRelationHops int      `json:"maxRelationHops"`
	Limit           int      `json:"limit"`
}

type ContextSearchResult struct {
	NodeID    string   `json:"nodeId,omitempty"`
	VersionID string   `json:"versionId,omitempty"`
	ArchiveID string   `json:"archiveId,omitempty"`
	Revision  int64    `json:"revision,omitempty"`
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	Score     float64  `json:"score"`
	Scope     string   `json:"scope"`
	Source    string   `json:"source"`
	Evidence  []string `json:"evidence,omitempty"`
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

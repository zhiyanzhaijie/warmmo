package writing

import appagent "warmmo/core/internal/application/agent"

var (
	ErrRunNotFound         = appagent.ErrRunNotFound
	ErrRunNotCancellable   = appagent.ErrRunNotCancellable
	ErrRunNotWaitingInput  = appagent.ErrRunNotWaitingInput
	ErrInvalidUserResponse = appagent.ErrInvalidUserResponse
	ErrCanvasUnavailable   = appagent.ErrCanvasUnavailable
	ErrApprovalRequired    = appagent.ErrApprovalRequired
	ErrInvalidDecision     = appagent.ErrInvalidDecision
)

const (
	TargetNodeUpdate            = appagent.TargetNodeUpdate
	TargetSectionOutlineBatch   = appagent.TargetSectionOutlineBatch
	TargetChapterSection        = appagent.TargetChapterSection
	TargetChapterArchive        = appagent.TargetChapterArchive
	TargetCollaborativeTargeted = appagent.TargetCollaborativeTargeted
	TargetCollaborativeExplore  = appagent.TargetCollaborativeExplore
	TargetCollaborativePlanner  = appagent.TargetCollaborativePlanner
	TargetWritingPolish         = appagent.TargetWritingPolish

	RunStatusQueued       = appagent.RunStatusQueued
	RunStatusRunning      = appagent.RunStatusRunning
	RunStatusWaitingInput = appagent.RunStatusWaitingInput
	RunStatusCompleted    = appagent.RunStatusCompleted
	RunStatusFailed       = appagent.RunStatusFailed
	RunStatusCancelled    = appagent.RunStatusCancelled

	CandidateStatusPending  = appagent.CandidateStatusPending
	CandidateStatusAccepted = appagent.CandidateStatusAccepted
	CandidateStatusRejected = appagent.CandidateStatusRejected

	RolePlanner = appagent.RolePlanner
	RoleCreator = appagent.RoleCreator
	RoleWriter  = appagent.RoleWriter

	EventRunQueued            = appagent.EventRunQueued
	EventRunStarted           = appagent.EventRunStarted
	EventContextPreparing     = appagent.EventContextPreparing
	EventContextReady         = appagent.EventContextReady
	EventBrainstormStarted    = appagent.EventBrainstormStarted
	EventBrainstormCompleted  = appagent.EventBrainstormCompleted
	EventPlanStarted          = appagent.EventPlanStarted
	EventPlanCompleted        = appagent.EventPlanCompleted
	EventSkillSearching       = appagent.EventSkillSearching
	EventSkillMatched         = appagent.EventSkillMatched
	EventSkillLoaded          = appagent.EventSkillLoaded
	EventSkillCompleted       = appagent.EventSkillCompleted
	EventDecisionInvalid      = appagent.EventDecisionInvalid
	EventToolRequested        = appagent.EventToolRequested
	EventToolStarted          = appagent.EventToolStarted
	EventToolCompleted        = appagent.EventToolCompleted
	EventToolFailed           = appagent.EventToolFailed
	EventApprovalRequired     = appagent.EventApprovalRequired
	EventUserResponseReceived = appagent.EventUserResponseReceived
	EventRunResumed           = appagent.EventRunResumed
	EventGenerationStarted    = appagent.EventGenerationStarted
	EventMessageDelta         = appagent.EventMessageDelta
	EventValidationCompleted  = appagent.EventValidationCompleted
	EventCandidateCreated     = appagent.EventCandidateCreated
	EventCandidateDecision    = appagent.EventCandidateDecision
	EventNodeUpdated          = appagent.EventNodeUpdated
	EventNodesCreated         = appagent.EventNodesCreated
	EventRunCompleted         = appagent.EventRunCompleted
	EventRunFailed            = appagent.EventRunFailed
	EventRunCancelled         = appagent.EventRunCancelled
	EventRoleStarted          = appagent.EventRoleStarted
	EventRoleHandoff          = appagent.EventRoleHandoff
	EventRoleCompleted        = appagent.EventRoleCompleted

	ModelResponseFormatText       = appagent.ModelResponseFormatText
	ModelResponseFormatJSONObject = appagent.ModelResponseFormatJSONObject
)

var (
	NodeUpdateTarget      = appagent.NodeUpdateTarget
	IsNodeUpdateTarget    = appagent.IsNodeUpdateTarget
	IsCollaborativeTarget = appagent.IsCollaborativeTarget
)

type RunStatus = appagent.RunStatus
type CandidateStatus = appagent.CandidateStatus
type AgentRole = appagent.AgentRole
type CollaborationPlan = appagent.CollaborationPlan
type ProposalSet = appagent.ProposalSet
type ProposalNode = appagent.ProposalNode
type ProposalUpdate = appagent.ProposalUpdate
type ProposalEdge = appagent.ProposalEdge
type EventType = appagent.EventType
type Run = appagent.Run
type Event = appagent.Event
type Candidate = appagent.Candidate
type RunInput = appagent.RunInput
type CollaborativeCandidate = appagent.CollaborativeCandidate
type UserResponse = appagent.UserResponse
type RunResult = appagent.RunResult
type NodeReference = appagent.NodeReference
type ModelResponseFormat = appagent.ModelResponseFormat
type ModelRequest = appagent.ModelRequest
type ModelUsage = appagent.ModelUsage
type TextModel = appagent.TextModel
type Emitter = appagent.Emitter
type Engine = appagent.Engine

package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	agent "warmmo/core/internal/application/agent"
	appharness "warmmo/core/internal/application/harness"
	"warmmo/core/internal/domain/canvas"
)

var ErrInvalidAgentRun = errors.New("invalid agent run")

type AgentStore interface {
	CreateRun(agent.RunInput) (agent.Run, error)
	GetRun(string) (agent.Run, error)
	ListInterruptedRuns() ([]agent.Run, error)
	ListEvents(string, int64) ([]agent.Event, error)
	AppendEvent(string, agent.EventType, any) (agent.Event, error)
	Cancel(string) error
	MarkStarted(string) error
	MarkResumed(string) error
	MarkRecovered(string) error
	Fail(string, string) error
	PrepareProductProjection(agent.Run, agent.RunResult) (agent.ProductProjection, error)
	RecordProductProjectionError(string, string, agent.ProductProjectionStatus, string) error
	RequeueProductProjection(string, string, int, time.Duration) error

	GetRunByCandidate(string, string) (agent.Run, agent.Candidate, error)
	RecordCandidateDecision(string, string, bool, string) error
	RequestCandidateDecisionReason(string, string, string) error
	ListCollaborativeCandidates(string) ([]agent.CollaborativeCandidate, error)
	ListUserResponses(string) ([]agent.UserResponse, error)
	QueueResponse(string, string, string) (agent.UserResponse, error)

	GetCanvasNodeMetadata(string, string) (canvas.NodeKind, int64, error)
	GetNodeAttachments(string, string) ([]agent.NodeReference, error)
	GetNodeReferences(string, []string) ([]agent.NodeReference, error)
	GetGlobalContextNodeReferences(string) ([]agent.NodeReference, error)
	GetChapterSectionContext(string, string) ([]string, error)
	GetChapterArchiveContext(string, string) ([]string, error)

	Complete(agent.Run, agent.RunResult) (agent.Candidate, error)
	CompleteReadOnly(agent.Run, agent.RunResult) error
	CompleteCollaborativeProposal(agent.Run, agent.RunResult) error
	CompleteNodeUpdate(context.Context, agent.Run, string, agent.RunResult) error
	CompleteDerivation(context.Context, agent.Run, string, agent.RunResult) error
	CompleteChapterArchive(context.Context, agent.Run, agent.RunResult) error
}

type AgentService struct {
	ctx          context.Context
	store        AgentStore
	engine       agent.Engine
	logger       *slog.Logger
	mu           sync.Mutex
	cancels      map[string]context.CancelFunc
	conversation appharness.ConversationStore
}

type executionMode uint8

const (
	executionFresh executionMode = iota
	executionResume
	executionRecover
)

func NewAgentService(ctx context.Context, store AgentStore, engine agent.Engine, logger *slog.Logger, conversationReaders ...appharness.ConversationStore) *AgentService {
	var conversation appharness.ConversationStore
	if len(conversationReaders) > 0 {
		conversation = conversationReaders[0]
	}
	return &AgentService{ctx: ctx, store: store, engine: engine, logger: logger, cancels: make(map[string]context.CancelFunc), conversation: conversation}
}

func (s *AgentService) ListConversation(ctx context.Context, workID string, limit int) (appharness.ConversationSnapshot, error) {
	if s.conversation == nil {
		return appharness.ConversationSnapshot{WorkID: strings.TrimSpace(workID), Sessions: []appharness.ConversationSession{}}, nil
	}
	return s.conversation.ListSessions(ctx, strings.TrimSpace(workID), limit)
}

func (s *AgentService) CreateRun(input agent.RunInput) (agent.Run, error) {
	input.WorkID = strings.TrimSpace(input.WorkID)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Target = strings.TrimSpace(input.Target)
	input.TargetNodeID = strings.TrimSpace(input.TargetNodeID)
	input.ProviderID = strings.TrimSpace(input.ProviderID)
	input.ModelID = strings.TrimSpace(input.ModelID)
	input.ConversationSessionID = strings.TrimSpace(input.ConversationSessionID)
	input.ContextNodeIDs = uniqueNodeIDs(input.ContextNodeIDs)
	if input.WorkID == "" || input.Prompt == "" || input.Target == "" || input.ProviderID == "" || input.ModelID == "" {
		return agent.Run{}, fmt.Errorf("%w: workId, prompt, target, providerId and modelId are required", ErrInvalidAgentRun)
	}
	if input.Target != agent.TargetNodeUpdate &&
		input.Target != agent.TargetSectionOutlineBatch &&
		input.Target != agent.TargetChapterSection &&
		input.Target != agent.TargetChapterArchive &&
		!agent.IsCollaborativeTarget(input.Target) {
		return agent.Run{}, fmt.Errorf("%w: target %q is not supported", ErrInvalidAgentRun, input.Target)
	}
	if input.TargetNodeID == "" && !agent.IsCollaborativeTarget(input.Target) {
		return agent.Run{}, fmt.Errorf("%w: targetNodeId is required", ErrInvalidAgentRun)
	}
	if agent.IsCollaborativeTarget(input.Target) {
		if input.ConversationSessionID == "" {
			input.ConversationSessionID = uuid.NewString()
		}
		globalContext, err := s.store.GetGlobalContextNodeReferences(input.WorkID)
		if err != nil {
			return agent.Run{}, err
		}
		input.ContextNodes = append(input.ContextNodes, globalContext...)
		input.ContextNodeIDs = append(input.ContextNodeIDs, nodeReferenceIDs(globalContext)...)
		input.ContextNodeIDs = uniqueNodeIDs(input.ContextNodeIDs)
		if len(input.ContextNodeIDs) > 0 {
			contextNodes, err := s.store.GetNodeReferences(input.WorkID, input.ContextNodeIDs)
			if err != nil {
				return agent.Run{}, err
			}
			input.ContextNodes = contextNodes
		}
		input.RunID = uuid.NewString()
		run, err := s.store.CreateRun(input)
		if err != nil {
			return agent.Run{}, err
		}
		go s.execute(run, input, executionFresh, "")
		return run, nil
	}
	if input.Target == agent.TargetNodeUpdate && !containsNodeID(input.ContextNodeIDs, input.TargetNodeID) {
		return agent.Run{}, fmt.Errorf("%w: targetNodeId must be included in contextNodeIds", ErrInvalidAgentRun)
	}
	nodeKind, targetNodeRevision, err := s.store.GetCanvasNodeMetadata(input.WorkID, input.TargetNodeID)
	if err != nil {
		return agent.Run{}, err
	}
	input.TargetNodeType = string(nodeKind)
	input.TargetNodeRevision = targetNodeRevision
	if input.Target == agent.TargetNodeUpdate {
		if !canvas.IsValidNodeKind(nodeKind) {
			return agent.Run{}, fmt.Errorf("%w: target node kind %q is not supported", ErrInvalidAgentRun, nodeKind)
		}
		attachmentNodes, err := s.store.GetNodeAttachments(input.WorkID, input.TargetNodeID)
		if err != nil {
			return agent.Run{}, err
		}
		if !hasOnlyAttachmentPriorityNodes(input.ContextNodeIDs, input.TargetNodeID, attachmentNodes) {
			return agent.Run{}, fmt.Errorf("%w: contextNodeIds must be attachments of targetNodeId", ErrInvalidAgentRun)
		}
		input.ContextNodes = attachmentNodes
		input.Target = agent.NodeUpdateTarget(string(nodeKind))
	} else {
		expectedKind := canvas.NodeKindChapterOutline
		if input.Target == agent.TargetChapterSection {
			expectedKind = canvas.NodeKindSectionOutline
		}
		if input.Target == agent.TargetChapterArchive {
			expectedKind = canvas.NodeKindChapterOutline
		}
		if nodeKind != expectedKind {
			return agent.Run{}, fmt.Errorf("%w: target %q requires a %q node", ErrInvalidAgentRun, input.Target, expectedKind)
		}
		if input.Target == agent.TargetSectionOutlineBatch {
			input.ContextNodeIDs = []string{input.TargetNodeID}
		} else if input.Target == agent.TargetChapterArchive {
			contextNodeIDs, err := s.store.GetChapterArchiveContext(input.WorkID, input.TargetNodeID)
			if err != nil {
				return agent.Run{}, fmt.Errorf("%w: resolve chapter archive context: %v", ErrInvalidAgentRun, err)
			}
			input.ContextNodeIDs = contextNodeIDs
		} else {
			contextNodeIDs, err := s.store.GetChapterSectionContext(input.WorkID, input.TargetNodeID)
			if err != nil {
				return agent.Run{}, fmt.Errorf("%w: resolve chapter section context: %v", ErrInvalidAgentRun, err)
			}
			input.ContextNodeIDs = contextNodeIDs
		}
		contextNodes, err := s.store.GetNodeReferences(
			input.WorkID,
			withoutNodeID(input.ContextNodeIDs, input.TargetNodeID),
		)
		if err != nil {
			return agent.Run{}, err
		}
		input.ContextNodes = contextNodes
	}
	input.RunID = uuid.NewString()
	run, err := s.store.CreateRun(input)
	if err != nil {
		return agent.Run{}, err
	}
	go s.execute(run, input, executionFresh, "")
	return run, nil
}

func (s *AgentService) GetRun(runID string) (agent.Run, error) {
	return s.store.GetRun(strings.TrimSpace(runID))
}

func (s *AgentService) ListEvents(runID string, afterSequence int64) ([]agent.Event, error) {
	if _, err := s.store.GetRun(runID); err != nil {
		return nil, err
	}
	return s.store.ListEvents(runID, afterSequence)
}

func (s *AgentService) CancelRun(runID string) error {
	runID = strings.TrimSpace(runID)
	if err := s.store.Cancel(runID); err != nil {
		return err
	}
	s.mu.Lock()
	cancel := s.cancels[runID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// ResumeAfterCandidateDecision feeds a canvas review decision back into the
// same collaborative run so the next generation can account for it.
func (s *AgentService) ResumeAfterCandidateDecision(ctx context.Context, workID, candidateID string, accepted bool, acceptedNodeID string) error {
	run, candidate, err := s.store.GetRunByCandidate(strings.TrimSpace(candidateID), strings.TrimSpace(workID))
	if err != nil {
		return err
	}
	if !agent.IsCollaborativeTarget(run.Target) || run.Status != agent.RunStatusCompleted {
		return nil
	}
	if !accepted {
		return s.store.RequestCandidateDecisionReason(run.ID, candidate.ID, candidate.Title)
	}
	if err := s.store.RecordCandidateDecision(run.ID, candidate.ID, true, strings.TrimSpace(acceptedNodeID)); err != nil {
		return err
	}
	// Acceptance completes the product projection. A new user request, rather
	// than an implicit replay of the original prompt, starts further work.
	return nil
}

func (s *AgentService) RespondToRun(runID, approvalEventID, answer string) (agent.Run, error) {
	runID = strings.TrimSpace(runID)
	approvalEventID = strings.TrimSpace(approvalEventID)
	answer = strings.TrimSpace(answer)
	if runID == "" || approvalEventID == "" || answer == "" {
		return agent.Run{}, fmt.Errorf("%w: approvalEventId and answer are required", ErrInvalidAgentRun)
	}
	run, err := s.store.GetRun(runID)
	if err != nil {
		return agent.Run{}, err
	}
	if run.Status != agent.RunStatusWaitingInput {
		return agent.Run{}, agent.ErrRunNotWaitingInput
	}
	responses, err := s.store.ListUserResponses(runID)
	if err != nil {
		return agent.Run{}, err
	}
	input := agent.RunInput{
		RunID: run.ID, WorkID: run.WorkID, Prompt: run.Prompt, Target: run.Target,
		TargetNodeID: run.TargetNodeID, ProviderID: run.ProviderID, ModelID: run.ModelID, ConversationSessionID: run.ConversationSessionID,
		ContextNodeIDs: uniqueNodeIDs(run.ContextNodeIDs),
	}
	if agent.IsCollaborativeTarget(run.Target) {
		globalContext, err := s.store.GetGlobalContextNodeReferences(run.WorkID)
		if err != nil {
			return agent.Run{}, err
		}
		input.ContextNodes = append(input.ContextNodes, globalContext...)
		input.ContextNodeIDs = append(input.ContextNodeIDs, nodeReferenceIDs(globalContext)...)
		input.ContextNodeIDs = uniqueNodeIDs(input.ContextNodeIDs)
		if len(input.ContextNodeIDs) > 0 {
			contextNodes, err := s.store.GetNodeReferences(run.WorkID, input.ContextNodeIDs)
			if err != nil {
				return agent.Run{}, err
			}
			input.ContextNodes = contextNodes
		}
		queuedResponse, err := s.store.QueueResponse(runID, approvalEventID, answer)
		if err != nil {
			return agent.Run{}, err
		}
		input.UserResponses = append(responses, queuedResponse)
		input.CollaborativeCandidates, err = s.store.ListCollaborativeCandidates(run.ID)
		if err != nil {
			return agent.Run{}, err
		}
		run.Status = agent.RunStatusQueued
		run.ErrorMessage = ""
		resumeAnswer := queuedResponse.Answer
		if queuedResponse.Reason == "candidate_rejected" {
			resumeAnswer = ""
		}
		go s.execute(run, input, executionResume, resumeAnswer)
		return run, nil
	}
	nodeKind, targetNodeRevision, err := s.store.GetCanvasNodeMetadata(run.WorkID, run.TargetNodeID)
	if err != nil {
		return agent.Run{}, err
	}
	input.TargetNodeType = string(nodeKind)
	input.TargetNodeRevision = run.TargetNodeRevision
	if input.TargetNodeRevision == 0 {
		input.TargetNodeRevision = targetNodeRevision
	}
	if agent.IsNodeUpdateTarget(run.Target) || run.Target == agent.TargetNodeUpdate {
		input.Target = agent.NodeUpdateTarget(string(nodeKind))
		attachmentNodes, err := s.store.GetNodeAttachments(run.WorkID, run.TargetNodeID)
		if err != nil {
			return agent.Run{}, err
		}
		input.ContextNodes = attachmentNodes
		input.ContextNodeIDs = attachmentPriorityContextNodeIDs(input.ContextNodeIDs, run.TargetNodeID, attachmentNodes)
	} else {
		contextNodes, err := s.store.GetNodeReferences(
			run.WorkID,
			withoutNodeID(run.ContextNodeIDs, run.TargetNodeID),
		)
		if err != nil {
			return agent.Run{}, err
		}
		input.ContextNodes = contextNodes
	}
	queuedResponse, err := s.store.QueueResponse(runID, approvalEventID, answer)
	if err != nil {
		return agent.Run{}, err
	}
	input.UserResponses = append(responses, queuedResponse)
	run.Status = agent.RunStatusQueued
	run.ErrorMessage = ""
	go s.execute(run, input, executionResume, queuedResponse.Answer)
	return run, nil
}

func nodeReferenceIDs(references []agent.NodeReference) []string {
	ids := make([]string, 0, len(references))
	for _, reference := range references {
		if strings.TrimSpace(reference.ID) != "" {
			ids = append(ids, reference.ID)
		}
	}
	return ids
}

func (s *AgentService) execute(run agent.Run, input agent.RunInput, mode executionMode, resumeAnswer string) {
	runCtx, cancel := context.WithCancel(s.ctx)
	s.mu.Lock()
	s.cancels[run.ID] = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.cancels, run.ID)
		s.mu.Unlock()
	}()

	var startErr error
	switch mode {
	case executionResume:
		startErr = s.store.MarkResumed(run.ID)
	case executionRecover:
		startErr = s.store.MarkRecovered(run.ID)
	default:
		startErr = s.store.MarkStarted(run.ID)
	}
	if startErr != nil {
		if !errors.Is(startErr, agent.ErrRunNotCancellable) {
			s.logger.Error("start agent run", "runId", run.ID, "error", startErr)
		}
		return
	}
	emit := func(eventType agent.EventType, data any) error {
		_, err := s.store.AppendEvent(run.ID, eventType, data)
		return err
	}
	var result agent.RunResult
	var err error
	if mode == executionRecover {
		recoverable, ok := s.engine.(agent.RecoverableEngine)
		if !ok {
			err = errors.New("agent engine does not support durable recovery")
		} else {
			result, err = recoverable.Recover(runCtx, input, emit)
		}
	} else if resumeAnswer != "" {
		resumable, ok := s.engine.(agent.ResumableEngine)
		if !ok {
			err = errors.New("agent engine does not support durable resume")
		} else {
			result, err = resumable.Resume(runCtx, input, resumeAnswer, emit)
		}
	} else {
		result, err = s.engine.Run(runCtx, input, emit)
	}
	if err != nil {
		if errors.Is(runCtx.Err(), context.Canceled) {
			return
		}
		if errors.Is(err, agent.ErrApprovalRequired) {
			return
		}
		if failErr := s.store.Fail(run.ID, publicAgentError(err)); failErr != nil && !errors.Is(failErr, agent.ErrRunNotCancellable) {
			s.logger.Error("fail agent run", "runId", run.ID, "error", failErr)
		}
		s.logger.Warn("agent run failed", "runId", run.ID, "error", err)
		return
	}
	projection, projectionErr := s.store.PrepareProductProjection(run, result)
	if projectionErr != nil {
		if errors.Is(projectionErr, agent.ErrRunNotCancellable) {
			return
		}
		if errors.Is(projectionErr, agent.ErrProjectionConflict) {
			s.failProjection(run, result, agent.ProductProjectionConflict, projectionErr)
			return
		}
		s.logger.Error("prepare agent product projection", "runId", run.ID, "artifactId", result.ArtifactID, "error", projectionErr)
		s.scheduleProjectionRetry(run, input, result, 1, projectionErr)
		return
	}
	if projection.Status == agent.ProductProjectionCompleted {
		return
	}
	if projection.Status == agent.ProductProjectionConflict || projection.Status == agent.ProductProjectionFailed {
		s.failProjection(run, result, projection.Status, agent.ErrProjectionTerminal)
		return
	}
	completeErr := s.completeProductResult(runCtx, run, input, result)
	if completeErr == nil {
		return
	}
	if errors.Is(completeErr, agent.ErrRunNotCancellable) || errors.Is(runCtx.Err(), context.Canceled) {
		_ = s.store.RecordProductProjectionError(run.ID, result.ArtifactID, agent.ProductProjectionFailed, "run cancelled before product projection completed")
		return
	}
	status, retryable := productProjectionErrorDisposition(completeErr)
	if retryable {
		s.scheduleProjectionRetry(run, input, result, projection.Attempts, completeErr)
		return
	}
	s.failProjection(run, result, status, completeErr)
}

func (s *AgentService) completeProductResult(ctx context.Context, run agent.Run, input agent.RunInput, result agent.RunResult) error {
	if agent.IsCollaborativeTarget(input.Target) {
		if result.Content == "" {
			return s.store.CompleteReadOnly(run, result)
		} else if result.SkillID == "entity-creator" || result.SkillID == "chapter-creator" {
			return s.store.CompleteCollaborativeProposal(run, result)
		} else {
			return s.store.CompleteReadOnly(run, result)
		}
	} else if agent.IsNodeUpdateTarget(input.Target) {
		return s.store.CompleteNodeUpdate(ctx, run, input.TargetNodeID, result)
	} else if input.Target == agent.TargetSectionOutlineBatch || input.Target == agent.TargetChapterSection {
		return s.store.CompleteDerivation(ctx, run, input.TargetNodeID, result)
	} else if input.Target == agent.TargetChapterArchive {
		return s.store.CompleteChapterArchive(ctx, run, result)
	}
	_, err := s.store.Complete(run, result)
	return err
}

func (s *AgentService) failProjection(run agent.Run, result agent.RunResult, status agent.ProductProjectionStatus, projectionErr error) {
	message := publicAgentError(projectionErr)
	if err := s.store.RecordProductProjectionError(run.ID, result.ArtifactID, status, message); err != nil && !errors.Is(err, agent.ErrProjectionTerminal) {
		s.logger.Error("record terminal product projection", "runId", run.ID, "artifactId", result.ArtifactID, "error", err)
	}
	if err := s.store.Fail(run.ID, message); err != nil && !errors.Is(err, agent.ErrRunNotCancellable) {
		s.logger.Error("fail incomplete agent run", "runId", run.ID, "error", err)
	}
	s.logger.Error("complete agent product projection", "runId", run.ID, "artifactId", result.ArtifactID, "error", projectionErr)
}

func (s *AgentService) scheduleProjectionRetry(run agent.Run, input agent.RunInput, result agent.RunResult, attempt int, projectionErr error) {
	delay := projectionRetryDelay(attempt)
	if err := s.store.RecordProductProjectionError(run.ID, result.ArtifactID, agent.ProductProjectionPending, publicAgentError(projectionErr)); err != nil && !errors.Is(err, agent.ErrProjectionTerminal) {
		s.logger.Error("record retryable product projection", "runId", run.ID, "artifactId", result.ArtifactID, "error", err)
	}
	if err := s.store.RequeueProductProjection(run.ID, result.ArtifactID, attempt, delay); err != nil {
		if !errors.Is(err, agent.ErrRunNotCancellable) {
			s.logger.Error("requeue product projection", "runId", run.ID, "artifactId", result.ArtifactID, "error", err)
		}
		return
	}
	run.Status = agent.RunStatusQueued
	s.logger.Warn("agent product projection scheduled for retry", "runId", run.ID, "artifactId", result.ArtifactID, "attempt", attempt, "retryAfter", delay, "error", projectionErr)
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			s.execute(run, input, executionRecover, "")
		}
	}()
}

func (s *AgentService) RecoverInterruptedRuns() error {
	runs, err := s.store.ListInterruptedRuns()
	if err != nil {
		return err
	}
	for _, run := range runs {
		input, inputErr := s.recoveryInput(run)
		if inputErr != nil {
			s.logger.Error("prepare interrupted agent run recovery", "runId", run.ID, "error", inputErr)
			_ = s.store.Fail(run.ID, publicAgentError(inputErr))
			continue
		}
		go s.execute(run, input, executionRecover, "")
	}
	return nil
}

func (s *AgentService) recoveryInput(run agent.Run) (agent.RunInput, error) {
	input := agent.RunInput{
		RunID: run.ID, WorkID: run.WorkID, Prompt: run.Prompt, Target: run.Target,
		TargetNodeID: run.TargetNodeID, ProviderID: run.ProviderID, ModelID: run.ModelID, ConversationSessionID: run.ConversationSessionID,
		ContextNodeIDs: uniqueNodeIDs(run.ContextNodeIDs),
	}
	if !agent.IsCollaborativeTarget(run.Target) {
		nodeKind, revision, err := s.store.GetCanvasNodeMetadata(run.WorkID, run.TargetNodeID)
		if err != nil {
			return agent.RunInput{}, err
		}
		input.TargetNodeType, input.TargetNodeRevision = string(nodeKind), run.TargetNodeRevision
		if input.TargetNodeRevision == 0 {
			input.TargetNodeRevision = revision
		}
		if agent.IsNodeUpdateTarget(run.Target) || run.Target == agent.TargetNodeUpdate {
			input.Target = agent.NodeUpdateTarget(string(nodeKind))
			input.ContextNodes, err = s.store.GetNodeAttachments(run.WorkID, run.TargetNodeID)
			if err != nil {
				return agent.RunInput{}, err
			}
			input.ContextNodeIDs = attachmentPriorityContextNodeIDs(input.ContextNodeIDs, run.TargetNodeID, input.ContextNodes)
		} else {
			input.ContextNodes, err = s.store.GetNodeReferences(run.WorkID, withoutNodeID(input.ContextNodeIDs, run.TargetNodeID))
			if err != nil {
				return agent.RunInput{}, err
			}
		}
		input.UserResponses, err = s.store.ListUserResponses(run.ID)
		if err != nil {
			return agent.RunInput{}, err
		}
		return input, nil
	}
	globalContext, err := s.store.GetGlobalContextNodeReferences(run.WorkID)
	if err != nil {
		return agent.RunInput{}, err
	}
	input.ContextNodes = append(input.ContextNodes, globalContext...)
	input.ContextNodeIDs = uniqueNodeIDs(append(input.ContextNodeIDs, nodeReferenceIDs(globalContext)...))
	if len(input.ContextNodeIDs) > 0 {
		input.ContextNodes, err = s.store.GetNodeReferences(run.WorkID, input.ContextNodeIDs)
		if err != nil {
			return agent.RunInput{}, err
		}
	}
	input.UserResponses, err = s.store.ListUserResponses(run.ID)
	if err != nil {
		return agent.RunInput{}, err
	}
	input.CollaborativeCandidates, err = s.store.ListCollaborativeCandidates(run.ID)
	if err != nil {
		return agent.RunInput{}, err
	}
	return input, nil
}

func containsNodeID(nodeIDs []string, targetNodeID string) bool {
	for _, nodeID := range nodeIDs {
		if strings.TrimSpace(nodeID) == targetNodeID {
			return true
		}
	}
	return false
}

func uniqueNodeIDs(nodeIDs []string) []string {
	seen := make(map[string]struct{}, len(nodeIDs))
	result := make([]string, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" {
			continue
		}
		if _, exists := seen[nodeID]; exists {
			continue
		}
		seen[nodeID] = struct{}{}
		result = append(result, nodeID)
	}
	return result
}

func hasOnlyAttachmentPriorityNodes(
	contextNodeIDs []string,
	targetNodeID string,
	attachments []agent.NodeReference,
) bool {
	attachmentNodeIDs := make(map[string]struct{}, len(attachments))
	for _, attachment := range attachments {
		attachmentNodeIDs[attachment.ID] = struct{}{}
	}
	for _, nodeID := range contextNodeIDs {
		if nodeID == targetNodeID {
			continue
		}
		if _, exists := attachmentNodeIDs[nodeID]; !exists {
			return false
		}
	}
	return true
}

func attachmentPriorityContextNodeIDs(
	contextNodeIDs []string,
	targetNodeID string,
	attachments []agent.NodeReference,
) []string {
	attachmentNodeIDs := make(map[string]struct{}, len(attachments))
	for _, attachment := range attachments {
		attachmentNodeIDs[attachment.ID] = struct{}{}
	}
	priorityNodeIDs := make([]string, 0, len(contextNodeIDs)+1)
	if targetNodeID != "" {
		priorityNodeIDs = append(priorityNodeIDs, targetNodeID)
	}
	for _, nodeID := range uniqueNodeIDs(contextNodeIDs) {
		if nodeID == targetNodeID {
			continue
		}
		if _, isAttachment := attachmentNodeIDs[nodeID]; isAttachment {
			priorityNodeIDs = append(priorityNodeIDs, nodeID)
		}
	}
	return priorityNodeIDs
}

func withoutNodeID(nodeIDs []string, excludedNodeID string) []string {
	result := make([]string, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if nodeID != excludedNodeID {
			result = append(result, nodeID)
		}
	}
	return result
}

func publicAgentError(err error) string {
	if errors.Is(err, agent.ErrCanvasUnavailable) {
		return "画布上下文读取尚未接入，请暂时不选择节点后重试"
	}
	if errors.Is(err, canvas.ErrRevisionConflict) {
		return "节点在生成期间已被修改，本次 Agent 结果未覆盖现有内容"
	}
	if errors.Is(err, canvas.ErrDerivationExists) {
		return "当前节点已经生成过下一层节点，请先检查或删除已有结果"
	}
	if errors.Is(err, canvas.ErrInvalidSectionOutline) || errors.Is(err, canvas.ErrInvalidNode) {
		return "模型返回的派生内容不符合节点结构，请重试或切换模型"
	}
	if errors.Is(err, canvas.ErrInvalidChapterArchive) {
		return "模型返回的章节归档不完整，请重试或切换模型"
	}
	if errors.Is(err, canvas.ErrChapterArchiveIncomplete) {
		return "章节仍有未完成的小节，完成全部章节小节后才能归档"
	}
	if errors.Is(err, canvas.ErrArchivedNodeLocked) {
		return "章节已归档并锁定，不能再次修改或归档"
	}
	if errors.Is(err, agent.ErrProjectionTerminal) {
		return "模型产物无法投影到画布，请重试或切换模型"
	}
	return "Agent 执行失败，请重试或查看 Core 日志"
}

func productProjectionErrorDisposition(err error) (agent.ProductProjectionStatus, bool) {
	if errors.Is(err, canvas.ErrRevisionConflict) || errors.Is(err, canvas.ErrDerivationExists) ||
		errors.Is(err, canvas.ErrArchivedNodeLocked) || errors.Is(err, canvas.ErrNodeNotFound) ||
		errors.Is(err, agent.ErrProjectionConflict) {
		return agent.ProductProjectionConflict, false
	}
	if errors.Is(err, canvas.ErrInvalidNode) || errors.Is(err, canvas.ErrInvalidSectionOutline) ||
		errors.Is(err, canvas.ErrInvalidChapterArchive) || errors.Is(err, canvas.ErrChapterArchiveIncomplete) ||
		errors.Is(err, agent.ErrProjectionTerminal) {
		return agent.ProductProjectionFailed, false
	}
	return agent.ProductProjectionPending, true
}

func projectionRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<uint(attempt-1)) * time.Second
}

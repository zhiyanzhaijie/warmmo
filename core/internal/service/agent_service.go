package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"

	"warmnote/core/internal/agent"
	"warmnote/core/internal/canvas"
	"warmnote/core/internal/repository"
)

var ErrInvalidAgentRun = errors.New("invalid agent run")

type AgentService struct {
	ctx        context.Context
	repository *repository.AgentRepository
	engine     agent.Engine
	logger     *slog.Logger
	mu         sync.Mutex
	cancels    map[string]context.CancelFunc
}

func NewAgentService(ctx context.Context, repository *repository.AgentRepository, engine agent.Engine, logger *slog.Logger) *AgentService {
	return &AgentService{ctx: ctx, repository: repository, engine: engine, logger: logger, cancels: make(map[string]context.CancelFunc)}
}

func (s *AgentService) CreateRun(input agent.RunInput) (agent.Run, error) {
	input.WorkID = strings.TrimSpace(input.WorkID)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Target = strings.TrimSpace(input.Target)
	input.TargetNodeID = strings.TrimSpace(input.TargetNodeID)
	input.ProviderID = strings.TrimSpace(input.ProviderID)
	input.ModelID = strings.TrimSpace(input.ModelID)
	input.ContextNodeIDs = uniqueNodeIDs(input.ContextNodeIDs)
	if input.WorkID == "" || input.Prompt == "" || input.Target == "" || input.ProviderID == "" || input.ModelID == "" {
		return agent.Run{}, fmt.Errorf("%w: workId, prompt, target, providerId and modelId are required", ErrInvalidAgentRun)
	}
	if input.Target != agent.TargetNodeUpdate &&
		input.Target != agent.TargetSectionOutlineBatch &&
		input.Target != agent.TargetChapterSection &&
		input.Target != agent.TargetChapterArchive {
		return agent.Run{}, fmt.Errorf("%w: target %q is not supported", ErrInvalidAgentRun, input.Target)
	}
	if input.TargetNodeID == "" {
		return agent.Run{}, fmt.Errorf("%w: targetNodeId is required", ErrInvalidAgentRun)
	}
	if input.Target == agent.TargetNodeUpdate && !containsNodeID(input.ContextNodeIDs, input.TargetNodeID) {
		return agent.Run{}, fmt.Errorf("%w: targetNodeId must be included in contextNodeIds", ErrInvalidAgentRun)
	}
	nodeKind, targetNodeRevision, err := s.repository.GetCanvasNodeMetadata(input.WorkID, input.TargetNodeID)
	if err != nil {
		return agent.Run{}, err
	}
	input.TargetNodeType = string(nodeKind)
	input.TargetNodeRevision = targetNodeRevision
	if input.Target == agent.TargetNodeUpdate {
		if !canvas.IsValidNodeKind(nodeKind) {
			return agent.Run{}, fmt.Errorf("%w: target node kind %q is not supported", ErrInvalidAgentRun, nodeKind)
		}
		attachmentNodes, err := s.repository.GetNodeAttachments(input.WorkID, input.TargetNodeID)
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
			contextNodeIDs, err := s.repository.GetChapterArchiveContext(input.WorkID, input.TargetNodeID)
			if err != nil {
				return agent.Run{}, fmt.Errorf("%w: resolve chapter archive context: %v", ErrInvalidAgentRun, err)
			}
			input.ContextNodeIDs = contextNodeIDs
		} else {
			contextNodeIDs, err := s.repository.GetChapterSectionContext(input.WorkID, input.TargetNodeID)
			if err != nil {
				return agent.Run{}, fmt.Errorf("%w: resolve chapter section context: %v", ErrInvalidAgentRun, err)
			}
			input.ContextNodeIDs = contextNodeIDs
		}
		contextNodes, err := s.repository.GetNodeReferences(
			input.WorkID,
			withoutNodeID(input.ContextNodeIDs, input.TargetNodeID),
		)
		if err != nil {
			return agent.Run{}, err
		}
		input.ContextNodes = contextNodes
	}
	input.RunID = uuid.NewString()
	run, err := s.repository.CreateRun(input)
	if err != nil {
		return agent.Run{}, err
	}
	go s.execute(run, input, false)
	return run, nil
}

func (s *AgentService) GetRun(runID string) (agent.Run, error) {
	return s.repository.GetRun(strings.TrimSpace(runID))
}

func (s *AgentService) ListEvents(runID string, afterSequence int64) ([]agent.Event, error) {
	if _, err := s.repository.GetRun(runID); err != nil {
		return nil, err
	}
	return s.repository.ListEvents(runID, afterSequence)
}

func (s *AgentService) CancelRun(runID string) error {
	runID = strings.TrimSpace(runID)
	if err := s.repository.Cancel(runID); err != nil {
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

func (s *AgentService) RespondToRun(runID, approvalEventID, answer string) (agent.Run, error) {
	runID = strings.TrimSpace(runID)
	approvalEventID = strings.TrimSpace(approvalEventID)
	answer = strings.TrimSpace(answer)
	if runID == "" || approvalEventID == "" || answer == "" {
		return agent.Run{}, fmt.Errorf("%w: approvalEventId and answer are required", ErrInvalidAgentRun)
	}
	run, err := s.repository.GetRun(runID)
	if err != nil {
		return agent.Run{}, err
	}
	if run.Status != agent.RunStatusWaitingInput {
		return agent.Run{}, agent.ErrRunNotWaitingInput
	}
	responses, err := s.repository.ListUserResponses(runID)
	if err != nil {
		return agent.Run{}, err
	}
	input := agent.RunInput{
		RunID: run.ID, WorkID: run.WorkID, Prompt: run.Prompt, Target: run.Target,
		TargetNodeID: run.TargetNodeID, ProviderID: run.ProviderID, ModelID: run.ModelID,
		ContextNodeIDs: uniqueNodeIDs(run.ContextNodeIDs),
	}
	nodeKind, targetNodeRevision, err := s.repository.GetCanvasNodeMetadata(run.WorkID, run.TargetNodeID)
	if err != nil {
		return agent.Run{}, err
	}
	input.TargetNodeType = string(nodeKind)
	input.TargetNodeRevision = targetNodeRevision
	if agent.IsNodeUpdateTarget(run.Target) || run.Target == agent.TargetNodeUpdate {
		input.Target = agent.NodeUpdateTarget(string(nodeKind))
		attachmentNodes, err := s.repository.GetNodeAttachments(run.WorkID, run.TargetNodeID)
		if err != nil {
			return agent.Run{}, err
		}
		input.ContextNodes = attachmentNodes
		input.ContextNodeIDs = attachmentPriorityContextNodeIDs(input.ContextNodeIDs, run.TargetNodeID, attachmentNodes)
	} else {
		contextNodes, err := s.repository.GetNodeReferences(
			run.WorkID,
			withoutNodeID(run.ContextNodeIDs, run.TargetNodeID),
		)
		if err != nil {
			return agent.Run{}, err
		}
		input.ContextNodes = contextNodes
	}
	queuedResponse, err := s.repository.QueueResponse(runID, approvalEventID, answer)
	if err != nil {
		return agent.Run{}, err
	}
	input.UserResponses = append(responses, queuedResponse)
	run.Status = agent.RunStatusQueued
	run.ErrorMessage = ""
	go s.execute(run, input, true)
	return run, nil
}

func (s *AgentService) execute(run agent.Run, input agent.RunInput, resumed bool) {
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
	if resumed {
		startErr = s.repository.MarkResumed(run.ID)
	} else {
		startErr = s.repository.MarkStarted(run.ID)
	}
	if startErr != nil {
		if !errors.Is(startErr, agent.ErrRunNotCancellable) {
			s.logger.Error("start agent run", "runId", run.ID, "error", startErr)
		}
		return
	}
	emit := func(eventType agent.EventType, data any) error {
		_, err := s.repository.AppendEvent(run.ID, eventType, data)
		return err
	}
	result, err := s.engine.Run(runCtx, input, emit)
	if err != nil {
		if errors.Is(runCtx.Err(), context.Canceled) {
			return
		}
		if errors.Is(err, agent.ErrApprovalRequired) {
			return
		}
		if failErr := s.repository.Fail(run.ID, publicAgentError(err)); failErr != nil && !errors.Is(failErr, agent.ErrRunNotCancellable) {
			s.logger.Error("fail agent run", "runId", run.ID, "error", failErr)
		}
		s.logger.Warn("agent run failed", "runId", run.ID, "error", err)
		return
	}
	var completeErr error
	if agent.IsNodeUpdateTarget(input.Target) {
		completeErr = s.repository.CompleteNodeUpdate(runCtx, run, input.TargetNodeID, result)
	} else if input.Target == agent.TargetSectionOutlineBatch || input.Target == agent.TargetChapterSection {
		completeErr = s.repository.CompleteDerivation(runCtx, run, input.TargetNodeID, result)
	} else if input.Target == agent.TargetChapterArchive {
		completeErr = s.repository.CompleteChapterArchive(runCtx, run, result)
	} else {
		_, completeErr = s.repository.Complete(run, result)
	}
	if completeErr != nil && !errors.Is(completeErr, agent.ErrRunNotCancellable) {
		message := publicAgentError(completeErr)
		if failErr := s.repository.Fail(run.ID, message); failErr != nil && !errors.Is(failErr, agent.ErrRunNotCancellable) {
			s.logger.Error("fail incomplete agent run", "runId", run.ID, "error", failErr)
		}
		s.logger.Error("complete agent run", "runId", run.ID, "error", completeErr)
	}
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
	if errors.Is(err, agent.ErrInvalidDecision) {
		return "模型未返回有效的 Agent 决策，请重试或切换模型"
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
	return "Agent 执行失败，请检查模型配置后重试"
}

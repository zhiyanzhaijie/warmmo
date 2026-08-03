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
	input.ProviderID = strings.TrimSpace(input.ProviderID)
	input.ModelID = strings.TrimSpace(input.ModelID)
	if input.WorkID == "" || input.Prompt == "" || input.Target == "" || input.ProviderID == "" || input.ModelID == "" {
		return agent.Run{}, fmt.Errorf("%w: workId, prompt, target, providerId and modelId are required", ErrInvalidAgentRun)
	}
	if input.Target != "chapter" {
		return agent.Run{}, fmt.Errorf("%w: target %q is not supported", ErrInvalidAgentRun, input.Target)
	}
	input.RunID = uuid.NewString()
	run, err := s.repository.CreateRun(input)
	if err != nil {
		return agent.Run{}, err
	}
	go s.execute(run, input)
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

func (s *AgentService) execute(run agent.Run, input agent.RunInput) {
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

	if err := s.repository.MarkStarted(run.ID); err != nil {
		if !errors.Is(err, agent.ErrRunNotCancellable) {
			s.logger.Error("start agent run", "runId", run.ID, "error", err)
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
		if failErr := s.repository.Fail(run.ID, publicAgentError(err)); failErr != nil && !errors.Is(failErr, agent.ErrRunNotCancellable) {
			s.logger.Error("fail agent run", "runId", run.ID, "error", failErr)
		}
		s.logger.Warn("agent run failed", "runId", run.ID, "error", err)
		return
	}
	if _, err := s.repository.Complete(run, result); err != nil && !errors.Is(err, agent.ErrRunNotCancellable) {
		s.logger.Error("complete agent run", "runId", run.ID, "error", err)
	}
}

func publicAgentError(err error) string {
	if errors.Is(err, agent.ErrCanvasUnavailable) {
		return "画布上下文读取尚未接入，请暂时不选择节点后重试"
	}
	return "Agent 执行失败，请检查模型配置后重试"
}

package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	appharness "warmmo/core/internal/application/harness"
)

type AgentChildRunStore struct {
	database *gorm.DB
}

func NewAgentChildRunStore(database *Database) *AgentChildRunStore {
	if database == nil {
		return &AgentChildRunStore{}
	}
	return &AgentChildRunStore{database: database.DB}
}

func (s *AgentChildRunStore) StartChildRun(ctx context.Context, child appharness.ChildRun) (appharness.ChildRun, error) {
	if s == nil || s.database == nil {
		return appharness.ChildRun{}, errors.New("agent child run store is not configured")
	}
	if strings.TrimSpace(child.ID) == "" || strings.TrimSpace(child.RunID) == "" || strings.TrimSpace(child.ParentTurnID) == "" ||
		strings.TrimSpace(child.ParentAgentID) == "" || strings.TrimSpace(child.ChildTurnID) == "" ||
		strings.TrimSpace(child.ChildAgentID) == "" || strings.TrimSpace(child.SessionID) == "" {
		return appharness.ChildRun{}, errors.New("child run identity and lineage are required")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	model := agentChildRunModel{
		ID: child.ID, RunID: child.RunID, ParentTurnID: child.ParentTurnID, ParentAgentID: child.ParentAgentID,
		ChildTurnID: child.ChildTurnID, ChildAgentID: child.ChildAgentID, SessionID: child.SessionID,
		Status: string(appharness.TurnRunning), ArtifactJSON: "null", PendingJSON: "null", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.database.WithContext(ctx).Create(&model).Error; err != nil {
		var existing agentChildRunModel
		if loadErr := s.database.WithContext(ctx).First(&existing, "child_turn_id = ?", child.ChildTurnID).Error; loadErr != nil {
			return appharness.ChildRun{}, fmt.Errorf("start agent child run: %w", err)
		}
		if existing.RunID != model.RunID || existing.ParentTurnID != model.ParentTurnID ||
			existing.ParentAgentID != model.ParentAgentID || existing.ChildAgentID != model.ChildAgentID || existing.SessionID != model.SessionID {
			return appharness.ChildRun{}, fmt.Errorf("child run lineage conflict: %s", child.ChildTurnID)
		}
		return childRunFromModel(existing)
	}
	return childRunFromModel(model)
}

func (s *AgentChildRunStore) FinishChildRun(ctx context.Context, childTurnID string, outcome appharness.TurnOutcome) (appharness.ChildRun, error) {
	if s == nil || s.database == nil {
		return appharness.ChildRun{}, errors.New("agent child run store is not configured")
	}
	artifact, err := json.Marshal(outcome.Artifact)
	if err != nil {
		return appharness.ChildRun{}, err
	}
	pending, err := json.Marshal(outcome.Pending)
	if err != nil {
		return appharness.ChildRun{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	result := s.database.WithContext(ctx).Model(&agentChildRunModel{}).Where("child_turn_id = ?", childTurnID).
		Updates(map[string]any{
			"status": string(outcome.Status), "stop_reason": string(outcome.StopReason),
			"artifact_json": string(artifact), "pending_json": string(pending), "updated_at": now,
		})
	if result.Error != nil {
		return appharness.ChildRun{}, fmt.Errorf("finish agent child run: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return appharness.ChildRun{}, appharness.ErrChildRunNotFound
	}
	return s.GetChildRunByTurn(ctx, childTurnID)
}

func (s *AgentChildRunStore) GetChildRunByTurn(ctx context.Context, childTurnID string) (appharness.ChildRun, error) {
	if s == nil || s.database == nil {
		return appharness.ChildRun{}, errors.New("agent child run store is not configured")
	}
	var model agentChildRunModel
	err := s.database.WithContext(ctx).First(&model, "child_turn_id = ?", childTurnID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appharness.ChildRun{}, appharness.ErrChildRunNotFound
	}
	if err != nil {
		return appharness.ChildRun{}, err
	}
	return childRunFromModel(model)
}

func childRunFromModel(model agentChildRunModel) (appharness.ChildRun, error) {
	var artifact *appharness.ArtifactRef
	if model.ArtifactJSON != "" && model.ArtifactJSON != "null" {
		artifact = &appharness.ArtifactRef{}
		if err := json.Unmarshal([]byte(model.ArtifactJSON), artifact); err != nil {
			return appharness.ChildRun{}, err
		}
	}
	var pending *appharness.PendingAction
	if model.PendingJSON != "" && model.PendingJSON != "null" {
		pending = &appharness.PendingAction{}
		if err := json.Unmarshal([]byte(model.PendingJSON), pending); err != nil {
			return appharness.ChildRun{}, err
		}
	}
	return appharness.ChildRun{
		ID: model.ID, RunID: model.RunID, ParentTurnID: model.ParentTurnID, ParentAgentID: model.ParentAgentID,
		ChildTurnID: model.ChildTurnID, ChildAgentID: model.ChildAgentID, SessionID: model.SessionID,
		Status: appharness.TurnStatus(model.Status), StopReason: appharness.StopReason(model.StopReason),
		Artifact: artifact, Pending: pending, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}, nil
}

var _ appharness.ChildRunStore = (*AgentChildRunStore)(nil)

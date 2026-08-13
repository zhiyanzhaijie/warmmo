package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	appharness "warmmo/core/internal/application/harness"
)

type AgentCheckpointStore struct {
	database *gorm.DB
}

func NewAgentCheckpointStore(database *Database) *AgentCheckpointStore {
	if database == nil {
		return &AgentCheckpointStore{}
	}
	return &AgentCheckpointStore{database: database.DB}
}

func (s *AgentCheckpointStore) GetCheckpoint(ctx context.Context, turnID string) (appharness.Checkpoint, error) {
	if s == nil || s.database == nil {
		return appharness.Checkpoint{}, errors.New("agent checkpoint store is not configured")
	}
	var model agentTurnCheckpointModel
	err := s.database.WithContext(ctx).First(&model, "turn_id = ?", turnID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appharness.Checkpoint{}, fmt.Errorf("%w: %s", appharness.ErrCheckpointNotFound, turnID)
	}
	if err != nil {
		return appharness.Checkpoint{}, fmt.Errorf("read agent turn checkpoint: %w", err)
	}
	return checkpointFromModel(model)
}

func (s *AgentCheckpointStore) FindLatestCheckpoint(ctx context.Context, runID, agentID string) (appharness.Checkpoint, error) {
	if s == nil || s.database == nil {
		return appharness.Checkpoint{}, errors.New("agent checkpoint store is not configured")
	}
	var model agentTurnCheckpointModel
	err := s.database.WithContext(ctx).
		Where("run_id = ? AND agent_id = ?", runID, agentID).
		Order("updated_at DESC").First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appharness.Checkpoint{}, fmt.Errorf("%w: latest run %s agent %s", appharness.ErrCheckpointNotFound, runID, agentID)
	}
	if err != nil {
		return appharness.Checkpoint{}, fmt.Errorf("find latest agent turn checkpoint: %w", err)
	}
	return checkpointFromModel(model)
}

func (s *AgentCheckpointStore) FindPendingCheckpoint(ctx context.Context, runID string) (appharness.Checkpoint, error) {
	if s == nil || s.database == nil {
		return appharness.Checkpoint{}, errors.New("agent checkpoint store is not configured")
	}
	var model agentTurnCheckpointModel
	err := s.database.WithContext(ctx).
		Where("run_id = ? AND status = ?", runID, string(appharness.TurnAwaitingUser)).
		Order("updated_at DESC").First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appharness.Checkpoint{}, fmt.Errorf("%w: pending run %s", appharness.ErrCheckpointNotFound, runID)
	}
	if err != nil {
		return appharness.Checkpoint{}, fmt.Errorf("find pending agent turn checkpoint: %w", err)
	}
	return checkpointFromModel(model)
}

func (s *AgentCheckpointStore) SaveCheckpoint(ctx context.Context, checkpoint appharness.Checkpoint) (appharness.Checkpoint, error) {
	if s == nil || s.database == nil {
		return appharness.Checkpoint{}, errors.New("agent checkpoint store is not configured")
	}
	if checkpoint.TurnID == "" || checkpoint.RunID == "" || checkpoint.SessionID == "" || checkpoint.AgentID == "" {
		return appharness.Checkpoint{}, errors.New("run, turn, session and agent IDs are required")
	}
	if checkpoint.Status == "" || checkpoint.Version < 0 || checkpoint.Usage.InputTokens < 0 ||
		checkpoint.Usage.CachedInputTokens < 0 || checkpoint.Usage.OutputTokens < 0 ||
		checkpoint.Budget.ModelCalls < 0 || checkpoint.Budget.ToolCalls < 0 || checkpoint.Budget.SideEffectCalls < 0 {
		return appharness.Checkpoint{}, errors.New("checkpoint status, version, usage and budget counters are invalid")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	model, err := checkpointToModel(checkpoint, now)
	if err != nil {
		return appharness.Checkpoint{}, err
	}
	if checkpoint.Version == 0 {
		model.Version = 1
		if err := s.database.WithContext(ctx).Create(&model).Error; err != nil {
			var count int64
			if countErr := s.database.WithContext(ctx).Model(&agentTurnCheckpointModel{}).Where("turn_id = ?", checkpoint.TurnID).Count(&count).Error; countErr == nil && count > 0 {
				return appharness.Checkpoint{}, fmt.Errorf("%w: %s", appharness.ErrCheckpointConflict, checkpoint.TurnID)
			}
			return appharness.Checkpoint{}, fmt.Errorf("create agent turn checkpoint: %w", err)
		}
		return checkpointFromModel(model)
	}
	model.Version = checkpoint.Version + 1
	model.UpdatedAt = now
	result := s.database.WithContext(ctx).Model(&agentTurnCheckpointModel{}).
		Where("turn_id = ? AND version = ?", checkpoint.TurnID, checkpoint.Version).
		Select(
			"RunID", "SessionID", "AgentID", "DefinitionVersion", "DefinitionHash", "PromptHash", "ToolsetHash",
			"Status", "StopReason", "FinalJSON", "PendingJSON", "OutputJSON", "SnapshotJSON", "InputTokens", "CachedInputTokens", "OutputTokens",
			"ModelCalls", "ToolCalls", "SideEffectCalls",
			"Version", "UpdatedAt",
		).Updates(&model)
	if result.Error != nil {
		return appharness.Checkpoint{}, fmt.Errorf("update agent turn checkpoint: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return appharness.Checkpoint{}, fmt.Errorf("%w: %s", appharness.ErrCheckpointConflict, checkpoint.TurnID)
	}
	return checkpointFromModel(model)
}

func checkpointToModel(checkpoint appharness.Checkpoint, now time.Time) (agentTurnCheckpointModel, error) {
	final, err := json.Marshal(checkpoint.Final)
	if err != nil {
		return agentTurnCheckpointModel{}, fmt.Errorf("encode checkpoint final message: %w", err)
	}
	pending, err := json.Marshal(checkpoint.Pending)
	if err != nil {
		return agentTurnCheckpointModel{}, fmt.Errorf("encode checkpoint pending action: %w", err)
	}
	output := checkpoint.Output
	if len(output) == 0 {
		output = json.RawMessage("null")
	} else if !json.Valid(output) {
		return agentTurnCheckpointModel{}, errors.New("checkpoint structured output is not valid JSON")
	}
	snapshot, err := json.Marshal(checkpoint.Snapshot)
	if err != nil {
		return agentTurnCheckpointModel{}, fmt.Errorf("encode checkpoint turn snapshot: %w", err)
	}
	return agentTurnCheckpointModel{
		TurnID: checkpoint.TurnID, RunID: checkpoint.RunID, SessionID: checkpoint.SessionID, AgentID: checkpoint.AgentID,
		DefinitionVersion: checkpoint.DefinitionVersion, DefinitionHash: checkpoint.DefinitionHash,
		PromptHash: checkpoint.PromptHash, ToolsetHash: checkpoint.ToolsetHash,
		Status: string(checkpoint.Status), StopReason: string(checkpoint.StopReason),
		FinalJSON: string(final), PendingJSON: string(pending), OutputJSON: string(output), SnapshotJSON: string(snapshot),
		InputTokens: checkpoint.Usage.InputTokens, CachedInputTokens: checkpoint.Usage.CachedInputTokens,
		OutputTokens: checkpoint.Usage.OutputTokens,
		ModelCalls:   checkpoint.Budget.ModelCalls, ToolCalls: checkpoint.Budget.ToolCalls,
		SideEffectCalls: checkpoint.Budget.SideEffectCalls,
		Version:         checkpoint.Version, UpdatedAt: now,
	}, nil
}

func checkpointFromModel(model agentTurnCheckpointModel) (appharness.Checkpoint, error) {
	var final *appharness.Message
	if model.FinalJSON != "" && model.FinalJSON != "null" {
		final = &appharness.Message{}
		if err := json.Unmarshal([]byte(model.FinalJSON), final); err != nil {
			return appharness.Checkpoint{}, fmt.Errorf("decode checkpoint final message: %w", err)
		}
	}
	var pending *appharness.PendingAction
	if model.PendingJSON != "" && model.PendingJSON != "null" {
		pending = &appharness.PendingAction{}
		if err := json.Unmarshal([]byte(model.PendingJSON), pending); err != nil {
			return appharness.Checkpoint{}, fmt.Errorf("decode checkpoint pending action: %w", err)
		}
	}
	var snapshot *appharness.TurnSnapshot
	if model.SnapshotJSON != "" && model.SnapshotJSON != "null" {
		snapshot = &appharness.TurnSnapshot{}
		if err := json.Unmarshal([]byte(model.SnapshotJSON), snapshot); err != nil {
			return appharness.Checkpoint{}, fmt.Errorf("decode checkpoint turn snapshot: %w", err)
		}
	}
	output := json.RawMessage(model.OutputJSON)
	if len(output) == 0 || string(output) == "null" {
		output = nil
	} else if !json.Valid(output) {
		return appharness.Checkpoint{}, errors.New("checkpoint structured output is not valid JSON")
	}
	return appharness.Checkpoint{
		TurnID: model.TurnID, RunID: model.RunID, SessionID: model.SessionID, AgentID: model.AgentID,
		DefinitionVersion: model.DefinitionVersion, DefinitionHash: model.DefinitionHash,
		PromptHash: model.PromptHash, ToolsetHash: model.ToolsetHash,
		Status: appharness.TurnStatus(model.Status), StopReason: appharness.StopReason(model.StopReason),
		Final: final, Pending: pending, Output: output,
		Usage: appharness.Usage{
			InputTokens: model.InputTokens, CachedInputTokens: model.CachedInputTokens, OutputTokens: model.OutputTokens,
		},
		Budget: appharness.BudgetUsage{
			ModelCalls: model.ModelCalls, ToolCalls: model.ToolCalls, SideEffectCalls: model.SideEffectCalls,
		},
		Version: model.Version, UpdatedAt: model.UpdatedAt,
		Snapshot: snapshot,
	}, nil
}

var _ appharness.CheckpointStore = (*AgentCheckpointStore)(nil)

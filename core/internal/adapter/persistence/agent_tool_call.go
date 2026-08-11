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

type AgentToolCallStore struct {
	database *gorm.DB
}

func NewAgentToolCallStore(database *Database) *AgentToolCallStore {
	if database == nil {
		return &AgentToolCallStore{}
	}
	return &AgentToolCallStore{database: database.DB}
}

func (s *AgentToolCallStore) ClaimToolCall(ctx context.Context, record appharness.ToolCallRecord) (appharness.ToolCallRecord, bool, error) {
	if s == nil || s.database == nil {
		return appharness.ToolCallRecord{}, false, errors.New("agent tool call store is not configured")
	}
	if strings.TrimSpace(record.CallID) == "" || strings.TrimSpace(record.RunID) == "" || strings.TrimSpace(record.TurnID) == "" ||
		strings.TrimSpace(record.ToolName) == "" || strings.TrimSpace(record.ArgsHash) == "" || strings.TrimSpace(record.SideEffect) == "" {
		return appharness.ToolCallRecord{}, false, errors.New("tool call identity and side-effect class are required")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	model := agentToolCallModel{
		CallID: record.CallID, RunID: record.RunID, TurnID: record.TurnID, ToolName: record.ToolName,
		ArgsHash: record.ArgsHash, SideEffect: record.SideEffect, Status: string(appharness.ToolCallStarted),
		ResultJSON: "null", CreatedAt: now, UpdatedAt: now,
	}
	if err := s.database.WithContext(ctx).Create(&model).Error; err == nil {
		return toolCallFromModel(model), true, nil
	}
	var existing agentToolCallModel
	if err := s.database.WithContext(ctx).First(&existing, "call_id = ?", record.CallID).Error; err != nil {
		return appharness.ToolCallRecord{}, false, fmt.Errorf("claim agent tool call: %w", err)
	}
	if existing.RunID != record.RunID || existing.TurnID != record.TurnID || existing.ToolName != record.ToolName ||
		existing.ArgsHash != record.ArgsHash || existing.SideEffect != record.SideEffect {
		return appharness.ToolCallRecord{}, false, fmt.Errorf("%w: %s", appharness.ErrToolCallConflict, record.CallID)
	}
	stored := toolCallFromModel(existing)
	if stored.Status == appharness.ToolCallStarted {
		return stored, false, fmt.Errorf("%w: %s", appharness.ErrToolCallInDoubt, record.CallID)
	}
	if stored.Status != appharness.ToolCallCompleted {
		return stored, false, fmt.Errorf("invalid tool call status %q", stored.Status)
	}
	return stored, false, nil
}

func (s *AgentToolCallStore) CompleteToolCall(ctx context.Context, callID, argsHash string, result json.RawMessage) (appharness.ToolCallRecord, error) {
	if s == nil || s.database == nil {
		return appharness.ToolCallRecord{}, errors.New("agent tool call store is not configured")
	}
	if !json.Valid(result) {
		return appharness.ToolCallRecord{}, errors.New("tool call result must be valid JSON")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	update := s.database.WithContext(ctx).Model(&agentToolCallModel{}).
		Where("call_id = ? AND args_hash = ? AND status = ?", callID, argsHash, appharness.ToolCallStarted).
		Updates(map[string]any{"status": appharness.ToolCallCompleted, "result_json": string(result), "updated_at": now})
	if update.Error != nil {
		return appharness.ToolCallRecord{}, fmt.Errorf("complete agent tool call: %w", update.Error)
	}
	if update.RowsAffected == 0 {
		return appharness.ToolCallRecord{}, fmt.Errorf("%w: %s", appharness.ErrToolCallConflict, callID)
	}
	var model agentToolCallModel
	if err := s.database.WithContext(ctx).First(&model, "call_id = ?", callID).Error; err != nil {
		return appharness.ToolCallRecord{}, err
	}
	return toolCallFromModel(model), nil
}

func toolCallFromModel(model agentToolCallModel) appharness.ToolCallRecord {
	result := json.RawMessage(model.ResultJSON)
	if string(result) == "null" {
		result = nil
	}
	return appharness.ToolCallRecord{
		CallID: model.CallID, RunID: model.RunID, TurnID: model.TurnID, ToolName: model.ToolName,
		ArgsHash: model.ArgsHash, SideEffect: model.SideEffect,
		Status: appharness.ToolCallStatus(model.Status), Result: result,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

var _ appharness.ToolCallStore = (*AgentToolCallStore)(nil)

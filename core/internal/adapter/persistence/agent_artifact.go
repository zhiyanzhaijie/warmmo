package persistence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	appharness "warmmo/core/internal/application/harness"
)

type AgentArtifactStore struct {
	database *gorm.DB
}

func NewAgentArtifactStore(database *Database) *AgentArtifactStore {
	if database == nil {
		return &AgentArtifactStore{}
	}
	return &AgentArtifactStore{database: database.DB}
}

func (s *AgentArtifactStore) SaveArtifact(ctx context.Context, artifact appharness.Artifact) (appharness.Artifact, error) {
	if s == nil || s.database == nil {
		return appharness.Artifact{}, errors.New("agent artifact store is not configured")
	}
	if strings.TrimSpace(artifact.Ref.ID) == "" || strings.TrimSpace(artifact.Ref.Kind) == "" ||
		strings.TrimSpace(artifact.Ref.SchemaVersion) == "" || strings.TrimSpace(artifact.RunID) == "" ||
		strings.TrimSpace(artifact.TurnID) == "" || strings.TrimSpace(artifact.AgentID) == "" || len(artifact.Payload) == 0 {
		return appharness.Artifact{}, errors.New("artifact identity, owner and payload are required")
	}
	if !artifactPayloadValid(artifact.Payload) {
		return appharness.Artifact{}, errors.New("artifact payload must be valid JSON")
	}
	createdAt := artifact.CreatedAt.UTC().Truncate(time.Microsecond)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	model := agentArtifactModel{
		ID: artifact.Ref.ID, RunID: artifact.RunID, TurnID: artifact.TurnID, AgentID: artifact.AgentID,
		Kind: artifact.Ref.Kind, SchemaVersion: artifact.Ref.SchemaVersion,
		PayloadJSON: string(artifact.Payload), CreatedAt: createdAt,
	}
	err := s.database.WithContext(ctx).Create(&model).Error
	if err == nil {
		return artifactFromModel(model), nil
	}
	var existing agentArtifactModel
	if loadErr := s.database.WithContext(ctx).First(&existing, "id = ?", artifact.Ref.ID).Error; loadErr != nil {
		return appharness.Artifact{}, fmt.Errorf("save agent artifact: %w", err)
	}
	if existing.RunID != model.RunID || existing.TurnID != model.TurnID || existing.AgentID != model.AgentID ||
		existing.Kind != model.Kind || existing.SchemaVersion != model.SchemaVersion ||
		!bytes.Equal([]byte(existing.PayloadJSON), []byte(model.PayloadJSON)) {
		return appharness.Artifact{}, fmt.Errorf("%w: %s", appharness.ErrArtifactConflict, artifact.Ref.ID)
	}
	return artifactFromModel(existing), nil
}

func (s *AgentArtifactStore) GetArtifact(ctx context.Context, id string) (appharness.Artifact, error) {
	if s == nil || s.database == nil {
		return appharness.Artifact{}, errors.New("agent artifact store is not configured")
	}
	var model agentArtifactModel
	err := s.database.WithContext(ctx).First(&model, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appharness.Artifact{}, fmt.Errorf("%w: %s", appharness.ErrArtifactNotFound, id)
	}
	if err != nil {
		return appharness.Artifact{}, fmt.Errorf("read agent artifact: %w", err)
	}
	return artifactFromModel(model), nil
}

func (s *AgentArtifactStore) FindArtifact(ctx context.Context, runID, kind string) (appharness.Artifact, error) {
	if s == nil || s.database == nil {
		return appharness.Artifact{}, errors.New("agent artifact store is not configured")
	}
	var model agentArtifactModel
	err := s.database.WithContext(ctx).Where("run_id = ? AND kind = ?", strings.TrimSpace(runID), strings.TrimSpace(kind)).
		Order("created_at DESC").First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return appharness.Artifact{}, fmt.Errorf("%w: run %s kind %s", appharness.ErrArtifactNotFound, runID, kind)
	}
	if err != nil {
		return appharness.Artifact{}, fmt.Errorf("find agent artifact: %w", err)
	}
	return artifactFromModel(model), nil
}

func artifactFromModel(model agentArtifactModel) appharness.Artifact {
	return appharness.Artifact{
		Ref:   appharness.ArtifactRef{ID: model.ID, Kind: model.Kind, SchemaVersion: model.SchemaVersion},
		RunID: model.RunID, TurnID: model.TurnID, AgentID: model.AgentID,
		Payload: []byte(model.PayloadJSON), CreatedAt: model.CreatedAt,
	}
}

func artifactPayloadValid(payload []byte) bool {
	return json.Valid(bytes.TrimSpace(payload))
}

var _ appharness.ArtifactStore = (*AgentArtifactStore)(nil)

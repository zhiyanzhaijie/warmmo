package persistence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appagent "warmmo/core/internal/application/agent"
)

func (r *AgentRepository) PrepareProductProjection(run appagent.Run, result appagent.RunResult) (appagent.ProductProjection, error) {
	if strings.TrimSpace(result.ArtifactID) == "" {
		return appagent.ProductProjection{}, nil
	}
	now := time.Now().UTC()
	payloadHash, err := productProjectionPayloadHash(result)
	if err != nil {
		return appagent.ProductProjection{}, err
	}
	var projection agentProductProjectionModel
	err = r.database.Transaction(func(tx *gorm.DB) error {
		var storedRun agentRunModel
		if err := tx.Select("status").First(&storedRun, "id = ?", run.ID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return appagent.ErrRunNotFound
		} else if err != nil {
			return err
		}
		if appagent.RunStatus(storedRun.Status) != appagent.RunStatusRunning {
			return appagent.ErrRunNotCancellable
		}
		var artifact agentArtifactModel
		if err := tx.First(&artifact, "id = ?", result.ArtifactID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("projection artifact %q does not exist", result.ArtifactID)
		} else if err != nil {
			return err
		}
		if artifact.RunID != run.ID || artifact.Kind != result.ArtifactKind {
			return fmt.Errorf("%w: artifact owner or kind mismatch", appagent.ErrProjectionConflict)
		}
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&projection, "artifact_id = ?", result.ArtifactID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			projection = agentProductProjectionModel{
				ArtifactID: result.ArtifactID, RunID: run.ID, ArtifactKind: result.ArtifactKind,
				Target: run.Target, TargetNodeID: run.TargetNodeID, ExpectedRevision: result.ExpectedRevision,
				PayloadHash: payloadHash, Status: string(appagent.ProductProjectionPending), Attempts: 1,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&projection).Error; err != nil {
				return err
			}
			_, err = appendEvent(tx, run.ID, appagent.EventProjectionPending, map[string]any{
				"artifactId": result.ArtifactID, "artifactKind": result.ArtifactKind, "attempt": projection.Attempts,
			}, now)
			return err
		}
		if err != nil {
			return err
		}
		if projection.RunID != run.ID || projection.ArtifactKind != result.ArtifactKind || projection.Target != run.Target ||
			projection.TargetNodeID != run.TargetNodeID || projection.ExpectedRevision != result.ExpectedRevision || projection.PayloadHash != payloadHash {
			return appagent.ErrProjectionConflict
		}
		if appagent.ProductProjectionStatus(projection.Status) != appagent.ProductProjectionPending {
			return nil
		}
		projection.Attempts++
		projection.LastError = ""
		projection.UpdatedAt = now
		return tx.Save(&projection).Error
	})
	if err != nil {
		return appagent.ProductProjection{}, fmt.Errorf("prepare agent product projection: %w", err)
	}
	return productProjectionFromModel(projection), nil
}

func (r *AgentRepository) RecordProductProjectionError(runID, artifactID string, status appagent.ProductProjectionStatus, message string) error {
	if strings.TrimSpace(artifactID) == "" {
		return nil
	}
	if status != appagent.ProductProjectionPending && status != appagent.ProductProjectionConflict && status != appagent.ProductProjectionFailed {
		return fmt.Errorf("invalid product projection error status %q", status)
	}
	result := r.database.Model(&agentProductProjectionModel{}).
		Where("artifact_id = ? AND run_id = ? AND status = ?", artifactID, runID, appagent.ProductProjectionPending).
		Updates(map[string]any{"status": status, "last_error": strings.TrimSpace(message), "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return fmt.Errorf("record product projection error: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		var projection agentProductProjectionModel
		if err := r.database.First(&projection, "artifact_id = ? AND run_id = ?", artifactID, runID).Error; err != nil {
			return fmt.Errorf("read product projection after error: %w", err)
		}
		if appagent.ProductProjectionStatus(projection.Status) == appagent.ProductProjectionCompleted {
			return nil
		}
		return appagent.ErrProjectionTerminal
	}
	return nil
}

func (r *AgentRepository) RequeueProductProjection(runID, artifactID string, attempt int, retryAfter time.Duration) error {
	return r.database.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Model(&agentRunModel{}).Where("id = ? AND status = ?", runID, appagent.RunStatusRunning).
			Updates(map[string]any{"status": appagent.RunStatusQueued, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return appagent.ErrRunNotCancellable
		}
		_, err := appendEvent(tx, runID, appagent.EventProjectionRetry, map[string]any{
			"artifactId": artifactID, "attempt": attempt, "retryAfterMs": retryAfter.Milliseconds(),
		}, now)
		return err
	})
}

func productProjectionCompleted(tx *gorm.DB, runID string, result appagent.RunResult) (bool, error) {
	if strings.TrimSpace(result.ArtifactID) == "" {
		return false, nil
	}
	var projection agentProductProjectionModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&projection, "artifact_id = ? AND run_id = ?", result.ArtifactID, runID).Error; err != nil {
		return false, fmt.Errorf("load product projection: %w", err)
	}
	switch appagent.ProductProjectionStatus(projection.Status) {
	case appagent.ProductProjectionCompleted:
		return true, nil
	case appagent.ProductProjectionPending:
		return false, nil
	default:
		return false, appagent.ErrProjectionTerminal
	}
}

func completeProductProjection(tx *gorm.DB, runID string, result appagent.RunResult, now time.Time) error {
	if strings.TrimSpace(result.ArtifactID) == "" {
		return nil
	}
	completedAt := now
	updated := tx.Model(&agentProductProjectionModel{}).
		Where("artifact_id = ? AND run_id = ? AND status = ?", result.ArtifactID, runID, appagent.ProductProjectionPending).
		Updates(map[string]any{"status": appagent.ProductProjectionCompleted, "last_error": "", "completed_at": &completedAt, "updated_at": now})
	if updated.Error != nil {
		return fmt.Errorf("complete product projection: %w", updated.Error)
	}
	if updated.RowsAffected == 0 {
		var projection agentProductProjectionModel
		if err := tx.First(&projection, "artifact_id = ? AND run_id = ?", result.ArtifactID, runID).Error; err != nil {
			return fmt.Errorf("read completed product projection: %w", err)
		}
		if appagent.ProductProjectionStatus(projection.Status) != appagent.ProductProjectionCompleted {
			return appagent.ErrProjectionTerminal
		}
	}
	return nil
}

func productProjectionPayloadHash(result appagent.RunResult) (string, error) {
	payload, err := json.Marshal(struct {
		Title            string `json:"title"`
		Content          string `json:"content"`
		Message          string `json:"message"`
		Role             string `json:"role"`
		SkillID          string `json:"skillId"`
		SkillVersion     string `json:"skillVersion"`
		CandidateID      string `json:"candidateId"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}{
		Title: result.Title, Content: result.Content, Message: result.Message, Role: string(result.Role),
		SkillID: result.SkillID, SkillVersion: result.SkillVersion, CandidateID: result.CandidateID,
		ExpectedRevision: result.ExpectedRevision,
	})
	if err != nil {
		return "", fmt.Errorf("hash product projection payload: %w", err)
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func productProjectionFromModel(model agentProductProjectionModel) appagent.ProductProjection {
	return appagent.ProductProjection{
		RunID: model.RunID, ArtifactID: model.ArtifactID, Status: appagent.ProductProjectionStatus(model.Status),
		Attempts: model.Attempts, LastError: model.LastError,
	}
}

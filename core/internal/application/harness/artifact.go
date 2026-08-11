package harness

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrArtifactNotFound = errors.New("agent artifact not found")
	ErrArtifactConflict = errors.New("agent artifact conflict")
)

type ArtifactRef struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	SchemaVersion string `json:"schemaVersion"`
}

type Artifact struct {
	Ref       ArtifactRef     `json:"ref"`
	RunID     string          `json:"runId"`
	TurnID    string          `json:"turnId"`
	AgentID   string          `json:"agentId"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"createdAt"`
}

type ArtifactStore interface {
	SaveArtifact(context.Context, Artifact) (Artifact, error)
	GetArtifact(context.Context, string) (Artifact, error)
	FindArtifact(context.Context, string, string) (Artifact, error)
}

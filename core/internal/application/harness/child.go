package harness

import (
	"context"
	"errors"
	"time"
)

var ErrChildRunNotFound = errors.New("agent child run not found")

type ChildRun struct {
	ID            string         `json:"id"`
	RunID         string         `json:"runId"`
	ParentTurnID  string         `json:"parentTurnId"`
	ParentAgentID string         `json:"parentAgentId"`
	ChildTurnID   string         `json:"childTurnId"`
	ChildAgentID  string         `json:"childAgentId"`
	SessionID     string         `json:"sessionId"`
	Status        TurnStatus     `json:"status"`
	StopReason    StopReason     `json:"stopReason,omitempty"`
	Artifact      *ArtifactRef   `json:"artifact,omitempty"`
	Pending       *PendingAction `json:"pending,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type ChildRunStore interface {
	StartChildRun(context.Context, ChildRun) (ChildRun, error)
	FinishChildRun(context.Context, string, TurnOutcome) (ChildRun, error)
	GetChildRunByTurn(context.Context, string) (ChildRun, error)
}

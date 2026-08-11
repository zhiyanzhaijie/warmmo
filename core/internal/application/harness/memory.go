package harness

import (
	"context"
	"errors"
	"time"
)

var ErrMemoryNotFound = errors.New("agent memory not found")

type MemoryRecord struct {
	ID               string    `json:"id"`
	WorkID           string    `json:"workId"`
	Kind             string    `json:"kind"`
	Content          string    `json:"content"`
	SourceRunID      string    `json:"sourceRunId,omitempty"`
	SourceArtifactID string    `json:"sourceArtifactId,omitempty"`
	ContentHash      string    `json:"contentHash"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type MemoryRecallQuery struct {
	WorkID string
	Query  string
	Limit  int
}

type MemoryStore interface {
	Remember(context.Context, MemoryRecord) (MemoryRecord, error)
	Load(context.Context, string, []string) ([]MemoryRecord, error)
	Recall(context.Context, MemoryRecallQuery) ([]MemoryRecord, error)
}

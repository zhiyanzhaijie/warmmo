package harness

import (
	"errors"
	"time"
)

var ErrContextBudgetExceeded = errors.New("agent context budget exceeded")

type CompactionManifest struct {
	Version               string       `json:"version"`
	Estimator             string       `json:"estimator"`
	EstimatedTokensBefore int          `json:"estimatedTokensBefore"`
	EstimatedTokensAfter  int          `json:"estimatedTokensAfter"`
	AvailableTokens       int          `json:"availableTokens"`
	RemovedContents       int          `json:"removedContents"`
	RemovedContentHashes  []string     `json:"removedContentHashes,omitempty"`
	SummaryArtifact       *ArtifactRef `json:"summaryArtifact,omitempty"`
	MemoryRecallPerformed bool         `json:"memoryRecallPerformed,omitempty"`
	RecalledMemoryIDs     []string     `json:"recalledMemoryIds,omitempty"`
	CreatedAt             time.Time    `json:"createdAt"`
}

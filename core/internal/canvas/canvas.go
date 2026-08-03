package canvas

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"

	"warmnote/core/internal/agent"
)

var (
	ErrNodeNotFound = errors.New("canvas node not found")
	ErrInvalidNode  = errors.New("invalid canvas node")
)

type Node struct {
	ID        string    `json:"id"`
	WorkID    string    `json:"workId"`
	Revision  int64     `json:"revision"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CreateNodeInput struct {
	WorkID  string
	Kind    string
	Title   string
	Content string
	X       float64
	Y       float64
}

type Store interface {
	CreateNode(context.Context, CreateNodeInput) (Node, error)
	ListNodes(context.Context, string) ([]Node, error)
	GetNodes(context.Context, string, []string) ([]Node, error)
	UpdateNodePosition(context.Context, string, string, float64, float64) error
	CreateCandidate(context.Context, agent.Candidate) (agent.Candidate, error)
	ListCandidates(context.Context, string) ([]agent.Candidate, error)
}

type ContextReader struct {
	store Store
}

func NewContextReader(store Store) *ContextReader {
	return &ContextReader{store: store}
}

func (r *ContextReader) BuildSnapshot(ctx context.Context, workID string, nodeIDs []string) (agent.ContextSnapshot, error) {
	nodes, err := r.store.GetNodes(ctx, workID, nodeIDs)
	if err != nil {
		return agent.ContextSnapshot{}, err
	}
	snapshotNodes := make([]agent.NodeSnapshot, 0, len(nodes))
	for _, node := range nodes {
		snapshotNodes = append(snapshotNodes, agent.NodeSnapshot{
			ID: node.ID, Revision: formatRevision(node.Revision), Type: node.Kind,
			Title: node.Title, Content: node.Content,
		})
	}
	return agent.ContextSnapshot{ID: uuid.NewString(), WorkID: workID, Nodes: snapshotNodes}, nil
}

func formatRevision(revision int64) string {
	return strconv.FormatInt(revision, 10)
}

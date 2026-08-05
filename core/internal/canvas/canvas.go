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
	ErrNodeNotFound       = errors.New("canvas node not found")
	ErrInvalidNode        = errors.New("invalid canvas node")
	ErrRevisionConflict   = errors.New("canvas node revision conflict")
	ErrHistoryUnavailable = errors.New("canvas history action unavailable")
	ErrCandidateNotFound  = errors.New("canvas candidate not found")
	ErrCandidateResolved  = errors.New("canvas candidate is already resolved")
)

const (
	NodeKindCharacter      = "character"
	NodeKindItem           = "item"
	NodeKindLocation       = "location"
	NodeKindTime           = "time"
	NodeKindWorld          = "world"
	NodeKindMechanism      = "mechanism"
	NodeKindEvent          = "event"
	NodeKindChapterOutline = "chapter-outline"
	NodeKindSectionDraft   = "section-draft"
	NodeKindManuscript     = "manuscript"
)

func IsValidNodeKind(kind string) bool {
	switch kind {
	case NodeKindCharacter, NodeKindItem, NodeKindLocation, NodeKindTime, NodeKindWorld,
		NodeKindMechanism, NodeKindEvent, NodeKindChapterOutline, NodeKindSectionDraft, NodeKindManuscript:
		return true
	default:
		return false
	}
}

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
	WorkID         string
	Kind           string
	Title          string
	Content        string
	X              float64
	Y              float64
	ContextNodeIDs []string
}

type UpdateNodeInput struct {
	WorkID           string
	NodeID           string
	Title            string
	Content          string
	ExpectedRevision int64
}

type NodePosition struct {
	NodeID string  `json:"nodeId"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

type HistoryState struct {
	CanUndo   bool   `json:"canUndo"`
	CanRedo   bool   `json:"canRedo"`
	UndoLabel string `json:"undoLabel"`
	RedoLabel string `json:"redoLabel"`
}
type Edge struct {
	ID           string    `json:"id"`
	WorkID       string    `json:"workId"`
	SourceNodeID string    `json:"sourceNodeId"`
	TargetNodeID string    `json:"targetNodeId"`
	Kind         string    `json:"kind"`
	CreatedAt    time.Time `json:"createdAt"`
}

type CreateEdgeInput struct {
	WorkID       string
	SourceNodeID string
	TargetNodeID string
}

type AcceptCandidateInput struct {
	WorkID      string
	CandidateID string
	Title       string
}

type Store interface {
	CreateNode(context.Context, CreateNodeInput) (Node, error)
	ListNodes(context.Context, string) ([]Node, error)
	GetNode(context.Context, string, string) (Node, error)
	GetNodes(context.Context, string, []string) ([]Node, error)
	UpdateNode(context.Context, UpdateNodeInput) (Node, error)
	UpdateNodePosition(context.Context, string, string, float64, float64) error
	UpdateNodePositions(context.Context, string, []NodePosition) error
	DeleteNodes(context.Context, string, []string) error
	GetHistoryState(context.Context, string) (HistoryState, error)
	Undo(context.Context, string) (HistoryState, error)
	Redo(context.Context, string) (HistoryState, error)
	ListEdges(context.Context, string) ([]Edge, error)
	CreateEdge(context.Context, CreateEdgeInput) (Edge, error)
	DeleteEdges(context.Context, string, []string) error
	CreateCandidate(context.Context, agent.Candidate) (agent.Candidate, error)
	ListCandidates(context.Context, string) ([]agent.Candidate, error)
	UpdateCandidatePosition(context.Context, string, string, float64, float64) error
	AcceptCandidate(context.Context, AcceptCandidateInput) (Node, error)
	RejectCandidate(context.Context, string, string) error
}

type NodeReader interface {
	GetNodes(context.Context, string, []string) ([]Node, error)
}

type CandidateCreator interface {
	CreateCandidate(context.Context, agent.Candidate) (agent.Candidate, error)
}

type ContextReader struct {
	store NodeReader
}

func NewContextReader(store NodeReader) *ContextReader {
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

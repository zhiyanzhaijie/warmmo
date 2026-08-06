package canvas

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"warmnote/core/internal/agent"
)

var (
	ErrNodeNotFound          = errors.New("canvas node not found")
	ErrInvalidNode           = errors.New("invalid canvas node")
	ErrRevisionConflict      = errors.New("canvas node revision conflict")
	ErrHistoryUnavailable    = errors.New("canvas history action unavailable")
	ErrCandidateNotFound     = errors.New("canvas candidate not found")
	ErrCandidateResolved     = errors.New("canvas candidate is already resolved")
	ErrDerivationExists      = errors.New("canvas node already has derived children")
	ErrInvalidChapterArchive = errors.New("invalid chapter archive")
)

type NodeKind string

const (
	NodeKindCharacter      NodeKind = "character"
	NodeKindItem           NodeKind = "item"
	NodeKindLocation       NodeKind = "location"
	NodeKindTime           NodeKind = "time"
	NodeKindWorld          NodeKind = "world"
	NodeKindMechanism      NodeKind = "mechanism"
	NodeKindEvent          NodeKind = "event"
	NodeKindChapterOutline NodeKind = "chapter-outline"
	NodeKindSectionOutline NodeKind = "section-outline"
	NodeKindChapterSection NodeKind = "chapter-section"
	NodeKindManuscript     NodeKind = "manuscript"
)

func ParseNodeKind(value string) (NodeKind, bool) {
	kind := NodeKind(strings.TrimSpace(value))
	return kind, IsValidNodeKind(kind)
}

func IsValidNodeKind(kind NodeKind) bool {
	switch kind {
	case NodeKindCharacter, NodeKindItem, NodeKindLocation, NodeKindTime, NodeKindWorld,
		NodeKindMechanism, NodeKindEvent, NodeKindChapterOutline, NodeKindSectionOutline,
		NodeKindChapterSection, NodeKindManuscript:
		return true
	default:
		return false
	}
}

func IsManuallyCreatableNodeKind(kind NodeKind) bool {
	switch kind {
	case NodeKindCharacter, NodeKindItem, NodeKindLocation, NodeKindTime, NodeKindWorld,
		NodeKindMechanism, NodeKindEvent, NodeKindChapterOutline:
		return true
	default:
		return false
	}
}

func IsDerivedNodeKind(kind NodeKind) bool {
	return IsValidNodeKind(kind) && !IsManuallyCreatableNodeKind(kind)
}

type Node struct {
	ID               string    `json:"id"`
	WorkID           string    `json:"workId"`
	Revision         int64     `json:"revision"`
	Kind             NodeKind  `json:"kind"`
	Title            string    `json:"title"`
	Content          string    `json:"content"`
	X                float64   `json:"x"`
	Y                float64   `json:"y"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	CurrentVersionID string    `json:"currentVersionId,omitempty"`
}

type NodeVersion struct {
	ID              string    `json:"id"`
	NodeID          string    `json:"nodeId"`
	WorkID          string    `json:"workId"`
	VersionNumber   int64     `json:"versionNumber"`
	ParentVersionID string    `json:"parentVersionId,omitempty"`
	Title           string    `json:"title"`
	Content         string    `json:"content"`
	SourceRunID     string    `json:"sourceRunId,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

type ChapterArchive struct {
	ID                   string                  `json:"id"`
	WorkID               string                  `json:"workId"`
	ChapterOutlineNodeID string                  `json:"chapterOutlineNodeId"`
	Revision             int64                   `json:"revision"`
	RunID                string                  `json:"runId"`
	OutlineVersionID     string                  `json:"outlineVersionId,omitempty"`
	OutlineRevision      int64                   `json:"outlineRevision"`
	OutlineTitle         string                  `json:"outlineTitle"`
	OutlineContent       string                  `json:"outlineContent"`
	Summary              string                  `json:"summary"`
	SourceDigest         string                  `json:"sourceDigest"`
	IsCurrent            bool                    `json:"isCurrent"`
	ProjectionStatus     string                  `json:"projectionStatus"`
	Sections             []ChapterArchiveSection `json:"sections"`
	CreatedAt            time.Time               `json:"createdAt"`
	SupersededAt         *time.Time              `json:"supersededAt,omitempty"`
}

type ChapterArchiveSection struct {
	ArchiveID               string `json:"archiveId"`
	Ordinal                 int    `json:"ordinal"`
	SectionOutlineNodeID    string `json:"sectionOutlineNodeId"`
	ChapterSectionNodeID    string `json:"chapterSectionNodeId"`
	ChapterSectionVersionID string `json:"chapterSectionVersionId,omitempty"`
	NodeRevision            int64  `json:"nodeRevision"`
	Title                   string `json:"title"`
	Summary                 string `json:"summary"`
	Content                 string `json:"content"`
	ContentHash             string `json:"contentHash"`
}

type CreateNodeInput struct {
	WorkID         string
	Kind           NodeKind
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
	ListNodeVersions(context.Context, string, string) ([]NodeVersion, error)
	SwitchNodeVersion(context.Context, string, string, string) (Node, error)
	ListCurrentChapterArchives(context.Context, string) ([]ChapterArchive, error)
	ListChapterArchiveHistory(context.Context, string, string) ([]ChapterArchive, error)
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
			ID: node.ID, Revision: formatRevision(node.Revision), Type: string(node.Kind),
			Title: node.Title, Content: node.Content,
		})
	}
	return agent.ContextSnapshot{ID: uuid.NewString(), WorkID: workID, Nodes: snapshotNodes}, nil
}

func formatRevision(revision int64) string {
	return strconv.FormatInt(revision, 10)
}

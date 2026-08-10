package application

import (
	"context"
	"errors"
	"strings"

	agent "warmmo/core/internal/application/agent"
	"warmmo/core/internal/application/pagination"
	"warmmo/core/internal/domain/canvas"
)

var ErrInvalidCanvasRequest = errors.New("invalid canvas request")

// CanvasStore is the persistence port required by canvas use cases.
type CanvasStore interface {
	CreateNode(context.Context, canvas.CreateNodeInput) (canvas.Node, error)
	ListNodes(context.Context, string) ([]canvas.Node, error)
	GetNode(context.Context, string, string) (canvas.Node, error)
	GetNodes(context.Context, string, []string) ([]canvas.Node, error)
	UpdateNode(context.Context, canvas.UpdateNodeInput) (canvas.Node, error)
	UpdateNodePosition(context.Context, string, string, float64, float64) error
	UpdateNodePositions(context.Context, string, []canvas.NodePosition) error
	LayoutChapter(context.Context, string, string) ([]canvas.NodePosition, error)
	DeleteNodes(context.Context, string, []string) error
	GetHistoryState(context.Context, string) (canvas.HistoryState, error)
	Undo(context.Context, string) (canvas.HistoryState, error)
	Redo(context.Context, string) (canvas.HistoryState, error)
	ListEdges(context.Context, string) ([]canvas.Edge, error)
	CreateEdge(context.Context, canvas.CreateEdgeInput) (canvas.Edge, error)
	DeleteEdges(context.Context, string, []string) error
	CreateCandidate(context.Context, canvas.Candidate) (canvas.Candidate, error)
	ListCandidates(context.Context, string) ([]canvas.Candidate, error)
	UpdateCandidatePosition(context.Context, string, string, float64, float64) error
	AcceptCandidate(context.Context, canvas.AcceptCandidateInput) (canvas.Node, error)
	RejectCandidate(context.Context, string, string) error
	ListNodeVersions(context.Context, string, string) ([]canvas.NodeVersion, error)
	SwitchNodeVersion(context.Context, string, string, string) (canvas.Node, error)
	ListCurrentChapterArchives(context.Context, string) ([]canvas.ChapterArchive, error)
	ListCurrentChapterArchivesPage(context.Context, string, pagination.Pageable) (pagination.Page[canvas.ChapterArchive], error)
	ListChapterArchiveVisibility(context.Context, string) ([]canvas.ChapterArchiveVisibility, error)
	ListChapterArchiveTimelinePage(context.Context, string, pagination.Pageable) (pagination.Page[canvas.ChapterArchiveTimeline], error)
	ListChapterArchiveHistory(context.Context, string, string) ([]canvas.ChapterArchive, error)
	RetractChapterArchive(context.Context, string, string) error
}

type CanvasService struct {
	store                    CanvasStore
	candidateDecisionHandler func(context.Context, string, string, bool, string) error
}

func NewCanvasService(store CanvasStore) *CanvasService {
	return &CanvasService{store: store}
}

func (s *CanvasService) SetCandidateDecisionHandler(handler func(context.Context, string, string, bool, string) error) {
	s.candidateDecisionHandler = handler
}

func (s *CanvasService) CreateNode(ctx context.Context, input canvas.CreateNodeInput) (canvas.Node, error) {
	input.WorkID = strings.TrimSpace(input.WorkID)
	input.Kind = canvas.NodeKind(strings.TrimSpace(string(input.Kind)))
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	if input.WorkID == "" || input.Title == "" || !canvas.IsManuallyCreatableNodeKind(input.Kind) {
		return canvas.Node{}, ErrInvalidCanvasRequest
	}
	if len(input.ContextNodeIDs) > 100 {
		return canvas.Node{}, ErrInvalidCanvasRequest
	}
	for index := range input.ContextNodeIDs {
		input.ContextNodeIDs[index] = strings.TrimSpace(input.ContextNodeIDs[index])
		if input.ContextNodeIDs[index] == "" {
			return canvas.Node{}, ErrInvalidCanvasRequest
		}
	}
	return s.store.CreateNode(ctx, input)
}

func (s *CanvasService) ListNodes(ctx context.Context, workID string) ([]canvas.Node, error) {
	return s.store.ListNodes(ctx, strings.TrimSpace(workID))
}

func (s *CanvasService) GetNode(ctx context.Context, workID, nodeID string) (canvas.Node, error) {
	workID = strings.TrimSpace(workID)
	nodeID = strings.TrimSpace(nodeID)
	if workID == "" || nodeID == "" {
		return canvas.Node{}, ErrInvalidCanvasRequest
	}
	return s.store.GetNode(ctx, workID, nodeID)
}

func (s *CanvasService) ListNodeVersions(ctx context.Context, workID, nodeID string) ([]canvas.NodeVersion, error) {
	workID = strings.TrimSpace(workID)
	nodeID = strings.TrimSpace(nodeID)
	if workID == "" || nodeID == "" {
		return nil, ErrInvalidCanvasRequest
	}
	return s.store.ListNodeVersions(ctx, workID, nodeID)
}

func (s *CanvasService) ListCurrentChapterArchives(ctx context.Context, workID string) ([]canvas.ChapterArchive, error) {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return nil, ErrInvalidCanvasRequest
	}
	return s.store.ListCurrentChapterArchives(ctx, workID)
}

func (s *CanvasService) ListChapterArchiveVisibility(ctx context.Context, workID string) ([]canvas.ChapterArchiveVisibility, error) {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return nil, ErrInvalidCanvasRequest
	}
	return s.store.ListChapterArchiveVisibility(ctx, workID)
}

func (s *CanvasService) ListCurrentChapterArchivesPage(
	ctx context.Context,
	workID string,
	pageable pagination.Pageable,
) (pagination.Page[canvas.ChapterArchive], error) {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return pagination.Page[canvas.ChapterArchive]{}, ErrInvalidCanvasRequest
	}
	if err := pagination.Validate(pageable); err != nil {
		return pagination.Page[canvas.ChapterArchive]{}, ErrInvalidCanvasRequest
	}
	return s.store.ListCurrentChapterArchivesPage(ctx, workID, pageable)
}

func (s *CanvasService) ListChapterArchiveTimelinePage(ctx context.Context, workID string, pageable pagination.Pageable) (pagination.Page[canvas.ChapterArchiveTimeline], error) {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return pagination.Page[canvas.ChapterArchiveTimeline]{}, ErrInvalidCanvasRequest
	}
	if err := pagination.Validate(pageable); err != nil {
		return pagination.Page[canvas.ChapterArchiveTimeline]{}, ErrInvalidCanvasRequest
	}
	return s.store.ListChapterArchiveTimelinePage(ctx, workID, pageable)
}

func (s *CanvasService) ListChapterArchiveHistory(ctx context.Context, workID, chapterOutlineNodeID string) ([]canvas.ChapterArchive, error) {
	workID = strings.TrimSpace(workID)
	chapterOutlineNodeID = strings.TrimSpace(chapterOutlineNodeID)
	if workID == "" || chapterOutlineNodeID == "" {
		return nil, ErrInvalidCanvasRequest
	}
	return s.store.ListChapterArchiveHistory(ctx, workID, chapterOutlineNodeID)
}

func (s *CanvasService) RetractChapterArchive(ctx context.Context, workID, archiveID string) error {
	workID = strings.TrimSpace(workID)
	archiveID = strings.TrimSpace(archiveID)
	if workID == "" || archiveID == "" {
		return ErrInvalidCanvasRequest
	}
	return s.store.RetractChapterArchive(ctx, workID, archiveID)
}

func (s *CanvasService) SwitchNodeVersion(ctx context.Context, workID, nodeID, versionID string) (canvas.Node, error) {
	workID = strings.TrimSpace(workID)
	nodeID = strings.TrimSpace(nodeID)
	versionID = strings.TrimSpace(versionID)
	if workID == "" || nodeID == "" || versionID == "" {
		return canvas.Node{}, ErrInvalidCanvasRequest
	}
	return s.store.SwitchNodeVersion(ctx, workID, nodeID, versionID)
}

func (s *CanvasService) GetNodes(ctx context.Context, workID string, nodeIDs []string) ([]canvas.Node, error) {
	if len(nodeIDs) == 0 {
		return nil, ErrInvalidCanvasRequest
	}
	return s.store.GetNodes(ctx, strings.TrimSpace(workID), nodeIDs)
}

func (s *CanvasService) UpdateNode(ctx context.Context, input canvas.UpdateNodeInput) (canvas.Node, error) {
	input.WorkID = strings.TrimSpace(input.WorkID)
	input.NodeID = strings.TrimSpace(input.NodeID)
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	if input.WorkID == "" || input.NodeID == "" || input.Title == "" || input.ExpectedRevision < 1 {
		return canvas.Node{}, ErrInvalidCanvasRequest
	}
	return s.store.UpdateNode(ctx, input)
}

func (s *CanvasService) UpdateNodePosition(ctx context.Context, workID, nodeID string, x, y float64) error {
	workID = strings.TrimSpace(workID)
	nodeID = strings.TrimSpace(nodeID)
	if workID == "" || nodeID == "" {
		return ErrInvalidCanvasRequest
	}
	return s.store.UpdateNodePosition(ctx, workID, nodeID, x, y)
}

func (s *CanvasService) UpdateNodePositions(
	ctx context.Context,
	workID string,
	positions []canvas.NodePosition,
) error {
	workID = strings.TrimSpace(workID)
	if workID == "" || len(positions) == 0 || len(positions) > 100 {
		return ErrInvalidCanvasRequest
	}
	for index := range positions {
		positions[index].NodeID = strings.TrimSpace(positions[index].NodeID)
		if positions[index].NodeID == "" {
			return ErrInvalidCanvasRequest
		}
	}
	return s.store.UpdateNodePositions(ctx, workID, positions)
}

func (s *CanvasService) LayoutChapter(ctx context.Context, workID, chapterOutlineNodeID string) ([]canvas.NodePosition, error) {
	workID = strings.TrimSpace(workID)
	chapterOutlineNodeID = strings.TrimSpace(chapterOutlineNodeID)
	if workID == "" || chapterOutlineNodeID == "" {
		return nil, ErrInvalidCanvasRequest
	}
	return s.store.LayoutChapter(ctx, workID, chapterOutlineNodeID)
}

func (s *CanvasService) DeleteNodes(ctx context.Context, workID string, nodeIDs []string) error {
	workID = strings.TrimSpace(workID)
	if workID == "" || len(nodeIDs) == 0 || len(nodeIDs) > 100 {
		return ErrInvalidCanvasRequest
	}
	for index := range nodeIDs {
		nodeIDs[index] = strings.TrimSpace(nodeIDs[index])
		if nodeIDs[index] == "" {
			return ErrInvalidCanvasRequest
		}
	}
	return s.store.DeleteNodes(ctx, workID, nodeIDs)
}

func (s *CanvasService) GetHistoryState(ctx context.Context, workID string) (canvas.HistoryState, error) {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return canvas.HistoryState{}, ErrInvalidCanvasRequest
	}
	return s.store.GetHistoryState(ctx, workID)
}

func (s *CanvasService) Undo(ctx context.Context, workID string) (canvas.HistoryState, error) {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return canvas.HistoryState{}, ErrInvalidCanvasRequest
	}
	return s.store.Undo(ctx, workID)
}

func (s *CanvasService) Redo(ctx context.Context, workID string) (canvas.HistoryState, error) {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return canvas.HistoryState{}, ErrInvalidCanvasRequest
	}
	return s.store.Redo(ctx, workID)
}

func (s *CanvasService) ListEdges(ctx context.Context, workID string) ([]canvas.Edge, error) {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return nil, ErrInvalidCanvasRequest
	}
	return s.store.ListEdges(ctx, workID)
}

func (s *CanvasService) CreateEdge(ctx context.Context, input canvas.CreateEdgeInput) (canvas.Edge, error) {
	input.WorkID = strings.TrimSpace(input.WorkID)
	input.SourceNodeID = strings.TrimSpace(input.SourceNodeID)
	input.TargetNodeID = strings.TrimSpace(input.TargetNodeID)
	if input.WorkID == "" || input.SourceNodeID == "" || input.TargetNodeID == "" || input.SourceNodeID == input.TargetNodeID {
		return canvas.Edge{}, ErrInvalidCanvasRequest
	}
	return s.store.CreateEdge(ctx, input)
}

func (s *CanvasService) DeleteEdges(ctx context.Context, workID string, edgeIDs []string) error {
	workID = strings.TrimSpace(workID)
	if workID == "" || len(edgeIDs) == 0 || len(edgeIDs) > 100 {
		return ErrInvalidCanvasRequest
	}
	for index := range edgeIDs {
		edgeIDs[index] = strings.TrimSpace(edgeIDs[index])
		if edgeIDs[index] == "" {
			return ErrInvalidCanvasRequest
		}
	}
	return s.store.DeleteEdges(ctx, workID, edgeIDs)
}

func (s *CanvasService) ListCandidates(ctx context.Context, workID string) ([]agent.Candidate, error) {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return nil, ErrInvalidCanvasRequest
	}
	return s.store.ListCandidates(ctx, workID)
}

func (s *CanvasService) UpdateCandidatePosition(
	ctx context.Context,
	workID string,
	candidateID string,
	x float64,
	y float64,
) error {
	workID = strings.TrimSpace(workID)
	candidateID = strings.TrimSpace(candidateID)
	if workID == "" || candidateID == "" {
		return ErrInvalidCanvasRequest
	}
	return s.store.UpdateCandidatePosition(ctx, workID, candidateID, x, y)
}

func (s *CanvasService) AcceptCandidate(ctx context.Context, input canvas.AcceptCandidateInput) (canvas.Node, error) {
	input.WorkID = strings.TrimSpace(input.WorkID)
	input.CandidateID = strings.TrimSpace(input.CandidateID)
	input.Title = strings.TrimSpace(input.Title)
	if input.WorkID == "" || input.CandidateID == "" {
		return canvas.Node{}, ErrInvalidCanvasRequest
	}
	node, err := s.store.AcceptCandidate(ctx, input)
	if err != nil {
		return canvas.Node{}, err
	}
	if s.candidateDecisionHandler != nil {
		if err := s.candidateDecisionHandler(ctx, input.WorkID, input.CandidateID, true, node.ID); err != nil {
			return canvas.Node{}, err
		}
	}
	return node, nil
}

func (s *CanvasService) RejectCandidate(ctx context.Context, workID, candidateID string) error {
	workID = strings.TrimSpace(workID)
	candidateID = strings.TrimSpace(candidateID)
	if workID == "" || candidateID == "" {
		return ErrInvalidCanvasRequest
	}
	if err := s.store.RejectCandidate(ctx, workID, candidateID); err != nil {
		return err
	}
	if s.candidateDecisionHandler != nil {
		return s.candidateDecisionHandler(ctx, workID, candidateID, false, "")
	}
	return nil
}

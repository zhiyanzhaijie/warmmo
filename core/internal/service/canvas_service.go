package service

import (
	"context"
	"errors"
	"strings"

	"warmnote/core/internal/agent"
	"warmnote/core/internal/canvas"
)

var ErrInvalidCanvasRequest = errors.New("invalid canvas request")

type CanvasService struct {
	store canvas.Store
}

func NewCanvasService(store canvas.Store) *CanvasService {
	return &CanvasService{store: store}
}

func (s *CanvasService) CreateNode(ctx context.Context, input canvas.CreateNodeInput) (canvas.Node, error) {
	input.WorkID = strings.TrimSpace(input.WorkID)
	input.Kind = strings.TrimSpace(input.Kind)
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	if input.WorkID == "" || input.Title == "" || input.Content == "" || !validNodeKind(input.Kind) {
		return canvas.Node{}, ErrInvalidCanvasRequest
	}
	return s.store.CreateNode(ctx, input)
}

func (s *CanvasService) ListNodes(ctx context.Context, workID string) ([]canvas.Node, error) {
	return s.store.ListNodes(ctx, strings.TrimSpace(workID))
}

func (s *CanvasService) GetNodes(ctx context.Context, workID string, nodeIDs []string) ([]canvas.Node, error) {
	if len(nodeIDs) == 0 {
		return nil, ErrInvalidCanvasRequest
	}
	return s.store.GetNodes(ctx, strings.TrimSpace(workID), nodeIDs)
}

func (s *CanvasService) ListCandidates(ctx context.Context, workID string) ([]agent.Candidate, error) {
	return s.store.ListCandidates(ctx, strings.TrimSpace(workID))
}

func validNodeKind(kind string) bool {
	switch kind {
	case "chapter", "character", "plot", "world", "note", "timeline":
		return true
	default:
		return false
	}
}

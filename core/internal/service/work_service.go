package service

import (
	"context"
	"strings"

	"warmnote/core/internal/work"
)

const defaultWorkTitle = "未命名作品"

type WorkService struct {
	store work.Store
}

func NewWorkService(store work.Store) *WorkService {
	return &WorkService{store: store}
}

func (s *WorkService) Create(ctx context.Context, input work.CreateInput) (work.Summary, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.FolderID = strings.TrimSpace(input.FolderID)
	title := input.Title
	if title == "" {
		title = defaultWorkTitle
	}
	if len([]rune(title)) > 120 || len([]rune(input.Description)) > 500 {
		return work.Summary{}, work.ErrInvalidWork
	}
	input.Title = title
	return s.store.Create(ctx, input)
}

func (s *WorkService) Get(ctx context.Context, workID string) (work.Detail, error) {
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return work.Detail{}, work.ErrInvalidWork
	}
	return s.store.Get(ctx, workID)
}

func (s *WorkService) List(ctx context.Context) ([]work.Summary, error) {
	return s.store.List(ctx)
}

func (s *WorkService) Update(ctx context.Context, input work.UpdateInput) (work.Detail, error) {
	input.ID = strings.TrimSpace(input.ID)
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.FolderID = strings.TrimSpace(input.FolderID)
	input.Status = strings.TrimSpace(input.Status)
	if input.ID == "" || input.Title == "" || len([]rune(input.Title)) > 120 ||
		len([]rune(input.Description)) > 500 || input.ExpectedRevision < 1 ||
		(input.Status != "active" && input.Status != "archived") {
		return work.Detail{}, work.ErrInvalidWork
	}
	return s.store.Update(ctx, input)
}

func (s *WorkService) CreateFolder(ctx context.Context, name string) (work.Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 60 {
		return work.Folder{}, work.ErrInvalidWork
	}
	return s.store.CreateFolder(ctx, name)
}

func (s *WorkService) ListFolders(ctx context.Context) ([]work.Folder, error) {
	return s.store.ListFolders(ctx)
}

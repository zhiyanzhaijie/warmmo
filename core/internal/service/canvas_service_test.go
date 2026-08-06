package service

import (
	"context"
	"errors"
	"testing"

	"warmnote/core/internal/canvas"
	"warmnote/core/internal/repository"
)

func TestCanvasServiceRejectsManualCreationOfDerivedNodes(t *testing.T) {
	t.Parallel()

	providerRepository, err := repository.NewProviderRepository(t.TempDir())
	if err != nil {
		t.Fatalf("create provider repository: %v", err)
	}
	t.Cleanup(func() {
		if err := providerRepository.Close(); err != nil {
			t.Errorf("close provider repository: %v", err)
		}
	})
	service := NewCanvasService(repository.NewCanvasRepository(providerRepository))

	for _, kind := range []canvas.NodeKind{canvas.NodeKindSectionOutline, canvas.NodeKindChapterSection, canvas.NodeKindManuscript} {
		_, err := service.CreateNode(context.Background(), canvas.CreateNodeInput{
			WorkID: "work-1", Kind: kind, Title: "不应创建",
		})
		if !errors.Is(err, ErrInvalidCanvasRequest) {
			t.Fatalf("CreateNode(%q) error = %v", kind, err)
		}
	}

	if _, err := service.CreateNode(context.Background(), canvas.CreateNodeInput{
		WorkID: "work-1", Kind: canvas.NodeKindChapterOutline, Title: "第一章",
	}); err != nil {
		t.Fatalf("create manual chapter outline: %v", err)
	}
}

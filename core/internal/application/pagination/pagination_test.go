package pagination

import (
	"errors"
	"testing"
)

func TestNewPageBuildsMetadata(t *testing.T) {
	request, err := New(2, 20)
	if err != nil {
		t.Fatalf("create pagination request: %v", err)
	}
	page, err := NewPage([]string{"item"}, 41, request)
	if err != nil {
		t.Fatalf("create page: %v", err)
	}
	if page.Pagination.TotalPages != 3 || !page.Pagination.HasPrevious || !page.Pagination.HasNext {
		t.Fatalf("unexpected pagination metadata: %+v", page.Pagination)
	}
}

func TestNewRejectsInvalidPageSize(t *testing.T) {
	_, err := New(1, MaxPageSize+1)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}

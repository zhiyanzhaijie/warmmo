package httpapi

import (
	"errors"
	"net/http/httptest"
	"testing"

	"warmmo/core/internal/application/pagination"
)

func TestParsePaginationUsesDefaults(t *testing.T) {
	request := httptest.NewRequest("GET", "/items", nil)
	pageable, err := parsePagination(request)
	if err != nil {
		t.Fatalf("parse pagination: %v", err)
	}
	if pageable.PageNumber() != pagination.DefaultPage || pageable.PageSize() != pagination.DefaultPageSize {
		t.Fatalf("pagination = (%d, %d)", pageable.PageNumber(), pageable.PageSize())
	}
}

func TestParsePaginationReadsQueryValues(t *testing.T) {
	request := httptest.NewRequest("GET", "/items?page=3&pageSize=40", nil)
	pageable, err := parsePagination(request)
	if err != nil {
		t.Fatalf("parse pagination: %v", err)
	}
	if pageable.PageNumber() != 3 || pageable.PageSize() != 40 {
		t.Fatalf("pagination = (%d, %d)", pageable.PageNumber(), pageable.PageSize())
	}
}

func TestParsePaginationRejectsInvalidValues(t *testing.T) {
	request := httptest.NewRequest("GET", "/items?pageSize=101", nil)
	_, err := parsePagination(request)
	if !errors.Is(err, pagination.ErrInvalid) {
		t.Fatalf("error = %v, want pagination.ErrInvalid", err)
	}
}

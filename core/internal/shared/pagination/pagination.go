package pagination

import (
	"errors"
	"fmt"
)

const (
	DefaultPage     = 1
	DefaultPageSize = 20
	MaxPageSize     = 100
)

var ErrInvalid = errors.New("invalid pagination")

// Pageable is the shared contract accepted by paginated services and stores.
// Implementations may be request DTOs or transport adapters.
type Pageable interface {
	PageNumber() int
	PageSize() int
}

type Request struct {
	page     int
	pageSize int
}

func New(page, pageSize int) (Request, error) {
	request := Request{page: page, pageSize: pageSize}
	if err := Validate(request); err != nil {
		return Request{}, err
	}
	return request, nil
}

func DefaultRequest() Request {
	return Request{page: DefaultPage, pageSize: DefaultPageSize}
}

func (r Request) PageNumber() int {
	return r.page
}

func (r Request) PageSize() int {
	return r.pageSize
}

type Window struct {
	Limit  int
	Offset int
}

func WindowFor(pageable Pageable) (Window, error) {
	if err := Validate(pageable); err != nil {
		return Window{}, err
	}
	pageSize := pageable.PageSize()
	page := pageable.PageNumber()
	maxInt := int(^uint(0) >> 1)
	if page-1 > maxInt/pageSize {
		return Window{}, fmt.Errorf("%w: page offset overflows int", ErrInvalid)
	}
	return Window{
		Limit:  pageSize,
		Offset: (page - 1) * pageSize,
	}, nil
}

func Validate(pageable Pageable) error {
	if pageable == nil {
		return fmt.Errorf("%w: request is nil", ErrInvalid)
	}
	if pageable.PageNumber() < 1 {
		return fmt.Errorf("%w: page must be at least 1", ErrInvalid)
	}
	if pageable.PageSize() < 1 || pageable.PageSize() > MaxPageSize {
		return fmt.Errorf("%w: pageSize must be between 1 and %d", ErrInvalid, MaxPageSize)
	}
	return nil
}

type Metadata struct {
	Page        int   `json:"page"`
	PageSize    int   `json:"pageSize"`
	Total       int64 `json:"total"`
	TotalPages  int   `json:"totalPages"`
	HasPrevious bool  `json:"hasPrevious"`
	HasNext     bool  `json:"hasNext"`
}

type Page[T any] struct {
	Items      []T      `json:"items"`
	Pagination Metadata `json:"pagination"`
}

func NewPage[T any](items []T, total int64, pageable Pageable) (Page[T], error) {
	if total < 0 {
		return Page[T]{}, fmt.Errorf("%w: total cannot be negative", ErrInvalid)
	}
	window, err := WindowFor(pageable)
	if err != nil {
		return Page[T]{}, err
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(window.Limit) - 1) / int64(window.Limit))
	}
	page := pageable.PageNumber()
	return Page[T]{
		Items: items,
		Pagination: Metadata{
			Page:        page,
			PageSize:    window.Limit,
			Total:       total,
			TotalPages:  totalPages,
			HasPrevious: page > 1,
			HasNext:     page < totalPages,
		},
	}, nil
}

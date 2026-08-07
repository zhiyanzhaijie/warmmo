package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"warmnote/core/internal/shared/pagination"
)

func parsePagination(request *http.Request) (pagination.Request, error) {
	query := request.URL.Query()
	page, err := parsePaginationValue(query.Get("page"), pagination.DefaultPage)
	if err != nil {
		return pagination.Request{}, fmt.Errorf("page: %w", err)
	}
	pageSize, err := parsePaginationValue(query.Get("pageSize"), pagination.DefaultPageSize)
	if err != nil {
		return pagination.Request{}, fmt.Errorf("pageSize: %w", err)
	}
	return pagination.New(page, pageSize)
}

func parsePaginationValue(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("must be an integer")
	}
	return parsed, nil
}

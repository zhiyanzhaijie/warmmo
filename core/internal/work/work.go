package work

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidWork      = errors.New("invalid work")
	ErrNotFound         = errors.New("work not found")
	ErrRevisionConflict = errors.New("work revision conflict")
	ErrFolderNotFound   = errors.New("work folder not found")
	ErrFolderConflict   = errors.New("work folder already exists")
)

type Detail struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	FolderID    string    `json:"folderId"`
	FolderName  string    `json:"folderName"`
	Status      string    `json:"status"`
	Revision    int64     `json:"revision"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type PreviewNode struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Kind  string  `json:"kind"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

type PreviewEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type Summary struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	FolderID     string        `json:"folderId"`
	FolderName   string        `json:"folderName"`
	Status       string        `json:"status"`
	Revision     int64         `json:"revision"`
	UpdatedAt    time.Time     `json:"updatedAt"`
	NodeCount    int           `json:"nodeCount"`
	PreviewNodes []PreviewNode `json:"previewNodes"`
	PreviewEdges []PreviewEdge `json:"previewEdges"`
}

type CreateInput struct {
	Title       string
	Description string
	FolderID    string
}

type UpdateInput struct {
	ID               string
	Title            string
	Description      string
	FolderID         string
	Status           string
	ExpectedRevision int64
}

type Folder struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Store interface {
	Create(context.Context, CreateInput) (Summary, error)
	Get(context.Context, string) (Detail, error)
	List(context.Context) ([]Summary, error)
	Update(context.Context, UpdateInput) (Detail, error)
	CreateFolder(context.Context, string) (Folder, error)
	ListFolders(context.Context) ([]Folder, error)
}

package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"warmnote/core/internal/domain/work"
)

const workPreviewLimit = 6

type WorkRepository struct {
	database *sql.DB
}

func NewWorkRepository(providerRepository *ProviderRepository) *WorkRepository {
	return &WorkRepository{database: providerRepository.database}
}

func (r *WorkRepository) Create(ctx context.Context, input work.CreateInput) (work.Summary, error) {
	folderName := ""
	if input.FolderID != "" {
		name, exists, err := r.getFolderName(ctx, input.FolderID)
		if err != nil {
			return work.Summary{}, err
		}
		if !exists {
			return work.Summary{}, work.ErrFolderNotFound
		}
		folderName = name
	}
	now := time.Now().UTC()
	summary := work.Summary{
		ID:           uuid.NewString(),
		Title:        input.Title,
		Description:  input.Description,
		FolderID:     input.FolderID,
		FolderName:   folderName,
		Status:       "active",
		Revision:     1,
		UpdatedAt:    now,
		PreviewNodes: []work.PreviewNode{},
		PreviewEdges: []work.PreviewEdge{},
	}
	_, err := r.database.ExecContext(ctx, `
INSERT INTO works (id, title, description, folder_id, status, revision, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, summary.ID, summary.Title, summary.Description, summary.FolderID,
		summary.Status, summary.Revision, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return work.Summary{}, fmt.Errorf("create work: %w", err)
	}
	return summary, nil
}

func (r *WorkRepository) Get(ctx context.Context, workID string) (work.Detail, error) {
	detail, err := scanWorkDetail(r.database.QueryRowContext(ctx, `
SELECT w.id, w.title, w.description, w.folder_id, COALESCE(f.name, ''), w.status, w.revision, w.updated_at
FROM works w
LEFT JOIN work_folders f ON f.id = w.folder_id
WHERE w.id = ?`, workID))
	if errors.Is(err, sql.ErrNoRows) {
		return work.Detail{}, work.ErrNotFound
	}
	if err != nil {
		return work.Detail{}, fmt.Errorf("get work: %w", err)
	}
	return detail, nil
}

func (r *WorkRepository) List(ctx context.Context) ([]work.Summary, error) {
	rows, err := r.database.QueryContext(ctx, `
SELECT w.id, w.title, w.description, w.folder_id, COALESCE(f.name, ''), w.status, w.revision,
       MAX(w.updated_at, COALESCE(n.last_updated_at, ''), COALESCE(a.last_created_at, ''), COALESCE(r.last_updated_at, '')) AS activity_at,
       COALESCE(n.node_count, 0)
FROM works w
LEFT JOIN work_folders f ON f.id = w.folder_id
LEFT JOIN (
    SELECT work_id, COUNT(*) AS node_count, MAX(updated_at) AS last_updated_at
    FROM canvas_nodes GROUP BY work_id
) n ON n.work_id = w.id
LEFT JOIN (
    SELECT work_id, MAX(created_at) AS last_created_at
    FROM canvas_actions GROUP BY work_id
) a ON a.work_id = w.id
LEFT JOIN (
    SELECT work_id, MAX(updated_at) AS last_updated_at
    FROM agent_runs GROUP BY work_id
) r ON r.work_id = w.id
ORDER BY activity_at DESC, w.id`)
	if err != nil {
		return nil, fmt.Errorf("list works: %w", err)
	}
	defer rows.Close()

	works := make([]work.Summary, 0)
	workIndexByID := make(map[string]int)
	for rows.Next() {
		var summary work.Summary
		var updatedAt string
		if err := rows.Scan(&summary.ID, &summary.Title, &summary.Description, &summary.FolderID,
			&summary.FolderName, &summary.Status, &summary.Revision, &updatedAt, &summary.NodeCount); err != nil {
			return nil, fmt.Errorf("scan work summary: %w", err)
		}
		parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse work update time: %w", err)
		}
		summary.UpdatedAt = parsedUpdatedAt
		summary.PreviewNodes = []work.PreviewNode{}
		summary.PreviewEdges = []work.PreviewEdge{}
		works = append(works, summary)
		workIndexByID[summary.ID] = len(works) - 1
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate works: %w", err)
	}
	if len(works) == 0 {
		return works, nil
	}
	if err := r.loadPreviewNodes(ctx, works, workIndexByID); err != nil {
		return nil, err
	}
	if err := r.loadPreviewEdges(ctx, works, workIndexByID); err != nil {
		return nil, err
	}
	return works, nil
}

func (r *WorkRepository) Update(ctx context.Context, input work.UpdateInput) (work.Detail, error) {
	if input.FolderID != "" {
		_, exists, err := r.getFolderName(ctx, input.FolderID)
		if err != nil {
			return work.Detail{}, err
		}
		if !exists {
			return work.Detail{}, work.ErrFolderNotFound
		}
	}
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return work.Detail{}, fmt.Errorf("begin update work: %w", err)
	}
	defer transaction.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := transaction.ExecContext(ctx, `
UPDATE works
SET title = ?, description = ?, folder_id = ?, status = ?, revision = revision + 1, updated_at = ?
WHERE id = ? AND revision = ?`, input.Title, input.Description, input.FolderID, input.Status,
		now, input.ID, input.ExpectedRevision)
	if err != nil {
		return work.Detail{}, fmt.Errorf("update work: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return work.Detail{}, fmt.Errorf("read updated work row count: %w", err)
	}
	if affected == 0 {
		var exists int
		if err := transaction.QueryRowContext(ctx, `SELECT 1 FROM works WHERE id = ?`, input.ID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return work.Detail{}, work.ErrNotFound
		} else if err != nil {
			return work.Detail{}, fmt.Errorf("check work after update conflict: %w", err)
		}
		return work.Detail{}, work.ErrRevisionConflict
	}
	detail, err := scanWorkDetail(transaction.QueryRowContext(ctx, `
SELECT w.id, w.title, w.description, w.folder_id, COALESCE(f.name, ''), w.status, w.revision, w.updated_at
FROM works w
LEFT JOIN work_folders f ON f.id = w.folder_id
WHERE w.id = ?`, input.ID))
	if err != nil {
		return work.Detail{}, fmt.Errorf("read updated work: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return work.Detail{}, fmt.Errorf("commit update work: %w", err)
	}
	return detail, nil
}

func (r *WorkRepository) CreateFolder(ctx context.Context, name string) (work.Folder, error) {
	var existingID string
	err := r.database.QueryRowContext(ctx, `SELECT id FROM work_folders WHERE name = ?`, name).Scan(&existingID)
	if err == nil {
		return work.Folder{}, work.ErrFolderConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return work.Folder{}, fmt.Errorf("check work folder name: %w", err)
	}
	now := time.Now().UTC()
	folder := work.Folder{ID: uuid.NewString(), Name: name, CreatedAt: now, UpdatedAt: now}
	_, err = r.database.ExecContext(ctx, `
INSERT INTO work_folders (id, name, sort_order, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)`, folder.ID, folder.Name, folder.SortOrder,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return work.Folder{}, fmt.Errorf("create work folder: %w", err)
	}
	return folder, nil
}

func (r *WorkRepository) ListFolders(ctx context.Context) ([]work.Folder, error) {
	rows, err := r.database.QueryContext(ctx, `
SELECT id, name, sort_order, created_at, updated_at
FROM work_folders ORDER BY sort_order, name`)
	if err != nil {
		return nil, fmt.Errorf("list work folders: %w", err)
	}
	defer rows.Close()
	folders := make([]work.Folder, 0)
	for rows.Next() {
		var folder work.Folder
		var createdAt, updatedAt string
		if err := rows.Scan(&folder.ID, &folder.Name, &folder.SortOrder, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan work folder: %w", err)
		}
		var err error
		folder.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse work folder creation time: %w", err)
		}
		folder.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse work folder update time: %w", err)
		}
		folders = append(folders, folder)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate work folders: %w", err)
	}
	return folders, nil
}

func (r *WorkRepository) getFolderName(ctx context.Context, folderID string) (string, bool, error) {
	var name string
	err := r.database.QueryRowContext(ctx, `SELECT name FROM work_folders WHERE id = ?`, folderID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get work folder name: %w", err)
	}
	return name, true, nil
}

func scanWorkDetail(scanner rowScanner) (work.Detail, error) {
	var detail work.Detail
	var updatedAt string
	if err := scanner.Scan(&detail.ID, &detail.Title, &detail.Description, &detail.FolderID,
		&detail.FolderName, &detail.Status, &detail.Revision, &updatedAt); err != nil {
		return work.Detail{}, err
	}
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return work.Detail{}, fmt.Errorf("parse work update time: %w", err)
	}
	detail.UpdatedAt = parsedUpdatedAt
	return detail, nil
}

func (r *WorkRepository) loadPreviewNodes(ctx context.Context, works []work.Summary, workIndexByID map[string]int) error {
	rows, err := r.database.QueryContext(ctx, `
SELECT work_id, id, title, kind, x, y
FROM (
    SELECT work_id, id, title, kind, x, y,
           ROW_NUMBER() OVER (PARTITION BY work_id ORDER BY updated_at DESC, id) AS row_number
    FROM canvas_nodes
)
WHERE row_number <= ?
ORDER BY work_id, row_number`, workPreviewLimit)
	if err != nil {
		return fmt.Errorf("list work preview nodes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var workID string
		var node work.PreviewNode
		if err := rows.Scan(&workID, &node.ID, &node.Label, &node.Kind, &node.X, &node.Y); err != nil {
			return fmt.Errorf("scan work preview node: %w", err)
		}
		if workIndex, exists := workIndexByID[workID]; exists {
			works[workIndex].PreviewNodes = append(works[workIndex].PreviewNodes, node)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate work preview nodes: %w", err)
	}
	return nil
}

func (r *WorkRepository) loadPreviewEdges(ctx context.Context, works []work.Summary, workIndexByID map[string]int) error {
	rows, err := r.database.QueryContext(ctx, `
WITH preview_nodes AS (
    SELECT work_id, id,
           ROW_NUMBER() OVER (PARTITION BY work_id ORDER BY updated_at DESC, id) AS row_number
    FROM canvas_nodes
)
SELECT e.work_id, e.source_node_id, e.target_node_id
FROM canvas_edges e
JOIN preview_nodes source ON source.work_id = e.work_id AND source.id = e.source_node_id AND source.row_number <= ?
JOIN preview_nodes target ON target.work_id = e.work_id AND target.id = e.target_node_id AND target.row_number <= ?
ORDER BY e.work_id, e.created_at`, workPreviewLimit, workPreviewLimit)
	if err != nil {
		return fmt.Errorf("list work preview edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var workID string
		var edge work.PreviewEdge
		if err := rows.Scan(&workID, &edge.Source, &edge.Target); err != nil {
			return fmt.Errorf("scan work preview edge: %w", err)
		}
		if workIndex, exists := workIndexByID[workID]; exists {
			works[workIndex].PreviewEdges = append(works[workIndex].PreviewEdges, edge)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate work preview edges: %w", err)
	}
	return nil
}

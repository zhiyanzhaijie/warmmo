package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"warmnote/core/internal/agent"
	"warmnote/core/internal/canvas"
)

type CanvasRepository struct {
	database *sql.DB
}

func NewCanvasRepository(providerRepository *ProviderRepository) *CanvasRepository {
	return &CanvasRepository{database: providerRepository.database}
}

func (r *CanvasRepository) CreateNode(ctx context.Context, input canvas.CreateNodeInput) (canvas.Node, error) {
	input.WorkID = strings.TrimSpace(input.WorkID)
	input.Kind = strings.TrimSpace(input.Kind)
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	if input.WorkID == "" || input.Kind == "" || input.Title == "" || input.Content == "" {
		return canvas.Node{}, canvas.ErrInvalidNode
	}
	now := time.Now().UTC()
	node := canvas.Node{
		ID: uuid.NewString(), WorkID: input.WorkID, Revision: 1, Kind: input.Kind,
		Title: input.Title, Content: input.Content, X: input.X, Y: input.Y, CreatedAt: now, UpdatedAt: now,
	}
	_, err := r.database.ExecContext(ctx, `
INSERT INTO canvas_nodes (id, work_id, revision, kind, title, content, x, y, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, node.ID, node.WorkID, node.Revision, node.Kind, node.Title,
		node.Content, node.X, node.Y, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return canvas.Node{}, fmt.Errorf("create canvas node: %w", err)
	}
	return node, nil
}

func (r *CanvasRepository) ListNodes(ctx context.Context, workID string) ([]canvas.Node, error) {
	rows, err := r.database.QueryContext(ctx, `
SELECT id, work_id, revision, kind, title, content, x, y, created_at, updated_at
FROM canvas_nodes WHERE work_id = ? ORDER BY created_at`, workID)
	if err != nil {
		return nil, fmt.Errorf("list canvas nodes: %w", err)
	}
	defer rows.Close()
	nodes := make([]canvas.Node, 0)
	for rows.Next() {
		node, err := scanCanvasNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate canvas nodes: %w", err)
	}
	return nodes, nil
}

func (r *CanvasRepository) GetNodes(ctx context.Context, workID string, nodeIDs []string) ([]canvas.Node, error) {
	if len(nodeIDs) == 0 {
		return []canvas.Node{}, nil
	}
	if len(nodeIDs) > 64 {
		return nil, fmt.Errorf("%w: at most 64 nodes can be read", canvas.ErrInvalidNode)
	}
	nodes := make([]canvas.Node, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		node, err := scanCanvasNode(r.database.QueryRowContext(ctx, `
SELECT id, work_id, revision, kind, title, content, x, y, created_at, updated_at
FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, nodeID))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", canvas.ErrNodeNotFound, nodeID)
		}
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (r *CanvasRepository) UpdateNodePosition(ctx context.Context, workID, nodeID string, x, y float64) error {
	result, err := r.database.ExecContext(ctx, `
UPDATE canvas_nodes SET x = ?, y = ?, updated_at = ? WHERE work_id = ? AND id = ?`,
		x, y, time.Now().UTC().Format(time.RFC3339Nano), workID, nodeID)
	if err != nil {
		return fmt.Errorf("update canvas node position: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated canvas node position: %w", err)
	}
	if changed == 0 {
		return canvas.ErrNodeNotFound
	}
	return nil
}

func (r *CanvasRepository) CreateCandidate(ctx context.Context, candidate agent.Candidate) (agent.Candidate, error) {
	existing, err := r.candidateByRun(ctx, candidate.RunID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return agent.Candidate{}, err
	}
	if candidate.ID == "" {
		candidate.ID = uuid.NewString()
	}
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = time.Now().UTC()
	}
	_, err = r.database.ExecContext(ctx, `
INSERT INTO agent_candidates (id, run_id, work_id, skill_id, skill_version, content, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, candidate.ID, candidate.RunID, candidate.WorkID, candidate.SkillID,
		candidate.SkillVersion, candidate.Content, candidate.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return agent.Candidate{}, fmt.Errorf("create canvas candidate: %w", err)
	}
	return candidate, nil
}

func (r *CanvasRepository) candidateByRun(ctx context.Context, runID string) (agent.Candidate, error) {
	var candidate agent.Candidate
	var createdAt string
	err := r.database.QueryRowContext(ctx, `
SELECT id, run_id, work_id, skill_id, skill_version, content, created_at
FROM agent_candidates WHERE run_id = ?`, runID).Scan(&candidate.ID, &candidate.RunID, &candidate.WorkID,
		&candidate.SkillID, &candidate.SkillVersion, &candidate.Content, &createdAt)
	if err != nil {
		return agent.Candidate{}, err
	}
	candidate.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return agent.Candidate{}, fmt.Errorf("parse canvas candidate time: %w", err)
	}
	return candidate, nil
}

func (r *CanvasRepository) ListCandidates(ctx context.Context, workID string) ([]agent.Candidate, error) {
	rows, err := r.database.QueryContext(ctx, `
SELECT id, run_id, work_id, skill_id, skill_version, content, created_at
FROM agent_candidates WHERE work_id = ? ORDER BY created_at DESC`, workID)
	if err != nil {
		return nil, fmt.Errorf("list canvas candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]agent.Candidate, 0)
	for rows.Next() {
		var candidate agent.Candidate
		var createdAt string
		if err := rows.Scan(&candidate.ID, &candidate.RunID, &candidate.WorkID, &candidate.SkillID,
			&candidate.SkillVersion, &candidate.Content, &createdAt); err != nil {
			return nil, fmt.Errorf("scan canvas candidate: %w", err)
		}
		var err error
		candidate.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse canvas candidate time: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func scanCanvasNode(scanner rowScanner) (canvas.Node, error) {
	var node canvas.Node
	var createdAt, updatedAt string
	if err := scanner.Scan(&node.ID, &node.WorkID, &node.Revision, &node.Kind, &node.Title, &node.Content, &node.X, &node.Y, &createdAt, &updatedAt); err != nil {
		return canvas.Node{}, err
	}
	var err error
	node.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return canvas.Node{}, fmt.Errorf("parse canvas node created time: %w", err)
	}
	node.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return canvas.Node{}, fmt.Errorf("parse canvas node updated time: %w", err)
	}
	return node, nil
}

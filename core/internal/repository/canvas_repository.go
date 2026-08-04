package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
	if input.WorkID == "" || input.Kind == "" || input.Title == "" {
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

type candidateQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func candidateByID(ctx context.Context, querier candidateQuerier, workID, candidateID string) (agent.Candidate, error) {
	candidate, err := scanCandidate(querier.QueryRowContext(ctx, `
SELECT c.id, c.run_id, c.work_id, c.skill_id, c.skill_version, c.status, c.kind, c.title,
       c.content, c.x, c.y, c.accepted_node_id, c.created_at, c.decided_at, r.context_node_ids_json
FROM agent_candidates c
JOIN agent_runs r ON r.id = c.run_id
WHERE c.work_id = ? AND c.id = ?`, workID, candidateID))
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Candidate{}, canvas.ErrCandidateNotFound
	}
	return candidate, err
}

func scanCandidate(scanner rowScanner) (agent.Candidate, error) {
	var candidate agent.Candidate
	var createdAt, decidedAt, contextNodeIDsJSON string
	if err := scanner.Scan(
		&candidate.ID, &candidate.RunID, &candidate.WorkID, &candidate.SkillID, &candidate.SkillVersion,
		&candidate.Status, &candidate.Kind, &candidate.Title, &candidate.Content, &candidate.X, &candidate.Y,
		&candidate.AcceptedNodeID, &createdAt, &decidedAt, &contextNodeIDsJSON,
	); err != nil {
		return agent.Candidate{}, err
	}
	if err := json.Unmarshal([]byte(contextNodeIDsJSON), &candidate.ContextNodeIDs); err != nil {
		return agent.Candidate{}, fmt.Errorf("decode canvas candidate context node ids: %w", err)
	}
	var err error
	candidate.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return agent.Candidate{}, fmt.Errorf("parse canvas candidate time: %w", err)
	}
	if decidedAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, decidedAt)
		if err != nil {
			return agent.Candidate{}, fmt.Errorf("parse canvas candidate decision time: %w", err)
		}
		candidate.DecidedAt = &parsed
	}
	return candidate, nil
}

func (r *CanvasRepository) initialCandidatePosition(
	ctx context.Context,
	transaction *sql.Tx,
	workID string,
	contextNodeIDs []string,
) (float64, float64, error) {
	x, y := 520.0, 80.0
	maxX := -math.MaxFloat64
	totalY := 0.0
	found := 0
	for _, nodeID := range contextNodeIDs {
		var nodeX, nodeY float64
		err := transaction.QueryRowContext(ctx, `
SELECT x, y FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, nodeID).Scan(&nodeX, &nodeY)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, 0, fmt.Errorf("read candidate context position: %w", err)
		}
		maxX = math.Max(maxX, nodeX)
		totalY += nodeY
		found++
	}
	if found > 0 {
		x = maxX + 320
		y = totalY / float64(found)
	}
	var pendingCount int
	if err := transaction.QueryRowContext(ctx, `
SELECT COUNT(*) FROM agent_candidates WHERE work_id = ? AND status = ?`,
		workID, agent.CandidateStatusPending).Scan(&pendingCount); err != nil {
		return 0, 0, fmt.Errorf("count pending canvas candidates: %w", err)
	}
	x += float64(pendingCount/8) * 36
	y += float64(pendingCount%8) * 36
	return x, y, nil
}

func (r *CanvasRepository) candidateMutationError(ctx context.Context, workID, candidateID string) error {
	var status agent.CandidateStatus
	err := r.database.QueryRowContext(ctx, `
SELECT status FROM agent_candidates WHERE work_id = ? AND id = ?`, workID, candidateID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return canvas.ErrCandidateNotFound
	}
	if err != nil {
		return fmt.Errorf("read canvas candidate status: %w", err)
	}
	return canvas.ErrCandidateResolved
}

func candidateTitle(content, kind string) string {
	for _, line := range strings.Split(content, "\n") {
		title := strings.TrimSpace(line)
		title = strings.TrimSpace(strings.TrimLeft(title, "#*-"))
		if title == "" {
			continue
		}
		runes := []rune(title)
		if len(runes) > 32 {
			return string(runes[:32]) + "…"
		}
		return title
	}
	if kind == canvas.NodeKindSectionDraft {
		return "小节草稿候选"
	}
	return "内容候选"
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

func (r *CanvasRepository) GetNode(ctx context.Context, workID, nodeID string) (canvas.Node, error) {
	node, err := scanCanvasNode(r.database.QueryRowContext(ctx, `
SELECT id, work_id, revision, kind, title, content, x, y, created_at, updated_at
FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return canvas.Node{}, canvas.ErrNodeNotFound
	}
	if err != nil {
		return canvas.Node{}, fmt.Errorf("get canvas node: %w", err)
	}
	return node, nil
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

func (r *CanvasRepository) UpdateNode(ctx context.Context, input canvas.UpdateNodeInput) (canvas.Node, error) {
	now := time.Now().UTC()
	node, err := scanCanvasNode(r.database.QueryRowContext(ctx, `
UPDATE canvas_nodes
SET title = ?, content = ?, revision = revision + 1, updated_at = ?
WHERE work_id = ? AND id = ? AND revision = ?
RETURNING id, work_id, revision, kind, title, content, x, y, created_at, updated_at`,
		input.Title, input.Content, now.Format(time.RFC3339Nano),
		input.WorkID, input.NodeID, input.ExpectedRevision))
	if err == nil {
		return node, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return canvas.Node{}, fmt.Errorf("update canvas node: %w", err)
	}
	if _, err := r.GetNode(ctx, input.WorkID, input.NodeID); errors.Is(err, canvas.ErrNodeNotFound) {
		return canvas.Node{}, canvas.ErrNodeNotFound
	} else if err != nil {
		return canvas.Node{}, err
	}
	return canvas.Node{}, canvas.ErrRevisionConflict
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

func (r *CanvasRepository) ListEdges(ctx context.Context, workID string) ([]canvas.Edge, error) {
	rows, err := r.database.QueryContext(ctx, `
SELECT id, work_id, source_node_id, target_node_id, kind, created_at
FROM canvas_edges WHERE work_id = ? ORDER BY created_at`, workID)
	if err != nil {
		return nil, fmt.Errorf("list canvas edges: %w", err)
	}
	defer rows.Close()
	edges := make([]canvas.Edge, 0)
	for rows.Next() {
		var edge canvas.Edge
		var createdAt string
		if err := rows.Scan(&edge.ID, &edge.WorkID, &edge.SourceNodeID, &edge.TargetNodeID, &edge.Kind, &createdAt); err != nil {
			return nil, fmt.Errorf("scan canvas edge: %w", err)
		}
		edge.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse canvas edge time: %w", err)
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate canvas edges: %w", err)
	}
	return edges, nil
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
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return agent.Candidate{}, fmt.Errorf("begin create canvas candidate: %w", err)
	}
	defer transaction.Rollback()

	var contextNodeIDsJSON string
	err = transaction.QueryRowContext(ctx, `
SELECT target, context_node_ids_json
FROM agent_runs WHERE id = ? AND work_id = ?`, candidate.RunID, candidate.WorkID).
		Scan(&candidate.Kind, &contextNodeIDsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return agent.Candidate{}, fmt.Errorf("create canvas candidate: %w", agent.ErrRunNotFound)
	}
	if err != nil {
		return agent.Candidate{}, fmt.Errorf("read candidate run: %w", err)
	}
	if err := json.Unmarshal([]byte(contextNodeIDsJSON), &candidate.ContextNodeIDs); err != nil {
		return agent.Candidate{}, fmt.Errorf("decode candidate context node ids: %w", err)
	}
	candidate.Status = agent.CandidateStatusPending
	candidate.Title = candidateTitle(candidate.Content, candidate.Kind)
	candidate.X, candidate.Y, err = r.initialCandidatePosition(ctx, transaction, candidate.WorkID, candidate.ContextNodeIDs)
	if err != nil {
		return agent.Candidate{}, err
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO agent_candidates (
    id, run_id, work_id, skill_id, skill_version, status, kind, title, content, x, y,
    accepted_node_id, created_at, decided_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, '')`,
		candidate.ID, candidate.RunID, candidate.WorkID, candidate.SkillID, candidate.SkillVersion,
		candidate.Status, candidate.Kind, candidate.Title, candidate.Content, candidate.X, candidate.Y,
		candidate.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return agent.Candidate{}, fmt.Errorf("create canvas candidate: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return agent.Candidate{}, fmt.Errorf("commit canvas candidate: %w", err)
	}
	return candidate, nil
}

func (r *CanvasRepository) candidateByRun(ctx context.Context, runID string) (agent.Candidate, error) {
	return scanCandidate(r.database.QueryRowContext(ctx, `
SELECT c.id, c.run_id, c.work_id, c.skill_id, c.skill_version, c.status, c.kind, c.title,
       c.content, c.x, c.y, c.accepted_node_id, c.created_at, c.decided_at, r.context_node_ids_json
FROM agent_candidates c
JOIN agent_runs r ON r.id = c.run_id
WHERE c.run_id = ?`, runID))
}

func (r *CanvasRepository) ListCandidates(ctx context.Context, workID string) ([]agent.Candidate, error) {
	rows, err := r.database.QueryContext(ctx, `
SELECT c.id, c.run_id, c.work_id, c.skill_id, c.skill_version, c.status, c.kind, c.title,
       c.content, c.x, c.y, c.accepted_node_id, c.created_at, c.decided_at, r.context_node_ids_json
FROM agent_candidates c
JOIN agent_runs r ON r.id = c.run_id
WHERE c.work_id = ? AND c.status = ?
ORDER BY c.created_at DESC`, workID, agent.CandidateStatusPending)
	if err != nil {
		return nil, fmt.Errorf("list canvas candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]agent.Candidate, 0)
	for rows.Next() {
		candidate, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate canvas candidates: %w", err)
	}
	return candidates, nil
}

func (r *CanvasRepository) UpdateCandidatePosition(ctx context.Context, workID, candidateID string, x, y float64) error {
	result, err := r.database.ExecContext(ctx, `
UPDATE agent_candidates SET x = ?, y = ?
WHERE work_id = ? AND id = ? AND status = ?`,
		x, y, workID, candidateID, agent.CandidateStatusPending)
	if err != nil {
		return fmt.Errorf("update canvas candidate position: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated canvas candidate position: %w", err)
	}
	if changed > 0 {
		return nil
	}
	return r.candidateMutationError(ctx, workID, candidateID)
}

func (r *CanvasRepository) AcceptCandidate(ctx context.Context, input canvas.AcceptCandidateInput) (canvas.Node, error) {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return canvas.Node{}, fmt.Errorf("begin accept canvas candidate: %w", err)
	}
	defer transaction.Rollback()

	candidate, err := candidateByID(ctx, transaction, input.WorkID, input.CandidateID)
	if err != nil {
		return canvas.Node{}, err
	}
	switch candidate.Status {
	case agent.CandidateStatusAccepted:
		node, err := scanCanvasNode(transaction.QueryRowContext(ctx, `
SELECT id, work_id, revision, kind, title, content, x, y, created_at, updated_at
FROM canvas_nodes WHERE work_id = ? AND id = ?`, input.WorkID, candidate.AcceptedNodeID))
		if errors.Is(err, sql.ErrNoRows) {
			return canvas.Node{}, canvas.ErrCandidateResolved
		}
		return node, err
	case agent.CandidateStatusRejected:
		return canvas.Node{}, canvas.ErrCandidateResolved
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = candidate.Title
	}
	now := time.Now().UTC()
	node := canvas.Node{
		ID: uuid.NewString(), WorkID: candidate.WorkID, Revision: 1, Kind: candidate.Kind,
		Title: title, Content: candidate.Content, X: candidate.X, Y: candidate.Y,
		CreatedAt: now, UpdatedAt: now,
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO canvas_nodes (id, work_id, revision, kind, title, content, x, y, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		node.ID, node.WorkID, node.Revision, node.Kind, node.Title, node.Content, node.X, node.Y,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return canvas.Node{}, fmt.Errorf("create accepted canvas node: %w", err)
	}
	for _, sourceNodeID := range candidate.ContextNodeIDs {
		_, err := transaction.ExecContext(ctx, `
INSERT INTO canvas_edges (id, work_id, source_node_id, target_node_id, kind, created_at)
SELECT ?, ?, id, ?, 'generated_from', ?
FROM canvas_nodes
WHERE work_id = ? AND id = ? AND id <> ?
ON CONFLICT(work_id, source_node_id, target_node_id, kind) DO NOTHING`,
			uuid.NewString(), candidate.WorkID, node.ID, now.Format(time.RFC3339Nano),
			candidate.WorkID, sourceNodeID, node.ID)
		if err != nil {
			return canvas.Node{}, fmt.Errorf("create accepted canvas edge: %w", err)
		}
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE agent_candidates
SET status = ?, title = ?, accepted_node_id = ?, decided_at = ?
WHERE work_id = ? AND id = ? AND status = ?`,
		agent.CandidateStatusAccepted, title, node.ID, now.Format(time.RFC3339Nano),
		input.WorkID, input.CandidateID, agent.CandidateStatusPending)
	if err != nil {
		return canvas.Node{}, fmt.Errorf("accept canvas candidate: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return canvas.Node{}, fmt.Errorf("read accepted canvas candidate: %w", err)
	}
	if changed == 0 {
		return canvas.Node{}, canvas.ErrCandidateResolved
	}
	if err := transaction.Commit(); err != nil {
		return canvas.Node{}, fmt.Errorf("commit accepted canvas candidate: %w", err)
	}
	return node, nil
}

func (r *CanvasRepository) RejectCandidate(ctx context.Context, workID, candidateID string) error {
	now := time.Now().UTC()
	result, err := r.database.ExecContext(ctx, `
UPDATE agent_candidates SET status = ?, decided_at = ?
WHERE work_id = ? AND id = ? AND status = ?`,
		agent.CandidateStatusRejected, now.Format(time.RFC3339Nano), workID, candidateID,
		agent.CandidateStatusPending)
	if err != nil {
		return fmt.Errorf("reject canvas candidate: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rejected canvas candidate: %w", err)
	}
	if changed > 0 {
		return nil
	}
	var status agent.CandidateStatus
	err = r.database.QueryRowContext(ctx, `
SELECT status FROM agent_candidates WHERE work_id = ? AND id = ?`, workID, candidateID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return canvas.ErrCandidateNotFound
	}
	if err != nil {
		return fmt.Errorf("read canvas candidate status: %w", err)
	}
	if status == agent.CandidateStatusRejected {
		return nil
	}
	return canvas.ErrCandidateResolved
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

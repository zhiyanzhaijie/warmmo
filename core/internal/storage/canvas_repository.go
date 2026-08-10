package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	agent "warmnote/core/internal/agent/writing"
	"warmnote/core/internal/domain/canvas"
)

type CanvasRepository struct {
	database      *sql.DB
	dataDirectory string
}

func NewCanvasRepository(providerRepository *ProviderRepository) *CanvasRepository {
	return &CanvasRepository{
		database:      providerRepository.database,
		dataDirectory: filepath.Dir(providerRepository.databasePath),
	}
}

func (r *CanvasRepository) CreateNode(ctx context.Context, input canvas.CreateNodeInput) (canvas.Node, error) {
	input.WorkID = strings.TrimSpace(input.WorkID)
	input.Kind = canvas.NodeKind(strings.TrimSpace(string(input.Kind)))
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	if input.WorkID == "" || !canvas.IsValidNodeKind(input.Kind) || input.Title == "" {
		return canvas.Node{}, canvas.ErrInvalidNode
	}
	now := time.Now().UTC()
	node := canvas.Node{
		ID: uuid.NewString(), WorkID: input.WorkID, Revision: 1, Kind: input.Kind,
		Title: input.Title, Content: input.Content, X: input.X, Y: input.Y, CreatedAt: now, UpdatedAt: now,
	}
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return canvas.Node{}, fmt.Errorf("begin create canvas node: %w", err)
	}
	defer transaction.Rollback()
	_, err = transaction.ExecContext(ctx, `
INSERT INTO canvas_nodes (id, work_id, revision, kind, title, content, x, y, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, node.ID, node.WorkID, node.Revision, node.Kind, node.Title,
		node.Content, node.X, node.Y, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return canvas.Node{}, fmt.Errorf("create canvas node: %w", err)
	}
	_, err = createInitialNodeVersion(ctx, transaction, node)
	if err != nil {
		return canvas.Node{}, err
	}
	contextNodeIDs := uniqueStrings(input.ContextNodeIDs)
	edges := make([]canvas.Edge, 0, len(contextNodeIDs))
	for _, sourceNodeID := range contextNodeIDs {
		if sourceNodeID == node.ID {
			continue
		}
		var sourceExists int
		err := transaction.QueryRowContext(ctx, `
SELECT 1 FROM canvas_nodes WHERE work_id = ? AND id = ?`, node.WorkID, sourceNodeID).Scan(&sourceExists)
		if errors.Is(err, sql.ErrNoRows) {
			return canvas.Node{}, canvas.ErrNodeNotFound
		}
		if err != nil {
			return canvas.Node{}, fmt.Errorf("read canvas context node: %w", err)
		}
		edge := canvas.Edge{
			ID: uuid.NewString(), WorkID: node.WorkID, SourceNodeID: sourceNodeID,
			TargetNodeID: node.ID, Kind: "generated_from", CreatedAt: now,
		}
		_, err = transaction.ExecContext(ctx, `
INSERT INTO canvas_edges (id, work_id, source_node_id, target_node_id, kind, created_at)
VALUES (?, ?, ?, ?, ?, ?)`, edge.ID, edge.WorkID, edge.SourceNodeID, edge.TargetNodeID,
			edge.Kind, edge.CreatedAt.Format(time.RFC3339Nano))
		if err != nil {
			return canvas.Node{}, fmt.Errorf("create canvas context edge: %w", err)
		}
		edges = append(edges, edge)
	}
	if err := appendCanvasAction(ctx, transaction, node.WorkID, actionCreateNodes, "创建节点", createNodesActionPayload{
		Nodes: []canvas.Node{node},
		Edges: edges,
	}); err != nil {
		return canvas.Node{}, err
	}
	if err := transaction.Commit(); err != nil {
		return canvas.Node{}, fmt.Errorf("commit create canvas node: %w", err)
	}
	return node, nil
}

func createInitialNodeVersion(ctx context.Context, transaction *sql.Tx, node canvas.Node) (string, error) {
	versionID := uuid.NewString()
	if _, err := transaction.ExecContext(ctx, `INSERT INTO canvas_node_versions (id,node_id,work_id,version_number,title,content,created_at) VALUES (?,?,?,?,?,?,?)`, versionID, node.ID, node.WorkID, 1, node.Title, node.Content, node.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return "", fmt.Errorf("create initial node version: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE canvas_nodes SET current_version_id=? WHERE work_id=? AND id=?`, versionID, node.WorkID, node.ID); err != nil {
		return "", fmt.Errorf("set initial node version: %w", err)
	}
	return versionID, nil
}

func createNodeVersion(ctx context.Context, transaction *sql.Tx, node canvas.Node, parentVersionID, sourceRunID string) (string, error) {
	var nextVersion int64
	if err := transaction.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM canvas_node_versions WHERE work_id=? AND node_id=?`, node.WorkID, node.ID).Scan(&nextVersion); err != nil {
		return "", fmt.Errorf("read next canvas node version: %w", err)
	}
	versionID := uuid.NewString()
	if _, err := transaction.ExecContext(ctx, `INSERT INTO canvas_node_versions (id,node_id,work_id,version_number,parent_version_id,title,content,source_run_id,created_at) VALUES (?,?,?,?,?,?,?,?,?)`, versionID, node.ID, node.WorkID, nextVersion, parentVersionID, node.Title, node.Content, sourceRunID, node.UpdatedAt.Format(time.RFC3339Nano)); err != nil {
		return "", fmt.Errorf("create canvas node version: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE canvas_nodes SET current_version_id=? WHERE work_id=? AND id=?`, versionID, node.WorkID, node.ID); err != nil {
		return "", fmt.Errorf("set current canvas node version: %w", err)
	}
	return versionID, nil
}

type candidateQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func candidateByID(ctx context.Context, querier candidateQuerier, workID, candidateID string) (agent.Candidate, error) {
	candidate, err := scanCandidate(querier.QueryRowContext(ctx, `
SELECT c.id, c.run_id, c.work_id, c.skill_id, c.skill_version, c.status, c.kind, c.title,
       c.content, c.x, c.y, c.accepted_node_id, c.created_at, c.decided_at, r.context_node_ids_json,
       c.candidate_type, c.node_id, c.base_version_id, c.reason, c.change_score
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
		&candidate.CandidateType, &candidate.NodeID, &candidate.BaseVersionID, &candidate.Reason, &candidate.ChangeScore,
	); err != nil {
		return agent.Candidate{}, err
	}
	if err := json.Unmarshal([]byte(contextNodeIDsJSON), &candidate.ContextNodeIDs); err != nil {
		return agent.Candidate{}, fmt.Errorf("decode canvas candidate context node ids: %w", err)
	}
	if candidate.CandidateType == "version" && candidate.NodeID != "" {
		candidate.ContextNodeIDs = []string{candidate.NodeID}
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
	if kind == string(canvas.NodeKindChapterSection) {
		return "章节小节候选"
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
	for index := range nodes {
		if err := r.hydrateCurrentVersionID(ctx, &nodes[index]); err != nil {
			return nil, err
		}
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
	if err := r.hydrateCurrentVersionID(ctx, &node); err != nil {
		return canvas.Node{}, err
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
		if err := r.hydrateCurrentVersionID(ctx, &node); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (r *CanvasRepository) hydrateCurrentVersionID(ctx context.Context, node *canvas.Node) error {
	var versionID string
	err := r.database.QueryRowContext(ctx, `SELECT current_version_id FROM canvas_nodes WHERE work_id=? AND id=?`, node.WorkID, node.ID).Scan(&versionID)
	if errors.Is(err, sql.ErrNoRows) {
		return canvas.ErrNodeNotFound
	}
	if err != nil {
		return fmt.Errorf("read current node version: %w", err)
	}
	node.CurrentVersionID = versionID
	return nil
}

func (r *CanvasRepository) UpdateNode(ctx context.Context, input canvas.UpdateNodeInput) (canvas.Node, error) {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return canvas.Node{}, fmt.Errorf("begin update canvas node: %w", err)
	}
	defer transaction.Rollback()
	locked, err := isNodeArchiveLocked(ctx, transaction, input.WorkID, input.NodeID)
	if err != nil {
		return canvas.Node{}, err
	}
	if locked {
		return canvas.Node{}, canvas.ErrArchivedNodeLocked
	}
	before, err := scanCanvasNode(transaction.QueryRowContext(ctx, `
SELECT id, work_id, revision, kind, title, content, x, y, created_at, updated_at
FROM canvas_nodes WHERE work_id = ? AND id = ?`, input.WorkID, input.NodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return canvas.Node{}, canvas.ErrNodeNotFound
	}
	if err != nil {
		return canvas.Node{}, fmt.Errorf("read canvas node before update: %w", err)
	}
	if before.Revision != input.ExpectedRevision {
		return canvas.Node{}, canvas.ErrRevisionConflict
	}
	var beforeVersionID string
	if err := transaction.QueryRowContext(ctx, `SELECT current_version_id FROM canvas_nodes WHERE work_id=? AND id=?`, input.WorkID, input.NodeID).Scan(&beforeVersionID); err != nil {
		return canvas.Node{}, fmt.Errorf("read canvas node version before update: %w", err)
	}
	now := time.Now().UTC()
	node, err := scanCanvasNode(transaction.QueryRowContext(ctx, `
UPDATE canvas_nodes
SET title = ?, content = ?, revision = revision + 1, updated_at = ?
WHERE work_id = ? AND id = ? AND revision = ?
RETURNING id, work_id, revision, kind, title, content, x, y, created_at, updated_at`,
		input.Title, input.Content, now.Format(time.RFC3339Nano),
		input.WorkID, input.NodeID, input.ExpectedRevision))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return canvas.Node{}, canvas.ErrRevisionConflict
		}
		return canvas.Node{}, fmt.Errorf("update canvas node: %w", err)
	}
	if before.Title != node.Title || before.Content != node.Content {
		if _, err := createNodeVersion(ctx, transaction, node, beforeVersionID, ""); err != nil {
			return canvas.Node{}, err
		}
		payload := updateNodeActionPayload{
			Before: nodeContentState{NodeID: before.ID, Title: before.Title, Content: before.Content},
			After:  nodeContentState{NodeID: node.ID, Title: node.Title, Content: node.Content},
		}
		if err := appendCanvasAction(ctx, transaction, input.WorkID, actionUpdateNode, "编辑节点", payload); err != nil {
			return canvas.Node{}, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return canvas.Node{}, fmt.Errorf("commit update canvas node: %w", err)
	}
	return node, nil
}

func (r *CanvasRepository) UpdateNodePosition(ctx context.Context, workID, nodeID string, x, y float64) error {
	return r.UpdateNodePositions(ctx, workID, []canvas.NodePosition{{NodeID: nodeID, X: x, Y: y}})
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

func (r *CanvasRepository) CreateEdge(ctx context.Context, input canvas.CreateEdgeInput) (canvas.Edge, error) {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return canvas.Edge{}, fmt.Errorf("begin create canvas edge: %w", err)
	}
	defer transaction.Rollback()

	var nodeCount int
	err = transaction.QueryRowContext(ctx, `
SELECT COUNT(*) FROM canvas_nodes
WHERE work_id = ? AND id IN (?, ?)`, input.WorkID, input.SourceNodeID, input.TargetNodeID).Scan(&nodeCount)
	if err != nil {
		return canvas.Edge{}, fmt.Errorf("read canvas edge nodes: %w", err)
	}
	if nodeCount != 2 {
		return canvas.Edge{}, canvas.ErrNodeNotFound
	}
	locked, err := isNodeArchiveLocked(ctx, transaction, input.WorkID, input.TargetNodeID)
	if err != nil {
		return canvas.Edge{}, err
	}
	if locked {
		return canvas.Edge{}, canvas.ErrArchivedNodeLocked
	}

	existing, err := scanCanvasEdge(transaction.QueryRowContext(ctx, `
SELECT id, work_id, source_node_id, target_node_id, kind, created_at
FROM canvas_edges
WHERE work_id = ? AND source_node_id = ? AND target_node_id = ? AND kind = 'generated_from'`,
		input.WorkID, input.SourceNodeID, input.TargetNodeID))
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return canvas.Edge{}, fmt.Errorf("read existing canvas edge: %w", err)
	}

	now := time.Now().UTC()
	edge := canvas.Edge{
		ID: uuid.NewString(), WorkID: input.WorkID, SourceNodeID: input.SourceNodeID,
		TargetNodeID: input.TargetNodeID, Kind: "generated_from", CreatedAt: now,
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO canvas_edges (id, work_id, source_node_id, target_node_id, kind, created_at)
VALUES (?, ?, ?, ?, ?, ?)`, edge.ID, edge.WorkID, edge.SourceNodeID, edge.TargetNodeID,
		edge.Kind, edge.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return canvas.Edge{}, fmt.Errorf("create canvas edge: %w", err)
	}
	if err := appendCanvasAction(ctx, transaction, input.WorkID, actionCreateEdge, "创建连接", createEdgeActionPayload{
		Edge: edge,
	}); err != nil {
		return canvas.Edge{}, err
	}
	if err := transaction.Commit(); err != nil {
		return canvas.Edge{}, fmt.Errorf("commit create canvas edge: %w", err)
	}
	return edge, nil
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
    accepted_node_id, created_at, decided_at, candidate_type, node_id, base_version_id, reason, change_score
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, '', ?, ?, ?, ?, ?)`,
		candidate.ID, candidate.RunID, candidate.WorkID, candidate.SkillID, candidate.SkillVersion,
		candidate.Status, candidate.Kind, candidate.Title, candidate.Content, candidate.X, candidate.Y,
		candidate.CreatedAt.Format(time.RFC3339Nano), valueOrDefault(candidate.CandidateType, "node"), candidate.NodeID, candidate.BaseVersionID, candidate.Reason, candidate.ChangeScore)
	if err != nil {
		return agent.Candidate{}, fmt.Errorf("create canvas candidate: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return agent.Candidate{}, fmt.Errorf("commit canvas candidate: %w", err)
	}
	return candidate, nil
}

func scanCanvasEdge(scanner rowScanner) (canvas.Edge, error) {
	var edge canvas.Edge
	var createdAt string
	if err := scanner.Scan(&edge.ID, &edge.WorkID, &edge.SourceNodeID, &edge.TargetNodeID, &edge.Kind, &createdAt); err != nil {
		return canvas.Edge{}, err
	}
	var err error
	edge.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return canvas.Edge{}, fmt.Errorf("parse canvas edge created time: %w", err)
	}
	return edge, nil
}

func (r *CanvasRepository) candidateByRun(ctx context.Context, runID string) (agent.Candidate, error) {
	return scanCandidate(r.database.QueryRowContext(ctx, `
SELECT c.id, c.run_id, c.work_id, c.skill_id, c.skill_version, c.status, c.kind, c.title,
       c.content, c.x, c.y, c.accepted_node_id, c.created_at, c.decided_at, r.context_node_ids_json,
       c.candidate_type, c.node_id, c.base_version_id, c.reason, c.change_score
FROM agent_candidates c
JOIN agent_runs r ON r.id = c.run_id
WHERE c.run_id = ?`, runID))
}

func (r *CanvasRepository) ListCandidates(ctx context.Context, workID string) ([]agent.Candidate, error) {
	rows, err := r.database.QueryContext(ctx, `
SELECT c.id, c.run_id, c.work_id, c.skill_id, c.skill_version, c.status, c.kind, c.title,
       c.content, c.x, c.y, c.accepted_node_id, c.created_at, c.decided_at, r.context_node_ids_json,
       c.candidate_type, c.node_id, c.base_version_id, c.reason, c.change_score
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
	if candidate.CandidateType == "version" {
		return r.acceptVersionCandidate(ctx, transaction, input, candidate)
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = candidate.Title
	}
	now := time.Now().UTC()
	node := canvas.Node{
		ID: uuid.NewString(), WorkID: candidate.WorkID, Revision: 1, Kind: canvas.NodeKind(candidate.Kind),
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
	if _, err := createInitialNodeVersion(ctx, transaction, node); err != nil {
		return canvas.Node{}, err
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

func (r *CanvasRepository) acceptVersionCandidate(ctx context.Context, transaction *sql.Tx, input canvas.AcceptCandidateInput, candidate agent.Candidate) (canvas.Node, error) {
	locked, err := isNodeArchiveLocked(ctx, transaction, candidate.WorkID, candidate.NodeID)
	if err != nil {
		return canvas.Node{}, err
	}
	if locked {
		return canvas.Node{}, canvas.ErrArchivedNodeLocked
	}
	var node canvas.Node
	var currentVersionID string
	node, err = scanCanvasNode(transaction.QueryRowContext(ctx, `SELECT id, work_id, revision, kind, title, content, x, y, created_at, updated_at FROM canvas_nodes WHERE work_id=? AND id=?`, candidate.WorkID, candidate.NodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return canvas.Node{}, canvas.ErrNodeNotFound
	}
	if err != nil {
		return canvas.Node{}, err
	}
	if err := transaction.QueryRowContext(ctx, `SELECT current_version_id FROM canvas_nodes WHERE work_id=? AND id=?`, candidate.WorkID, candidate.NodeID).Scan(&currentVersionID); err != nil {
		return canvas.Node{}, err
	}
	var next int64
	if err := transaction.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number),0)+1 FROM canvas_node_versions WHERE node_id=?`, candidate.NodeID).Scan(&next); err != nil {
		return canvas.Node{}, err
	}
	versionID := uuid.NewString()
	now := time.Now().UTC()
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = candidate.Title
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO canvas_node_versions (id,node_id,work_id,version_number,parent_version_id,title,content,source_run_id,created_at) VALUES (?,?,?,?,?,?,?,?,?)`, versionID, candidate.NodeID, candidate.WorkID, next, currentVersionID, title, candidate.Content, candidate.RunID, now.Format(time.RFC3339Nano)); err != nil {
		return canvas.Node{}, err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE canvas_nodes SET title=?,content=?,revision=revision+1,current_version_id=?,updated_at=? WHERE work_id=? AND id=?`, title, candidate.Content, versionID, now.Format(time.RFC3339Nano), candidate.WorkID, candidate.NodeID); err != nil {
		return canvas.Node{}, err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE agent_candidates SET status=?,title=?,accepted_node_id=?,decided_at=? WHERE work_id=? AND id=? AND status=?`, agent.CandidateStatusAccepted, title, candidate.NodeID, now.Format(time.RFC3339Nano), input.WorkID, input.CandidateID, agent.CandidateStatusPending); err != nil {
		return canvas.Node{}, err
	}
	if err := transaction.Commit(); err != nil {
		return canvas.Node{}, err
	}
	node.Title, node.Content, node.Revision, node.UpdatedAt, node.CurrentVersionID = title, candidate.Content, node.Revision+1, now, versionID
	return node, nil
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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

func (r *CanvasRepository) ListNodeVersions(ctx context.Context, workID, nodeID string) ([]canvas.NodeVersion, error) {
	rows, err := r.database.QueryContext(ctx, `SELECT id,node_id,work_id,version_number,parent_version_id,title,content,source_run_id,created_at FROM canvas_node_versions WHERE work_id=? AND node_id=? ORDER BY version_number DESC`, workID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list node versions: %w", err)
	}
	defer rows.Close()
	versions := make([]canvas.NodeVersion, 0)
	for rows.Next() {
		var v canvas.NodeVersion
		var created string
		if err := rows.Scan(&v.ID, &v.NodeID, &v.WorkID, &v.VersionNumber, &v.ParentVersionID, &v.Title, &v.Content, &v.SourceRunID, &created); err != nil {
			return nil, err
		}
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

func (r *CanvasRepository) SwitchNodeVersion(ctx context.Context, workID, nodeID, versionID string) (canvas.Node, error) {
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return canvas.Node{}, err
	}
	defer tx.Rollback()
	locked, err := isNodeArchiveLocked(ctx, tx, workID, nodeID)
	if err != nil {
		return canvas.Node{}, err
	}
	if locked {
		return canvas.Node{}, canvas.ErrArchivedNodeLocked
	}
	var title, content string
	if err := tx.QueryRowContext(ctx, `SELECT title,content FROM canvas_node_versions WHERE id=? AND work_id=? AND node_id=?`, versionID, workID, nodeID).Scan(&title, &content); errors.Is(err, sql.ErrNoRows) {
		return canvas.Node{}, canvas.ErrNodeNotFound
	} else if err != nil {
		return canvas.Node{}, err
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE canvas_nodes SET title=?,content=?,current_version_id=?,revision=revision+1,updated_at=? WHERE work_id=? AND id=?`, title, content, versionID, now.Format(time.RFC3339Nano), workID, nodeID); err != nil {
		return canvas.Node{}, err
	}
	node, err := scanCanvasNode(tx.QueryRowContext(ctx, `SELECT id,work_id,revision,kind,title,content,x,y,created_at,updated_at FROM canvas_nodes WHERE work_id=? AND id=?`, workID, nodeID))
	if err != nil {
		return canvas.Node{}, err
	}
	node.CurrentVersionID = versionID
	if err := tx.Commit(); err != nil {
		return canvas.Node{}, err
	}
	return node, nil
}

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"warmnote/core/internal/canvas"
)

const (
	canvasHistoryLimit = 20

	actionCreateNodes = "nodes.created"
	actionDeleteNodes = "nodes.deleted"
	actionMoveNodes   = "nodes.moved"
	actionUpdateNode  = "node.updated"
)

type createNodesActionPayload struct {
	Nodes []canvas.Node `json:"nodes"`
}

type deleteNodesActionPayload struct {
	Nodes []canvas.Node `json:"nodes"`
	Edges []canvas.Edge `json:"edges"`
}

type moveNodesActionPayload struct {
	Before []canvas.NodePosition `json:"before"`
	After  []canvas.NodePosition `json:"after"`
}

type nodeContentState struct {
	NodeID  string `json:"nodeId"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type updateNodeActionPayload struct {
	Before nodeContentState `json:"before"`
	After  nodeContentState `json:"after"`
}

type storedCanvasAction struct {
	ID          string
	Sequence    int64
	ActionType  string
	Label       string
	PayloadJSON string
}

func (r *CanvasRepository) UpdateNodePositions(
	ctx context.Context,
	workID string,
	positions []canvas.NodePosition,
) error {
	positions = uniqueNodePositions(positions)
	if len(positions) == 0 || len(positions) > 100 {
		return canvas.ErrInvalidNode
	}
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin update canvas node positions: %w", err)
	}
	defer transaction.Rollback()

	before := make([]canvas.NodePosition, 0, len(positions))
	after := make([]canvas.NodePosition, 0, len(positions))
	for _, position := range positions {
		var current canvas.NodePosition
		current.NodeID = position.NodeID
		err := transaction.QueryRowContext(ctx, `
SELECT x, y FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, position.NodeID).
			Scan(&current.X, &current.Y)
		if errors.Is(err, sql.ErrNoRows) {
			return canvas.ErrNodeNotFound
		}
		if err != nil {
			return fmt.Errorf("read canvas node position: %w", err)
		}
		if current.X == position.X && current.Y == position.Y {
			continue
		}
		before = append(before, current)
		after = append(after, position)
	}
	if len(after) == 0 {
		return nil
	}
	if err := applyNodePositions(ctx, transaction, workID, after); err != nil {
		return err
	}
	label := "移动节点"
	if len(after) > 1 {
		label = fmt.Sprintf("移动 %d 个节点", len(after))
	}
	if err := appendCanvasAction(ctx, transaction, workID, actionMoveNodes, label, moveNodesActionPayload{
		Before: before,
		After:  after,
	}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit canvas node positions: %w", err)
	}
	return nil
}

func (r *CanvasRepository) DeleteNodes(ctx context.Context, workID string, nodeIDs []string) error {
	nodeIDs = uniqueStrings(nodeIDs)
	if len(nodeIDs) == 0 || len(nodeIDs) > 100 {
		return canvas.ErrInvalidNode
	}
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete canvas nodes: %w", err)
	}
	defer transaction.Rollback()

	nodes := make([]canvas.Node, 0, len(nodeIDs))
	deletedNodeIDs := make(map[string]struct{}, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		node, err := scanCanvasNode(transaction.QueryRowContext(ctx, `
SELECT id, work_id, revision, kind, title, content, x, y, created_at, updated_at
FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, nodeID))
		if errors.Is(err, sql.ErrNoRows) {
			return canvas.ErrNodeNotFound
		}
		if err != nil {
			return fmt.Errorf("read deleted canvas node: %w", err)
		}
		nodes = append(nodes, node)
		deletedNodeIDs[nodeID] = struct{}{}
	}
	edges, err := listEdgesForHistory(ctx, transaction, workID, deletedNodeIDs)
	if err != nil {
		return err
	}
	for _, nodeID := range nodeIDs {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, nodeID); err != nil {
			return fmt.Errorf("delete canvas node: %w", err)
		}
	}
	label := "删除节点"
	if len(nodes) > 1 {
		label = fmt.Sprintf("删除 %d 个节点", len(nodes))
	}
	if err := appendCanvasAction(ctx, transaction, workID, actionDeleteNodes, label, deleteNodesActionPayload{
		Nodes: nodes,
		Edges: edges,
	}); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit delete canvas nodes: %w", err)
	}
	return nil
}

func (r *CanvasRepository) GetHistoryState(ctx context.Context, workID string) (canvas.HistoryState, error) {
	var currentSequence int64
	err := r.database.QueryRowContext(ctx, `
SELECT current_sequence FROM canvas_history_state WHERE work_id = ?`, workID).Scan(&currentSequence)
	if errors.Is(err, sql.ErrNoRows) {
		return canvas.HistoryState{}, nil
	}
	if err != nil {
		return canvas.HistoryState{}, fmt.Errorf("read canvas history state: %w", err)
	}
	return readHistoryState(ctx, r.database, workID, currentSequence)
}

func (r *CanvasRepository) Undo(ctx context.Context, workID string) (canvas.HistoryState, error) {
	return r.moveHistoryCursor(ctx, workID, false)
}

func (r *CanvasRepository) Redo(ctx context.Context, workID string) (canvas.HistoryState, error) {
	return r.moveHistoryCursor(ctx, workID, true)
}

func (r *CanvasRepository) moveHistoryCursor(
	ctx context.Context,
	workID string,
	forward bool,
) (canvas.HistoryState, error) {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return canvas.HistoryState{}, fmt.Errorf("begin move canvas history cursor: %w", err)
	}
	defer transaction.Rollback()

	currentSequence, err := readHistoryCursor(ctx, transaction, workID)
	if err != nil {
		return canvas.HistoryState{}, err
	}
	targetSequence := currentSequence
	if forward {
		targetSequence++
	}
	action, err := readCanvasAction(ctx, transaction, workID, targetSequence)
	if errors.Is(err, sql.ErrNoRows) {
		return canvas.HistoryState{}, canvas.ErrHistoryUnavailable
	}
	if err != nil {
		return canvas.HistoryState{}, err
	}
	if err := applyCanvasAction(ctx, transaction, workID, action, forward); err != nil {
		return canvas.HistoryState{}, err
	}
	newSequence := action.Sequence
	newActionID := action.ID
	if !forward {
		newSequence--
		newActionID = ""
		if newSequence > 0 {
			previousAction, previousErr := readCanvasAction(ctx, transaction, workID, newSequence)
			if previousErr == nil {
				newActionID = previousAction.ID
			} else if !errors.Is(previousErr, sql.ErrNoRows) {
				return canvas.HistoryState{}, previousErr
			}
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE canvas_history_state SET current_sequence = ?, current_action_id = ? WHERE work_id = ?`,
		newSequence, newActionID, workID); err != nil {
		return canvas.HistoryState{}, fmt.Errorf("update canvas history cursor: %w", err)
	}
	state, err := readHistoryState(ctx, transaction, workID, newSequence)
	if err != nil {
		return canvas.HistoryState{}, err
	}
	if err := transaction.Commit(); err != nil {
		return canvas.HistoryState{}, fmt.Errorf("commit canvas history cursor: %w", err)
	}
	return state, nil
}

func appendCanvasAction(
	ctx context.Context,
	transaction *sql.Tx,
	workID string,
	actionType string,
	label string,
	payload any,
) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode canvas action: %w", err)
	}
	currentSequence, err := readHistoryCursor(ctx, transaction, workID)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `
DELETE FROM canvas_actions WHERE work_id = ? AND sequence > ?`, workID, currentSequence); err != nil {
		return fmt.Errorf("clear canvas redo history: %w", err)
	}
	nextSequence := currentSequence + 1
	actionID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO canvas_actions (id, work_id, sequence, action_type, label, payload_json, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, actionID, workID, nextSequence, actionType, label, string(payloadJSON), now); err != nil {
		return fmt.Errorf("append canvas action: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO canvas_history_state (work_id, current_sequence, current_action_id)
VALUES (?, ?, ?)
ON CONFLICT(work_id) DO UPDATE SET current_sequence = excluded.current_sequence,
    current_action_id = excluded.current_action_id`, workID, nextSequence, actionID); err != nil {
		return fmt.Errorf("advance canvas history cursor: %w", err)
	}
	oldestSequence := nextSequence - canvasHistoryLimit + 1
	if oldestSequence > 1 {
		if _, err := transaction.ExecContext(ctx, `
DELETE FROM canvas_actions WHERE work_id = ? AND sequence < ?`, workID, oldestSequence); err != nil {
			return fmt.Errorf("prune canvas action history: %w", err)
		}
	}
	return nil
}

func readHistoryCursor(ctx context.Context, transaction *sql.Tx, workID string) (int64, error) {
	var currentSequence int64
	err := transaction.QueryRowContext(ctx, `
SELECT current_sequence FROM canvas_history_state WHERE work_id = ?`, workID).Scan(&currentSequence)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read canvas history cursor: %w", err)
	}
	return currentSequence, nil
}

type historyStateQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readHistoryState(
	ctx context.Context,
	querier historyStateQuerier,
	workID string,
	currentSequence int64,
) (canvas.HistoryState, error) {
	state := canvas.HistoryState{}
	err := querier.QueryRowContext(ctx, `
SELECT label FROM canvas_actions WHERE work_id = ? AND sequence = ?`, workID, currentSequence).
		Scan(&state.UndoLabel)
	if err == nil {
		state.CanUndo = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return canvas.HistoryState{}, fmt.Errorf("read canvas undo action: %w", err)
	}
	err = querier.QueryRowContext(ctx, `
SELECT label FROM canvas_actions WHERE work_id = ? AND sequence = ?`, workID, currentSequence+1).
		Scan(&state.RedoLabel)
	if err == nil {
		state.CanRedo = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return canvas.HistoryState{}, fmt.Errorf("read canvas redo action: %w", err)
	}
	return state, nil
}

func readCanvasAction(
	ctx context.Context,
	transaction *sql.Tx,
	workID string,
	sequence int64,
) (storedCanvasAction, error) {
	var action storedCanvasAction
	err := transaction.QueryRowContext(ctx, `
SELECT id, sequence, action_type, label, payload_json
FROM canvas_actions WHERE work_id = ? AND sequence = ?`, workID, sequence).
		Scan(&action.ID, &action.Sequence, &action.ActionType, &action.Label, &action.PayloadJSON)
	if err != nil {
		return storedCanvasAction{}, err
	}
	return action, nil
}

func applyCanvasAction(
	ctx context.Context,
	transaction *sql.Tx,
	workID string,
	action storedCanvasAction,
	forward bool,
) error {
	switch action.ActionType {
	case actionCreateNodes:
		var payload createNodesActionPayload
		if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode create nodes action: %w", err)
		}
		if forward {
			return restoreNodes(ctx, transaction, payload.Nodes)
		}
		return deleteNodeSnapshots(ctx, transaction, workID, payload.Nodes)
	case actionDeleteNodes:
		var payload deleteNodesActionPayload
		if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode delete nodes action: %w", err)
		}
		if forward {
			return deleteNodeSnapshots(ctx, transaction, workID, payload.Nodes)
		}
		if err := restoreNodes(ctx, transaction, payload.Nodes); err != nil {
			return err
		}
		return restoreEdges(ctx, transaction, payload.Edges)
	case actionMoveNodes:
		var payload moveNodesActionPayload
		if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode move nodes action: %w", err)
		}
		if forward {
			return applyNodePositions(ctx, transaction, workID, payload.After)
		}
		return applyNodePositions(ctx, transaction, workID, payload.Before)
	case actionUpdateNode:
		var payload updateNodeActionPayload
		if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode update node action: %w", err)
		}
		if forward {
			return applyNodeContent(ctx, transaction, workID, payload.After)
		}
		return applyNodeContent(ctx, transaction, workID, payload.Before)
	default:
		return fmt.Errorf("unsupported canvas action type %q", action.ActionType)
	}
}

func applyNodePositions(
	ctx context.Context,
	transaction *sql.Tx,
	workID string,
	positions []canvas.NodePosition,
) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, position := range positions {
		result, err := transaction.ExecContext(ctx, `
UPDATE canvas_nodes SET x = ?, y = ?, updated_at = ? WHERE work_id = ? AND id = ?`,
			position.X, position.Y, now, workID, position.NodeID)
		if err != nil {
			return fmt.Errorf("apply canvas node position: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read applied canvas node position: %w", err)
		}
		if changed == 0 {
			return canvas.ErrNodeNotFound
		}
	}
	return nil
}

func applyNodeContent(
	ctx context.Context,
	transaction *sql.Tx,
	workID string,
	state nodeContentState,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE canvas_nodes
SET title = ?, content = ?, revision = revision + 1, updated_at = ?
WHERE work_id = ? AND id = ?`, state.Title, state.Content,
		time.Now().UTC().Format(time.RFC3339Nano), workID, state.NodeID)
	if err != nil {
		return fmt.Errorf("apply canvas node content: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read applied canvas node content: %w", err)
	}
	if changed == 0 {
		return canvas.ErrNodeNotFound
	}
	return nil
}

func restoreNodes(ctx context.Context, transaction *sql.Tx, nodes []canvas.Node) error {
	for _, node := range nodes {
		_, err := transaction.ExecContext(ctx, `
INSERT INTO canvas_nodes (id, work_id, revision, kind, title, content, x, y, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, node.ID, node.WorkID, node.Revision, node.Kind,
			node.Title, node.Content, node.X, node.Y, node.CreatedAt.Format(time.RFC3339Nano),
			node.UpdatedAt.Format(time.RFC3339Nano))
		if err != nil {
			return fmt.Errorf("restore canvas node: %w", err)
		}
	}
	return nil
}

func restoreEdges(ctx context.Context, transaction *sql.Tx, edges []canvas.Edge) error {
	for _, edge := range edges {
		_, err := transaction.ExecContext(ctx, `
INSERT INTO canvas_edges (id, work_id, source_node_id, target_node_id, kind, created_at)
VALUES (?, ?, ?, ?, ?, ?)`, edge.ID, edge.WorkID, edge.SourceNodeID, edge.TargetNodeID,
			edge.Kind, edge.CreatedAt.Format(time.RFC3339Nano))
		if err != nil {
			return fmt.Errorf("restore canvas edge: %w", err)
		}
	}
	return nil
}

func deleteNodeSnapshots(
	ctx context.Context,
	transaction *sql.Tx,
	workID string,
	nodes []canvas.Node,
) error {
	for _, node := range nodes {
		result, err := transaction.ExecContext(ctx, `
DELETE FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, node.ID)
		if err != nil {
			return fmt.Errorf("reapply canvas node deletion: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read reapplied canvas node deletion: %w", err)
		}
		if changed == 0 {
			return canvas.ErrNodeNotFound
		}
	}
	return nil
}

func listEdgesForHistory(
	ctx context.Context,
	transaction *sql.Tx,
	workID string,
	nodeIDs map[string]struct{},
) ([]canvas.Edge, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT id, work_id, source_node_id, target_node_id, kind, created_at
FROM canvas_edges WHERE work_id = ? ORDER BY created_at`, workID)
	if err != nil {
		return nil, fmt.Errorf("list canvas edges for history: %w", err)
	}
	defer rows.Close()
	edges := make([]canvas.Edge, 0)
	for rows.Next() {
		var edge canvas.Edge
		var createdAt string
		if err := rows.Scan(&edge.ID, &edge.WorkID, &edge.SourceNodeID, &edge.TargetNodeID, &edge.Kind, &createdAt); err != nil {
			return nil, fmt.Errorf("scan canvas edge for history: %w", err)
		}
		if _, sourceDeleted := nodeIDs[edge.SourceNodeID]; !sourceDeleted {
			if _, targetDeleted := nodeIDs[edge.TargetNodeID]; !targetDeleted {
				continue
			}
		}
		edge.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse canvas edge history time: %w", err)
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate canvas edges for history: %w", err)
	}
	return edges, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func uniqueNodePositions(positions []canvas.NodePosition) []canvas.NodePosition {
	seen := make(map[string]int, len(positions))
	result := make([]canvas.NodePosition, 0, len(positions))
	for _, position := range positions {
		if position.NodeID == "" {
			continue
		}
		if index, exists := seen[position.NodeID]; exists {
			result[index] = position
			continue
		}
		seen[position.NodeID] = len(result)
		result = append(result, position)
	}
	return result
}

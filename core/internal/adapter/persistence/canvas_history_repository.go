package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"warmmo/core/internal/domain/canvas"
)

const (
	canvasHistoryLimit = 20

	actionCreateNodes = "nodes.created"
	actionDeleteNodes = "nodes.deleted"
	actionMoveNodes   = "nodes.moved"
	actionUpdateNode  = "node.updated"
	actionCreateEdge  = "edge.created"
	actionDeleteEdges = "edges.deleted"
)

type createNodesActionPayload struct {
	Nodes []canvas.Node `json:"nodes"`
	Edges []canvas.Edge `json:"edges,omitempty"`
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

type createEdgeActionPayload struct {
	Edge canvas.Edge `json:"edge"`
}

type deleteEdgesActionPayload struct {
	Edges []canvas.Edge `json:"edges"`
}

func (r *CanvasRepository) DeleteEdges(ctx context.Context, workID string, edgeIDs []string) error {
	edgeIDs = uniqueStrings(edgeIDs)
	if len(edgeIDs) == 0 || len(edgeIDs) > 100 {
		return canvas.ErrInvalidNode
	}
	return r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var models []canvasEdgeModel
		if err := tx.Where("work_id = ? AND id IN ?", workID, edgeIDs).Find(&models).Error; err != nil {
			return fmt.Errorf("read deleted canvas edges: %w", err)
		}
		if len(models) != len(edgeIDs) {
			return canvas.ErrNodeNotFound
		}
		for _, edge := range models {
			locked, err := isNodeArchiveLocked(tx, workID, edge.TargetNodeID)
			if err != nil {
				return err
			}
			if locked {
				return canvas.ErrArchivedNodeLocked
			}
		}
		if err := deleteEdgeSnapshots(tx, workID, edgeIDs); err != nil {
			return err
		}
		label := "删除连接"
		if len(models) > 1 {
			label = fmt.Sprintf("删除 %d 条连接", len(models))
		}
		return appendCanvasAction(tx, workID, actionDeleteEdges, label, deleteEdgesActionPayload{Edges: canvasEdgesFromModels(models)})
	})
}

type storedCanvasAction struct {
	ID          string
	Sequence    int64
	ActionType  string
	Label       string
	PayloadJSON string
}

func (r *CanvasRepository) UpdateNodePositions(ctx context.Context, workID string, positions []canvas.NodePosition) error {
	positions = uniqueNodePositions(positions)
	if len(positions) == 0 || len(positions) > 100 {
		return canvas.ErrInvalidNode
	}
	return r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids := make([]string, len(positions))
		for i, position := range positions {
			ids[i] = position.NodeID
		}
		var models []canvasNodeModel
		if err := tx.Select("id", "x", "y").Where("work_id = ? AND id IN ?", workID, ids).Find(&models).Error; err != nil {
			return fmt.Errorf("read canvas node positions: %w", err)
		}
		if len(models) != len(ids) {
			return canvas.ErrNodeNotFound
		}
		current := make(map[string]canvasNodeModel, len(models))
		for _, model := range models {
			current[model.ID] = model
		}
		before, after := make([]canvas.NodePosition, 0, len(positions)), make([]canvas.NodePosition, 0, len(positions))
		for _, position := range positions {
			model := current[position.NodeID]
			if model.X == position.X && model.Y == position.Y {
				continue
			}
			before = append(before, canvas.NodePosition{NodeID: model.ID, X: model.X, Y: model.Y})
			after = append(after, position)
		}
		if len(after) == 0 {
			return nil
		}
		if err := applyNodePositions(tx, workID, after); err != nil {
			return err
		}
		label := "移动节点"
		if len(after) > 1 {
			label = fmt.Sprintf("移动 %d 个节点", len(after))
		}
		return appendCanvasAction(tx, workID, actionMoveNodes, label, moveNodesActionPayload{Before: before, After: after})
	})
}

func (r *CanvasRepository) DeleteNodes(ctx context.Context, workID string, nodeIDs []string) error {
	nodeIDs = uniqueStrings(nodeIDs)
	if len(nodeIDs) == 0 || len(nodeIDs) > 100 {
		return canvas.ErrInvalidNode
	}
	return r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, nodeID := range nodeIDs {
			locked, err := isNodeArchiveLocked(tx, workID, nodeID)
			if err != nil {
				return err
			}
			if locked {
				return canvas.ErrArchivedNodeLocked
			}
		}
		var models []canvasNodeModel
		if err := tx.Where("work_id = ? AND id IN ?", workID, nodeIDs).Find(&models).Error; err != nil {
			return fmt.Errorf("read deleted canvas nodes: %w", err)
		}
		if len(models) != len(nodeIDs) {
			return canvas.ErrNodeNotFound
		}
		edges, err := listEdgesForHistory(tx, workID, nodeIDs)
		if err != nil {
			return err
		}
		if len(edges) > 0 {
			edgeIDs := make([]string, len(edges))
			for i, edge := range edges {
				edgeIDs[i] = edge.ID
			}
			if err := deleteEdgeSnapshots(tx, workID, edgeIDs); err != nil {
				return err
			}
		}
		result := tx.Where("work_id = ? AND id IN ?", workID, nodeIDs).Delete(&canvasNodeModel{})
		if result.Error != nil {
			return fmt.Errorf("delete canvas nodes: %w", result.Error)
		}
		if result.RowsAffected != int64(len(nodeIDs)) {
			return canvas.ErrNodeNotFound
		}
		label := "删除节点"
		if len(models) > 1 {
			label = fmt.Sprintf("删除 %d 个节点", len(models))
		}
		return appendCanvasAction(tx, workID, actionDeleteNodes, label, deleteNodesActionPayload{Nodes: canvasNodesFromModels(models), Edges: edges})
	})
}

func (r *CanvasRepository) GetHistoryState(ctx context.Context, workID string) (canvas.HistoryState, error) {
	current, err := readHistoryCursor(r.database.WithContext(ctx), workID)
	if err != nil {
		return canvas.HistoryState{}, err
	}
	return readHistoryState(r.database.WithContext(ctx), workID, current)
}

func (r *CanvasRepository) Undo(ctx context.Context, workID string) (canvas.HistoryState, error) {
	return r.moveHistoryCursor(ctx, workID, false)
}

func (r *CanvasRepository) Redo(ctx context.Context, workID string) (canvas.HistoryState, error) {
	return r.moveHistoryCursor(ctx, workID, true)
}

func (r *CanvasRepository) moveHistoryCursor(ctx context.Context, workID string, forward bool) (canvas.HistoryState, error) {
	var state canvas.HistoryState
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := readHistoryCursor(tx, workID)
		if err != nil {
			return err
		}
		target := current
		if forward {
			target++
		}
		action, err := readCanvasAction(tx, workID, target)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return canvas.ErrHistoryUnavailable
		}
		if err != nil {
			return err
		}
		if err := applyCanvasAction(tx, workID, action, forward); err != nil {
			return err
		}
		sequence, actionID := action.Sequence, action.ID
		if !forward {
			sequence--
			actionID = ""
			if sequence > 0 {
				previous, previousErr := readCanvasAction(tx, workID, sequence)
				if previousErr == nil {
					actionID = previous.ID
				} else if !errors.Is(previousErr, gorm.ErrRecordNotFound) {
					return previousErr
				}
			}
		}
		if err := tx.Model(&canvasHistoryStateModel{}).Where("work_id = ?", workID).Updates(map[string]any{"current_sequence": sequence, "current_action_id": actionID}).Error; err != nil {
			return fmt.Errorf("update canvas history cursor: %w", err)
		}
		state, err = readHistoryState(tx, workID, sequence)
		return err
	})
	return state, err
}

func appendCanvasAction(tx *gorm.DB, workID, actionType, label string, payload any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode canvas action: %w", err)
	}
	current, err := readHistoryCursor(tx, workID)
	if err != nil {
		return err
	}
	if err := tx.Where("work_id = ? AND sequence > ?", workID, current).Delete(&canvasActionModel{}).Error; err != nil {
		return fmt.Errorf("clear canvas redo history: %w", err)
	}
	next := current + 1
	action := canvasActionModel{ID: uuid.NewString(), WorkID: workID, Sequence: next, ActionType: actionType, Label: label, PayloadJSON: string(payloadJSON), CreatedAt: time.Now().UTC()}
	if err := tx.Create(&action).Error; err != nil {
		return fmt.Errorf("append canvas action: %w", err)
	}
	state := canvasHistoryStateModel{WorkID: workID, CurrentSequence: next, CurrentActionID: action.ID}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "work_id"}}, DoUpdates: clause.AssignmentColumns([]string{"current_sequence", "current_action_id"})}).Create(&state).Error; err != nil {
		return fmt.Errorf("advance canvas history cursor: %w", err)
	}
	if oldest := next - canvasHistoryLimit + 1; oldest > 1 {
		if err := tx.Where("work_id = ? AND sequence < ?", workID, oldest).Delete(&canvasActionModel{}).Error; err != nil {
			return fmt.Errorf("prune canvas action history: %w", err)
		}
	}
	return nil
}

func readHistoryCursor(db *gorm.DB, workID string) (int64, error) {
	var state canvasHistoryStateModel
	if err := db.Select("current_sequence").First(&state, "work_id = ?", workID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("read canvas history cursor: %w", err)
	}
	return state.CurrentSequence, nil
}

func readHistoryState(db *gorm.DB, workID string, current int64) (canvas.HistoryState, error) {
	var state canvas.HistoryState
	var undo, redo canvasActionModel
	if err := db.Select("label").Where("work_id = ? AND sequence = ?", workID, current).First(&undo).Error; err == nil {
		state.CanUndo, state.UndoLabel = true, undo.Label
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return state, fmt.Errorf("read canvas undo action: %w", err)
	}
	if err := db.Select("label").Where("work_id = ? AND sequence = ?", workID, current+1).First(&redo).Error; err == nil {
		state.CanRedo, state.RedoLabel = true, redo.Label
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return state, fmt.Errorf("read canvas redo action: %w", err)
	}
	return state, nil
}

func readCanvasAction(db *gorm.DB, workID string, sequence int64) (storedCanvasAction, error) {
	var model canvasActionModel
	if err := db.Where("work_id = ? AND sequence = ?", workID, sequence).First(&model).Error; err != nil {
		return storedCanvasAction{}, err
	}
	return storedCanvasAction{ID: model.ID, Sequence: model.Sequence, ActionType: model.ActionType, Label: model.Label, PayloadJSON: model.PayloadJSON}, nil
}

func applyCanvasAction(tx *gorm.DB, workID string, action storedCanvasAction, forward bool) error {
	switch action.ActionType {
	case actionCreateNodes:
		var payload createNodesActionPayload
		if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode create nodes action: %w", err)
		}
		if forward {
			if err := restoreNodes(tx, payload.Nodes); err != nil {
				return err
			}
			return restoreEdges(tx, payload.Edges)
		}
		if len(payload.Edges) > 0 {
			ids := make([]string, len(payload.Edges))
			for i, edge := range payload.Edges {
				ids[i] = edge.ID
			}
			if err := deleteEdgeSnapshots(tx, workID, ids); err != nil {
				return err
			}
		}
		return deleteNodeSnapshots(tx, workID, payload.Nodes)
	case actionDeleteNodes:
		var payload deleteNodesActionPayload
		if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode delete nodes action: %w", err)
		}
		if forward {
			if len(payload.Edges) > 0 {
				ids := make([]string, len(payload.Edges))
				for i, edge := range payload.Edges {
					ids[i] = edge.ID
				}
				if err := deleteEdgeSnapshots(tx, workID, ids); err != nil {
					return err
				}
			}
			return deleteNodeSnapshots(tx, workID, payload.Nodes)
		}
		if err := restoreNodes(tx, payload.Nodes); err != nil {
			return err
		}
		return restoreEdges(tx, payload.Edges)
	case actionMoveNodes:
		var payload moveNodesActionPayload
		if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode move nodes action: %w", err)
		}
		if forward {
			return applyNodePositions(tx, workID, payload.After)
		}
		return applyNodePositions(tx, workID, payload.Before)
	case actionUpdateNode:
		var payload updateNodeActionPayload
		if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode update node action: %w", err)
		}
		if forward {
			return applyNodeContent(tx, workID, payload.After)
		}
		return applyNodeContent(tx, workID, payload.Before)
	case actionCreateEdge:
		var payload createEdgeActionPayload
		if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode create edge action: %w", err)
		}
		if forward {
			return restoreEdges(tx, []canvas.Edge{payload.Edge})
		}
		return deleteEdgeSnapshots(tx, workID, []string{payload.Edge.ID})
	case actionDeleteEdges:
		var payload deleteEdgesActionPayload
		if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
			return fmt.Errorf("decode delete edges action: %w", err)
		}
		if forward {
			ids := make([]string, len(payload.Edges))
			for i, edge := range payload.Edges {
				ids[i] = edge.ID
			}
			return deleteEdgeSnapshots(tx, workID, ids)
		}
		return restoreEdges(tx, payload.Edges)
	default:
		return fmt.Errorf("unsupported canvas action type %q", action.ActionType)
	}
}

func deleteEdgeSnapshots(tx *gorm.DB, workID string, edgeIDs []string) error {
	result := tx.Where("work_id = ? AND id IN ?", workID, edgeIDs).Delete(&canvasEdgeModel{})
	if result.Error != nil {
		return fmt.Errorf("delete canvas edge snapshots: %w", result.Error)
	}
	if result.RowsAffected != int64(len(edgeIDs)) {
		return canvas.ErrNodeNotFound
	}
	return nil
}

func applyNodePositions(tx *gorm.DB, workID string, positions []canvas.NodePosition) error {
	now := time.Now().UTC()
	for _, position := range positions {
		result := tx.Model(&canvasNodeModel{}).Where("work_id = ? AND id = ?", workID, position.NodeID).Updates(map[string]any{"x": position.X, "y": position.Y, "updated_at": now})
		if result.Error != nil {
			return fmt.Errorf("apply canvas node position: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return canvas.ErrNodeNotFound
		}
	}
	return nil
}

func applyNodeContent(tx *gorm.DB, workID string, state nodeContentState) error {
	result := tx.Model(&canvasNodeModel{}).Where("work_id = ? AND id = ?", workID, state.NodeID).Updates(map[string]any{"title": state.Title, "content": state.Content, "revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return fmt.Errorf("apply canvas node content: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return canvas.ErrNodeNotFound
	}
	return nil
}

func restoreNodes(tx *gorm.DB, nodes []canvas.Node) error {
	models := make([]canvasNodeModel, len(nodes))
	for i, node := range nodes {
		models[i] = canvasNodeModel{ID: node.ID, WorkID: node.WorkID, Revision: node.Revision, Kind: string(node.Kind), Title: node.Title, Content: node.Content, X: node.X, Y: node.Y, CurrentVersionID: node.CurrentVersionID, CreatedAt: node.CreatedAt, UpdatedAt: node.UpdatedAt}
	}
	if len(models) > 0 {
		if err := tx.Create(&models).Error; err != nil {
			return fmt.Errorf("restore canvas nodes: %w", err)
		}
	}
	return nil
}

func restoreEdges(tx *gorm.DB, edges []canvas.Edge) error {
	models := make([]canvasEdgeModel, len(edges))
	for i, edge := range edges {
		models[i] = canvasEdgeModel{ID: edge.ID, WorkID: edge.WorkID, SourceNodeID: edge.SourceNodeID, TargetNodeID: edge.TargetNodeID, Kind: edge.Kind, CreatedAt: edge.CreatedAt}
	}
	if len(models) > 0 {
		if err := tx.Create(&models).Error; err != nil {
			return fmt.Errorf("restore canvas edges: %w", err)
		}
	}
	return nil
}

func deleteNodeSnapshots(tx *gorm.DB, workID string, nodes []canvas.Node) error {
	ids := make([]string, len(nodes))
	for i, node := range nodes {
		ids[i] = node.ID
	}
	result := tx.Where("work_id = ? AND id IN ?", workID, ids).Delete(&canvasNodeModel{})
	if result.Error != nil {
		return fmt.Errorf("reapply canvas node deletion: %w", result.Error)
	}
	if result.RowsAffected != int64(len(ids)) {
		return canvas.ErrNodeNotFound
	}
	return nil
}

func listEdgesForHistory(tx *gorm.DB, workID string, nodeIDs []string) ([]canvas.Edge, error) {
	var models []canvasEdgeModel
	if err := tx.Where("work_id = ? AND (source_node_id IN ? OR target_node_id IN ?)", workID, nodeIDs, nodeIDs).Order("created_at").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list canvas edges for history: %w", err)
	}
	return canvasEdgesFromModels(models), nil
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

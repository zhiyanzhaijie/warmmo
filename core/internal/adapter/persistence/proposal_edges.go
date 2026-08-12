package persistence

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appagent "warmmo/core/internal/application/agent"
)

const (
	proposalEdgePending      = "pending"
	proposalEdgeMaterialized = "materialized"
	proposalEdgeCancelled    = "cancelled"
)

// persistProposalEdges stores the proposal graph without putting client IDs in
// canvas domain fields. At least one endpoint must be a proposed node so that
// review decisions have a clear lifecycle for the relationship.
func persistProposalEdges(tx *gorm.DB, run appagent.Run, proposal appagent.ProposalSet, clientCandidates map[string]string, now time.Time) error {
	if len(proposal.Edges) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(proposal.Edges))
	models := make([]agentProposalEdgeModel, 0, len(proposal.Edges))
	for index, edge := range proposal.Edges {
		sourceID := strings.TrimSpace(edge.SourceID)
		targetID := strings.TrimSpace(edge.TargetID)
		kind := strings.TrimSpace(edge.Kind)
		if sourceID == targetID {
			return fmt.Errorf("%w: proposal edge %d cannot connect an endpoint to itself", appagent.ErrProjectionTerminal, index)
		}
		if kind != "generated_from" {
			return fmt.Errorf("%w: proposal edge %d has unsupported kind %q", appagent.ErrProjectionTerminal, index, kind)
		}
		sourceCandidateID, sourceNodeID, sourceIsCandidate, err := proposalEndpoint(tx, run.WorkID, sourceID, clientCandidates)
		if err != nil {
			return fmt.Errorf("%w: proposal edge %d source %q does not reference a proposed or existing node", appagent.ErrProjectionTerminal, index, sourceID)
		}
		targetCandidateID, targetNodeID, targetIsCandidate, err := proposalEndpoint(tx, run.WorkID, targetID, clientCandidates)
		if err != nil {
			return fmt.Errorf("%w: proposal edge %d target %q does not reference a proposed or existing node", appagent.ErrProjectionTerminal, index, targetID)
		}
		if !sourceIsCandidate && !targetIsCandidate {
			return fmt.Errorf("%w: proposal edge %d must reference at least one proposed node", appagent.ErrProjectionTerminal, index)
		}
		key := sourceCandidateID + "|" + sourceNodeID + "|" + targetCandidateID + "|" + targetNodeID + "|" + kind
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: proposal edge %d duplicates an earlier edge", appagent.ErrProjectionTerminal, index)
		}
		seen[key] = struct{}{}
		models = append(models, agentProposalEdgeModel{
			ID: uuid.NewString(), RunID: run.ID, WorkID: run.WorkID,
			SourceCandidateID: sourceCandidateID, SourceNodeID: sourceNodeID,
			TargetCandidateID: targetCandidateID, TargetNodeID: targetNodeID,
			Kind: kind, Status: proposalEdgePending, CreatedAt: now,
		})
	}
	if err := tx.Create(&models).Error; err != nil {
		return fmt.Errorf("persist proposal edges: %w", err)
	}
	return nil
}

func proposalEndpoint(tx *gorm.DB, workID, endpoint string, clientCandidates map[string]string) (candidateID, nodeID string, isCandidate bool, err error) {
	if candidateID, ok := clientCandidates[endpoint]; ok {
		return candidateID, "", true, nil
	}
	if _, err := getCanvasNode(tx, workID, endpoint); err != nil {
		return "", "", false, err
	}
	return "", endpoint, false, nil
}

// resolveProposalEdges advances relationships after a candidate becomes a
// real node. Edges are materialized only when both endpoints are durable nodes.
func resolveProposalEdges(tx *gorm.DB, workID, candidateID, nodeID string, now time.Time) error {
	if candidateID == "" || nodeID == "" {
		return nil
	}
	if result := tx.Model(&agentProposalEdgeModel{}).Where("work_id = ? AND status = ? AND source_candidate_id = ?", workID, proposalEdgePending, candidateID).Updates(map[string]any{"source_node_id": nodeID}); result.Error != nil {
		return fmt.Errorf("resolve proposal source edges: %w", result.Error)
	}
	if result := tx.Model(&agentProposalEdgeModel{}).Where("work_id = ? AND status = ? AND target_candidate_id = ?", workID, proposalEdgePending, candidateID).Updates(map[string]any{"target_node_id": nodeID}); result.Error != nil {
		return fmt.Errorf("resolve proposal target edges: %w", result.Error)
	}
	var edges []agentProposalEdgeModel
	if err := tx.Where("work_id = ? AND status = ? AND source_node_id <> '' AND target_node_id <> ''", workID, proposalEdgePending).Find(&edges).Error; err != nil {
		return fmt.Errorf("list resolved proposal edges: %w", err)
	}
	for _, edge := range edges {
		canvasEdge := canvasEdgeModel{ID: uuid.NewString(), WorkID: workID, SourceNodeID: edge.SourceNodeID, TargetNodeID: edge.TargetNodeID, Kind: edge.Kind, CreatedAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&canvasEdge).Error; err != nil {
			return fmt.Errorf("materialize proposal edge: %w", err)
		}
		if err := tx.Model(&agentProposalEdgeModel{}).Where("id = ? AND status = ?", edge.ID, proposalEdgePending).Updates(map[string]any{"status": proposalEdgeMaterialized, "resolved_at": now}).Error; err != nil {
			return fmt.Errorf("mark proposal edge materialized: %w", err)
		}
	}
	return nil
}

func cancelProposalEdges(tx *gorm.DB, workID, candidateID string, now time.Time) error {
	if err := tx.Model(&agentProposalEdgeModel{}).
		Where("work_id = ? AND status = ? AND (source_candidate_id = ? OR target_candidate_id = ?)", workID, proposalEdgePending, candidateID, candidateID).
		Updates(map[string]any{"status": proposalEdgeCancelled, "resolved_at": now}).Error; err != nil {
		return fmt.Errorf("cancel proposal edges: %w", err)
	}
	return nil
}

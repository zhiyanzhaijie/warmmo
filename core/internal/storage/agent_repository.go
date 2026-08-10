package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	agenttools "warmmo/core/internal/agent/tools"
	warmagent "warmmo/core/internal/agent/writing"
	"warmmo/core/internal/domain/canvas"
	"warmmo/core/internal/shared/safepath"
)

type AgentRepository struct {
	database      *sql.DB
	dataDirectory string
}

func (r *AgentRepository) GetRunByCandidate(candidateID, workID string) (warmagent.Run, warmagent.Candidate, error) {
	var run warmagent.Run
	var candidate warmagent.Candidate
	var targetNodeID, contextJSON, candidateCreatedAt, candidateDecidedAt, runCreatedAt, runUpdatedAt string
	err := r.database.QueryRow(`
SELECT c.id,c.run_id,c.work_id,c.skill_id,c.skill_version,c.status,c.kind,c.title,c.content,c.x,c.y,c.accepted_node_id,c.created_at,c.decided_at,
       r.id,r.work_id,r.status,r.prompt,r.target,r.target_node_id,r.provider_id,r.model_id,r.context_node_ids_json,r.created_at,r.updated_at
FROM agent_candidates c JOIN agent_runs r ON r.id=c.run_id
WHERE c.id=? AND c.work_id=?`, candidateID, workID).Scan(
		&candidate.ID, &candidate.RunID, &candidate.WorkID, &candidate.SkillID, &candidate.SkillVersion,
		&candidate.Status, &candidate.Kind, &candidate.Title, &candidate.Content, &candidate.X, &candidate.Y,
		&candidate.AcceptedNodeID, &candidateCreatedAt, &candidateDecidedAt,
		&run.ID, &run.WorkID, &run.Status, &run.Prompt, &run.Target, &targetNodeID, &run.ProviderID, &run.ModelID, &contextJSON, &runCreatedAt, &runUpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return warmagent.Run{}, warmagent.Candidate{}, canvas.ErrCandidateNotFound
	}
	if err != nil {
		return warmagent.Run{}, warmagent.Candidate{}, fmt.Errorf("read candidate run: %w", err)
	}
	run.TargetNodeID = targetNodeID
	if err := json.Unmarshal([]byte(contextJSON), &run.ContextNodeIDs); err != nil {
		return warmagent.Run{}, warmagent.Candidate{}, fmt.Errorf("decode candidate context: %w", err)
	}
	return run, candidate, nil
}

func (r *AgentRepository) ListCollaborativeCandidates(runID string) ([]warmagent.CollaborativeCandidate, error) {
	rows, err := r.database.Query(`
SELECT id,status,kind,title,accepted_node_id
FROM agent_candidates
WHERE run_id=?
ORDER BY created_at,id`, runID)
	if err != nil {
		return nil, fmt.Errorf("list collaborative candidates: %w", err)
	}
	defer rows.Close()
	candidates := make([]warmagent.CollaborativeCandidate, 0)
	for rows.Next() {
		var candidate warmagent.CollaborativeCandidate
		if err := rows.Scan(&candidate.CandidateID, &candidate.Status, &candidate.Kind, &candidate.Title, &candidate.AcceptedNodeID); err != nil {
			return nil, fmt.Errorf("scan collaborative candidate: %w", err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate collaborative candidates: %w", err)
	}
	return candidates, nil
}

func (r *AgentRepository) RequeueAfterCandidateDecision(runID, candidateID string, accepted bool, acceptedNodeID string) (bool, error) {
	tx, err := r.database.Begin()
	if err != nil {
		return false, fmt.Errorf("begin candidate decision: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	var batchStartedAt string
	if err := tx.QueryRow(`SELECT COALESCE(MAX(created_at),'') FROM agent_run_events WHERE run_id=? AND type=?`, runID, warmagent.EventRunResumed).Scan(&batchStartedAt); err != nil {
		return false, fmt.Errorf("read candidate batch start: %w", err)
	}
	var pending, rejected int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM agent_candidates WHERE run_id=? AND created_at>? AND status=?`, runID, batchStartedAt, warmagent.CandidateStatusPending).Scan(&pending); err != nil {
		return false, fmt.Errorf("count pending candidate decisions: %w", err)
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM agent_candidates WHERE run_id=? AND created_at>? AND status=?`, runID, batchStartedAt, warmagent.CandidateStatusRejected).Scan(&rejected); err != nil {
		return false, fmt.Errorf("count rejected candidate decisions: %w", err)
	}
	// A decision closes the current one-node generation. Continue after either
	// outcome so the model can decide whether the requested work is complete;
	// rejected candidates are included in the next prompt for correction.
	requeued := pending == 0
	if requeued {
		result, err := tx.Exec(`UPDATE agent_runs SET status=?,error_message='',updated_at=? WHERE id=? AND status=?`, warmagent.RunStatusQueued, now.Format(time.RFC3339Nano), runID, warmagent.RunStatusCompleted)
		if err != nil {
			return false, fmt.Errorf("requeue agent run: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed == 0 {
			return false, warmagent.ErrRunNotCancellable
		}
	}
	if _, err := appendEvent(tx, runID, warmagent.EventCandidateDecision, map[string]any{
		"candidateId": candidateID, "accepted": accepted, "acceptedNodeId": acceptedNodeID,
		"pending": pending, "rejected": rejected,
	}, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return requeued, nil
}

func (r *AgentRepository) RequestCandidateDecisionReason(runID, candidateID, title string) error {
	tx, err := r.database.Begin()
	if err != nil {
		return fmt.Errorf("begin candidate rejection question: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	result, err := tx.Exec(`UPDATE agent_runs SET status=?,error_message='',updated_at=? WHERE id=? AND status=?`,
		warmagent.RunStatusWaitingInput, now.Format(time.RFC3339Nano), runID, warmagent.RunStatusCompleted)
	if err != nil {
		return fmt.Errorf("wait for candidate rejection reason: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return warmagent.ErrRunNotCancellable
	}
	if _, err := appendEvent(tx, runID, warmagent.EventCandidateDecision, map[string]any{
		"candidateId": candidateID, "accepted": false, "awaitingReason": true,
	}, now); err != nil {
		return err
	}
	if _, err := appendEvent(tx, runID, warmagent.EventApprovalRequired, map[string]any{
		"candidateId": candidateID,
		"question":    fmt.Sprintf("你拒绝了候选节点“%s”。请告诉我拒绝原因，我会据此重新生成。", title),
		"reason":      "candidate_rejected",
	}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func NewAgentRepository(providerRepository *ProviderRepository) *AgentRepository {
	return &AgentRepository{
		database:      providerRepository.database,
		dataDirectory: filepath.Dir(providerRepository.databasePath),
	}
}

func (r *AgentRepository) GetCanvasNodeMetadata(workID, nodeID string) (canvas.NodeKind, int64, error) {
	var kind canvas.NodeKind
	var revision int64
	err := r.database.QueryRow(`SELECT kind, revision FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, nodeID).Scan(&kind, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, canvas.ErrNodeNotFound
	}
	if err != nil {
		return "", 0, fmt.Errorf("read target canvas node metadata: %w", err)
	}
	return kind, revision, nil
}

func (r *AgentRepository) GetNodeAttachments(workID, targetNodeID string) ([]warmagent.NodeReference, error) {
	rows, err := r.database.Query(`
SELECT source.id, source.kind
FROM canvas_edges edge
JOIN canvas_nodes source ON source.work_id = edge.work_id AND source.id = edge.source_node_id
WHERE edge.work_id = ? AND edge.target_node_id = ? AND edge.kind = 'generated_from'
ORDER BY edge.created_at, edge.source_node_id`, workID, targetNodeID)
	if err != nil {
		return nil, fmt.Errorf("read node attachments: %w", err)
	}
	defer rows.Close()

	attachments := make([]warmagent.NodeReference, 0)
	for rows.Next() {
		var attachment warmagent.NodeReference
		if err := rows.Scan(&attachment.ID, &attachment.Type); err != nil {
			return nil, fmt.Errorf("scan node attachment: %w", err)
		}
		attachments = append(attachments, attachment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node attachments: %w", err)
	}
	return attachments, nil
}

func (r *AgentRepository) GetNodeReferences(workID string, nodeIDs []string) ([]warmagent.NodeReference, error) {
	nodeIDs = uniqueStrings(nodeIDs)
	if len(nodeIDs) == 0 {
		return []warmagent.NodeReference{}, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(nodeIDs)), ",")
	arguments := make([]any, 0, len(nodeIDs)+1)
	arguments = append(arguments, workID)
	for _, nodeID := range nodeIDs {
		arguments = append(arguments, nodeID)
	}
	rows, err := r.database.Query(`
SELECT id, kind
FROM canvas_nodes
WHERE work_id = ? AND id IN (`+placeholders+`)`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("read canvas node references: %w", err)
	}
	defer rows.Close()

	referencesByID := make(map[string]warmagent.NodeReference, len(nodeIDs))
	for rows.Next() {
		var reference warmagent.NodeReference
		if err := rows.Scan(&reference.ID, &reference.Type); err != nil {
			return nil, fmt.Errorf("scan canvas node reference: %w", err)
		}
		referencesByID[reference.ID] = reference
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate canvas node references: %w", err)
	}
	references := make([]warmagent.NodeReference, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		reference, exists := referencesByID[nodeID]
		if !exists {
			return nil, fmt.Errorf("%w: %s", canvas.ErrNodeNotFound, nodeID)
		}
		references = append(references, reference)
	}
	return references, nil
}

// GetGlobalContextNodeReferences returns the current worldview nodes that are
// authoritative for every collaborative creation. The agent receives only
// routing metadata and must read content through canvas.get_nodes.
func (r *AgentRepository) GetGlobalContextNodeReferences(workID string) ([]warmagent.NodeReference, error) {
	rows, err := r.database.Query(`
SELECT id, kind
FROM canvas_nodes
WHERE work_id = ? AND kind IN (?, ?, ?)
ORDER BY CASE kind
    WHEN ? THEN 1
    WHEN ? THEN 2
    WHEN ? THEN 3
    ELSE 4
END, created_at, id`, workID,
		canvas.NodeKindWorld, canvas.NodeKindMechanism, canvas.NodeKindEvent,
		canvas.NodeKindWorld, canvas.NodeKindMechanism, canvas.NodeKindEvent)
	if err != nil {
		return nil, fmt.Errorf("read global context node references: %w", err)
	}
	defer rows.Close()
	global := make([]warmagent.NodeReference, 0)
	for rows.Next() {
		var reference warmagent.NodeReference
		if err := rows.Scan(&reference.ID, &reference.Type); err != nil {
			return nil, fmt.Errorf("scan global context node reference: %w", err)
		}
		global = append(global, reference)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate global context node references: %w", err)
	}
	return global, nil
}

func (r *AgentRepository) SearchStorySpineDatabase(ctx context.Context, workID, query string, limit int) ([]agenttools.StorySpineSearchResult, error) {
	statement := `SELECT archive.id,archive.chapter_outline_node_id,archive.outline_title,archive.summary || COALESCE((SELECT '\n' || GROUP_CONCAT(section.summary, '\n') FROM chapter_archive_sections section WHERE section.archive_id=archive.id),'') FROM chapter_archives archive WHERE archive.work_id=? AND archive.is_current=1`
	arguments := []any{workID}
	if query != "" {
		pattern := "%" + query + "%"
		statement += ` AND (archive.outline_title LIKE ? OR archive.summary LIKE ? OR archive.outline_content LIKE ? OR EXISTS (SELECT 1 FROM chapter_archive_sections section WHERE section.archive_id=archive.id AND section.summary LIKE ?))`
		arguments = append(arguments, pattern, pattern, pattern, pattern)
	}
	statement += ` ORDER BY archive.created_at DESC,archive.revision DESC,archive.id DESC LIMIT ?`
	arguments = append(arguments, limit)
	rows, err := r.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("search story spine database: %w", err)
	}
	results := make([]agenttools.StorySpineSearchResult, 0, limit)
	for rows.Next() {
		var result agenttools.StorySpineSearchResult
		var summary string
		if err := rows.Scan(&result.ArchiveID, &result.ChapterOutlineNodeID, &result.Title, &summary); err != nil {
			return nil, err
		}
		result.Snippet = truncateStorySpineDatabaseContext(summary, 1200)
		result.Source = "database"
		result.ContextRole = "completed-chapter"
		result.RecencyRank = len(results) + 1
		results = append(results, result)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(results) == 0 && query != "" {
		return r.SearchStorySpineDatabase(ctx, workID, "", limit)
	}
	return results, nil
}

func truncateStorySpineDatabaseContext(content string, limit int) string {
	content = strings.TrimSpace(content)
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

func (r *AgentRepository) GetChapterSectionContext(workID, sectionOutlineNodeID string) ([]string, error) {
	contextNodeIDs := []string{sectionOutlineNodeID}
	rows, err := r.database.Query(`
SELECT e.source_node_id
FROM canvas_edges e
JOIN canvas_nodes source ON source.work_id = e.work_id AND source.id = e.source_node_id
WHERE e.work_id = ? AND e.target_node_id = ? AND e.kind = 'generated_from'
  AND source.kind = ?
ORDER BY e.created_at`, workID, sectionOutlineNodeID, canvas.NodeKindChapterOutline)
	if err != nil {
		return nil, fmt.Errorf("read section outline chapter context: %w", err)
	}
	chapterOutlineNodeIDs := make([]string, 0, 1)
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan section outline chapter context: %w", err)
		}
		chapterOutlineNodeIDs = append(chapterOutlineNodeIDs, nodeID)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close section outline chapter context: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate section outline chapter context: %w", err)
	}
	if len(chapterOutlineNodeIDs) == 0 {
		return nil, fmt.Errorf("%w: section outline has no chapter outline parent", canvas.ErrInvalidNode)
	}
	contextNodeIDs = append(contextNodeIDs, chapterOutlineNodeIDs...)
	for _, chapterOutlineNodeID := range chapterOutlineNodeIDs {
		chapterContextRows, err := r.database.Query(`
SELECT source_node_id
FROM canvas_edges
WHERE work_id = ? AND target_node_id = ? AND kind = 'generated_from'
ORDER BY created_at`, workID, chapterOutlineNodeID)
		if err != nil {
			return nil, fmt.Errorf("read chapter outline inherited context: %w", err)
		}
		for chapterContextRows.Next() {
			var nodeID string
			if err := chapterContextRows.Scan(&nodeID); err != nil {
				chapterContextRows.Close()
				return nil, fmt.Errorf("scan chapter outline inherited context: %w", err)
			}
			contextNodeIDs = append(contextNodeIDs, nodeID)
		}
		if err := chapterContextRows.Close(); err != nil {
			return nil, fmt.Errorf("close chapter outline inherited context: %w", err)
		}
		if err := chapterContextRows.Err(); err != nil {
			return nil, fmt.Errorf("iterate chapter outline inherited context: %w", err)
		}
	}
	return uniqueStrings(contextNodeIDs), nil
}

func (r *AgentRepository) GetChapterArchiveContext(workID, chapterOutlineNodeID string) ([]string, error) {
	contextNodeIDs := []string{chapterOutlineNodeID}
	rows, err := r.database.Query(`
SELECT source_node_id
FROM canvas_edges
WHERE work_id=? AND target_node_id=? AND kind='generated_from'
ORDER BY created_at`, workID, chapterOutlineNodeID)
	if err != nil {
		return nil, fmt.Errorf("read chapter archive context: %w", err)
	}
	inheritedNodeIDs := make([]string, 0)
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			rows.Close()
			return nil, err
		}
		inheritedNodeIDs = append(inheritedNodeIDs, nodeID)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	contextNodeIDs = append(contextNodeIDs, inheritedNodeIDs...)

	sectionRows, err := r.database.Query(`
SELECT n.id, n.kind
FROM canvas_edges e
JOIN canvas_nodes n ON n.work_id=e.work_id AND n.id=e.target_node_id
WHERE e.work_id=? AND e.source_node_id=? AND e.kind='generated_from'
  AND n.kind IN (?, ?)
ORDER BY n.created_at`, workID, chapterOutlineNodeID, canvas.NodeKindSectionOutline, canvas.NodeKindChapterSection)
	if err != nil {
		return nil, fmt.Errorf("read archived chapter children: %w", err)
	}
	sectionOutlineNodeIDs := make([]string, 0)
	for sectionRows.Next() {
		var nodeID string
		var kind canvas.NodeKind
		if err := sectionRows.Scan(&nodeID, &kind); err != nil {
			sectionRows.Close()
			return nil, err
		}
		contextNodeIDs = append(contextNodeIDs, nodeID)
		if kind == canvas.NodeKindSectionOutline {
			sectionOutlineNodeIDs = append(sectionOutlineNodeIDs, nodeID)
		}
	}
	if err := sectionRows.Close(); err != nil {
		return nil, err
	}
	if err := sectionRows.Err(); err != nil {
		return nil, err
	}

	for _, sectionOutlineNodeID := range sectionOutlineNodeIDs {
		chapterSectionRows, err := r.database.Query(`
SELECT n.id
FROM canvas_edges e
JOIN canvas_nodes n ON n.work_id=e.work_id AND n.id=e.target_node_id
WHERE e.work_id=? AND e.source_node_id=? AND e.kind='generated_from' AND n.kind=?
ORDER BY n.created_at`, workID, sectionOutlineNodeID, canvas.NodeKindChapterSection)
		if err != nil {
			return nil, fmt.Errorf("read archived chapter sections: %w", err)
		}
		for chapterSectionRows.Next() {
			var nodeID string
			if err := chapterSectionRows.Scan(&nodeID); err != nil {
				chapterSectionRows.Close()
				return nil, err
			}
			contextNodeIDs = append(contextNodeIDs, nodeID)
		}
		if err := chapterSectionRows.Close(); err != nil {
			return nil, err
		}
		if err := chapterSectionRows.Err(); err != nil {
			return nil, err
		}
	}
	return uniqueStrings(contextNodeIDs), nil
}

func (r *AgentRepository) CreateRun(input warmagent.RunInput) (warmagent.Run, error) {
	contextNodeIDs, err := json.Marshal(input.ContextNodeIDs)
	if err != nil {
		return warmagent.Run{}, fmt.Errorf("encode context node ids: %w", err)
	}
	now := time.Now().UTC()
	run := warmagent.Run{
		ID: input.RunID, WorkID: input.WorkID, Status: warmagent.RunStatusQueued,
		Prompt: input.Prompt, Target: input.Target, TargetNodeID: input.TargetNodeID, ProviderID: input.ProviderID, ModelID: input.ModelID,
		ContextNodeIDs: append([]string(nil), input.ContextNodeIDs...), CreatedAt: now, UpdatedAt: now,
	}
	transaction, err := r.database.Begin()
	if err != nil {
		return warmagent.Run{}, fmt.Errorf("begin create agent run: %w", err)
	}
	defer transaction.Rollback()
	_, err = transaction.Exec(`
INSERT INTO agent_runs (
    id, work_id, status, prompt, target, target_node_id, provider_id, model_id, context_node_ids_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.WorkID, run.Status, run.Prompt, run.Target, run.TargetNodeID, run.ProviderID, run.ModelID,
		string(contextNodeIDs), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return warmagent.Run{}, fmt.Errorf("insert agent run: %w", err)
	}
	if _, err := appendEvent(transaction, run.ID, warmagent.EventRunQueued, map[string]any{"status": run.Status}, now); err != nil {
		return warmagent.Run{}, err
	}
	if err := transaction.Commit(); err != nil {
		return warmagent.Run{}, fmt.Errorf("commit agent run: %w", err)
	}
	return run, nil
}

func (r *AgentRepository) GetRun(runID string) (warmagent.Run, error) {
	return scanRun(r.database.QueryRow(`
SELECT id, work_id, status, prompt, target, target_node_id, provider_id, model_id, context_node_ids_json,
       error_message, created_at, updated_at
FROM agent_runs WHERE id = ?`, runID))
}

func (r *AgentRepository) ListEvents(runID string, afterSequence int64) ([]warmagent.Event, error) {
	rows, err := r.database.Query(`
SELECT id, run_id, sequence, type, data_json, created_at
FROM agent_run_events
WHERE run_id = ? AND sequence > ?
ORDER BY sequence`, runID, afterSequence)
	if err != nil {
		return nil, fmt.Errorf("query agent events: %w", err)
	}
	defer rows.Close()
	events := make([]warmagent.Event, 0)
	for rows.Next() {
		var event warmagent.Event
		var dataJSON, createdAt string
		if err := rows.Scan(&event.ID, &event.RunID, &event.Sequence, &event.Type, &dataJSON, &createdAt); err != nil {
			return nil, fmt.Errorf("scan agent event: %w", err)
		}
		event.Data = json.RawMessage(dataJSON)
		event.Timestamp, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse agent event time: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent events: %w", err)
	}
	return events, nil
}

func (r *AgentRepository) AppendEvent(runID string, eventType warmagent.EventType, data any) (warmagent.Event, error) {
	transaction, err := r.database.Begin()
	if err != nil {
		return warmagent.Event{}, fmt.Errorf("begin append agent event: %w", err)
	}
	defer transaction.Rollback()
	event, err := appendEvent(transaction, runID, eventType, data, time.Now().UTC())
	if err != nil {
		return warmagent.Event{}, err
	}
	if eventType == warmagent.EventApprovalRequired {
		result, err := transaction.Exec(`
UPDATE agent_runs SET status = ?, error_message = '', updated_at = ?
WHERE id = ? AND status = ?`, warmagent.RunStatusWaitingInput, event.Timestamp.Format(time.RFC3339Nano), runID, warmagent.RunStatusRunning)
		if err != nil {
			return warmagent.Event{}, fmt.Errorf("wait for agent input: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return warmagent.Event{}, fmt.Errorf("read waiting agent run: %w", err)
		}
		if changed == 0 {
			return warmagent.Event{}, warmagent.ErrRunNotCancellable
		}
	}
	if err := transaction.Commit(); err != nil {
		return warmagent.Event{}, fmt.Errorf("commit agent event: %w", err)
	}
	return event, nil
}

func (r *AgentRepository) MarkStarted(runID string) error {
	transaction, err := r.database.Begin()
	if err != nil {
		return fmt.Errorf("begin start agent run: %w", err)
	}
	defer transaction.Rollback()
	now := time.Now().UTC()
	result, err := transaction.Exec(`
UPDATE agent_runs SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		warmagent.RunStatusRunning, now.Format(time.RFC3339Nano), runID, warmagent.RunStatusQueued)
	if err != nil {
		return fmt.Errorf("start agent run: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read started agent run: %w", err)
	}
	if changed == 0 {
		return warmagent.ErrRunNotCancellable
	}
	if _, err := appendEvent(transaction, runID, warmagent.EventRunStarted, nil, now); err != nil {
		return err
	}
	return transaction.Commit()
}

func (r *AgentRepository) QueueResponse(runID, approvalEventID, answer string) (warmagent.UserResponse, error) {
	transaction, err := r.database.Begin()
	if err != nil {
		return warmagent.UserResponse{}, fmt.Errorf("begin queue agent response: %w", err)
	}
	defer transaction.Rollback()
	var status warmagent.RunStatus
	if err := transaction.QueryRow(`SELECT status FROM agent_runs WHERE id = ?`, runID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return warmagent.UserResponse{}, warmagent.ErrRunNotFound
	} else if err != nil {
		return warmagent.UserResponse{}, fmt.Errorf("read agent response status: %w", err)
	}
	if status != warmagent.RunStatusWaitingInput {
		return warmagent.UserResponse{}, warmagent.ErrRunNotWaitingInput
	}
	var latestID, dataJSON string
	if err := transaction.QueryRow(`
SELECT id, data_json FROM agent_run_events
	WHERE run_id = ? AND type = ? ORDER BY sequence DESC LIMIT 1`, runID, warmagent.EventApprovalRequired).Scan(&latestID, &dataJSON); errors.Is(err, sql.ErrNoRows) {
		return warmagent.UserResponse{}, warmagent.ErrInvalidUserResponse
	} else if err != nil {
		return warmagent.UserResponse{}, fmt.Errorf("read pending agent question: %w", err)
	}
	if latestID != approvalEventID {
		return warmagent.UserResponse{}, warmagent.ErrInvalidUserResponse
	}
	var approval struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(dataJSON), &approval); err != nil {
		return warmagent.UserResponse{}, fmt.Errorf("decode pending agent question: %w", err)
	}
	queuedResponse := warmagent.UserResponse{
		ApprovalEventID: approvalEventID,
		Question:        approval.Question,
		Answer:          answer,
	}
	now := time.Now().UTC()
	if _, err := appendEvent(transaction, runID, warmagent.EventUserResponseReceived, queuedResponse, now); err != nil {
		return warmagent.UserResponse{}, err
	}
	result, err := transaction.Exec(`
UPDATE agent_runs SET status = ?, error_message = '', updated_at = ?
	WHERE id = ? AND status = ?`, warmagent.RunStatusQueued, now.Format(time.RFC3339Nano), runID, warmagent.RunStatusWaitingInput)
	if err != nil {
		return warmagent.UserResponse{}, fmt.Errorf("queue agent response: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return warmagent.UserResponse{}, fmt.Errorf("read queued agent response: %w", err)
	}
	if changed == 0 {
		return warmagent.UserResponse{}, warmagent.ErrRunNotWaitingInput
	}
	if err := transaction.Commit(); err != nil {
		return warmagent.UserResponse{}, err
	}
	return queuedResponse, nil
}

func (r *AgentRepository) MarkResumed(runID string) error {
	transaction, err := r.database.Begin()
	if err != nil {
		return fmt.Errorf("begin resume agent run: %w", err)
	}
	defer transaction.Rollback()
	now := time.Now().UTC()
	result, err := transaction.Exec(`
UPDATE agent_runs SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		warmagent.RunStatusRunning, now.Format(time.RFC3339Nano), runID, warmagent.RunStatusQueued)
	if err != nil {
		return fmt.Errorf("resume agent run: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read resumed agent run: %w", err)
	}
	if changed == 0 {
		return warmagent.ErrRunNotCancellable
	}
	if _, err := appendEvent(transaction, runID, warmagent.EventRunResumed, nil, now); err != nil {
		return err
	}
	return transaction.Commit()
}

func (r *AgentRepository) ListUserResponses(runID string) ([]warmagent.UserResponse, error) {
	events, err := r.ListEvents(runID, 0)
	if err != nil {
		return nil, err
	}
	responses := make([]warmagent.UserResponse, 0)
	for _, event := range events {
		if event.Type != warmagent.EventUserResponseReceived {
			continue
		}
		var response warmagent.UserResponse
		if err := json.Unmarshal(event.Data, &response); err != nil {
			return nil, fmt.Errorf("decode agent user response: %w", err)
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func (r *AgentRepository) Complete(run warmagent.Run, result warmagent.RunResult) (warmagent.Candidate, error) {
	now := time.Now().UTC()
	candidate := warmagent.Candidate{
		ID: result.CandidateID, RunID: run.ID, WorkID: run.WorkID,
		SkillID: result.SkillID, SkillVersion: result.SkillVersion, Content: result.Content, CreatedAt: now,
	}
	transaction, err := r.database.Begin()
	if err != nil {
		return warmagent.Candidate{}, fmt.Errorf("begin complete agent run: %w", err)
	}
	defer transaction.Rollback()
	statusResult, err := transaction.Exec(`
UPDATE agent_runs SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		warmagent.RunStatusCompleted, now.Format(time.RFC3339Nano), run.ID, warmagent.RunStatusRunning)
	if err != nil {
		return warmagent.Candidate{}, fmt.Errorf("complete agent run: %w", err)
	}
	changed, err := statusResult.RowsAffected()
	if err != nil {
		return warmagent.Candidate{}, fmt.Errorf("read completed agent run: %w", err)
	}
	if changed == 0 {
		return warmagent.Candidate{}, warmagent.ErrRunNotCancellable
	}
	if _, err := appendEvent(transaction, run.ID, warmagent.EventRunCompleted, map[string]any{"candidateId": candidate.ID}, now); err != nil {
		return warmagent.Candidate{}, err
	}
	if err := transaction.Commit(); err != nil {
		return warmagent.Candidate{}, fmt.Errorf("commit completed agent run: %w", err)
	}
	return candidate, nil
}

// CompleteReadOnly finishes a work-level exploration without creating a
// canvas candidate. The result is already exposed through message.delta; this
// event only closes the durable run lifecycle.
func (r *AgentRepository) CompleteReadOnly(run warmagent.Run, result warmagent.RunResult) error {
	now := time.Now().UTC()
	transaction, err := r.database.Begin()
	if err != nil {
		return fmt.Errorf("begin complete read-only agent run: %w", err)
	}
	defer transaction.Rollback()
	statusResult, err := transaction.Exec(`
UPDATE agent_runs SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		warmagent.RunStatusCompleted, now.Format(time.RFC3339Nano), run.ID, warmagent.RunStatusRunning)
	if err != nil {
		return fmt.Errorf("complete read-only agent run: %w", err)
	}
	changed, err := statusResult.RowsAffected()
	if err != nil {
		return fmt.Errorf("read completed read-only agent run: %w", err)
	}
	if changed == 0 {
		return warmagent.ErrRunNotCancellable
	}
	if strings.TrimSpace(result.Message) != "" {
		if _, err := appendEvent(transaction, run.ID, warmagent.EventMessageDelta, map[string]string{
			"delta": strings.TrimSpace(result.Message),
		}, now); err != nil {
			return err
		}
	}
	if _, err := appendEvent(transaction, run.ID, warmagent.EventRunCompleted, map[string]any{
		"mode": "read-only", "role": result.Role, "skillId": result.SkillID,
	}, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit read-only agent run: %w", err)
	}
	return nil
}

// CompleteCollaborativeProposal persists each proposed new node as an
// independent pending candidate. The proposal remains reviewable; accepting a
// candidate is what creates the durable canvas node.
func (r *AgentRepository) CompleteCollaborativeProposal(run warmagent.Run, result warmagent.RunResult) error {
	var proposal warmagent.ProposalSet
	decoder := json.NewDecoder(strings.NewReader(result.Content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil {
		return fmt.Errorf("decode collaborative proposal: %w", err)
	}
	if len(proposal.Nodes) == 0 {
		return fmt.Errorf("decode collaborative proposal: %w", errors.New("proposal has no new nodes"))
	}
	if len(proposal.Nodes) > 1 {
		return fmt.Errorf("decode collaborative proposal: %w", errors.New("proposal has more than one node"))
	}
	now := time.Now().UTC()
	tx, err := r.database.Begin()
	if err != nil {
		return fmt.Errorf("begin complete collaborative proposal: %w", err)
	}
	defer tx.Rollback()
	created := make([]map[string]any, 0, len(proposal.Nodes))
	for index, node := range proposal.Nodes {
		clientID := strings.TrimSpace(node.ClientID)
		kind := strings.TrimSpace(node.Kind)
		title := strings.TrimSpace(node.Title)
		content := strings.TrimSpace(node.Content)
		if clientID == "" || kind == "" || title == "" || content == "" {
			return fmt.Errorf("collaborative proposal node %d is incomplete", index)
		}
		candidateID := uuid.NewString()
		x := 520 + float64(index%8)*36
		y := 80 + float64(index/8)*220
		if _, err := tx.Exec(`
INSERT INTO agent_candidates (id,run_id,work_id,skill_id,skill_version,status,kind,title,content,x,y,accepted_node_id,created_at,decided_at,candidate_type,node_id,base_version_id,reason,change_score)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			candidateID, run.ID, run.WorkID, result.SkillID, result.SkillVersion,
			warmagent.CandidateStatusPending, kind, title, content, x, y, "",
			now.Format(time.RFC3339Nano), "", "node", "", "", "", 0); err != nil {
			return fmt.Errorf("create collaborative candidate: %w", err)
		}
		created = append(created, map[string]any{
			"candidateId": candidateID,
			"clientId":    clientID,
			"kind":        kind,
			"title":       title,
			"ordinal":     index + 1,
			"total":       len(proposal.Nodes),
			"x":           x,
			"y":           y,
		})
	}
	statusResult, err := tx.Exec(`UPDATE agent_runs SET status=?,updated_at=? WHERE id=? AND status=?`,
		warmagent.RunStatusCompleted, now.Format(time.RFC3339Nano), run.ID, warmagent.RunStatusRunning)
	if err != nil {
		return fmt.Errorf("complete collaborative proposal: %w", err)
	}
	changed, err := statusResult.RowsAffected()
	if err != nil || changed == 0 {
		return warmagent.ErrRunNotCancellable
	}
	for _, metadata := range created {
		if _, err := appendEvent(tx, run.ID, warmagent.EventCandidateCreated, map[string]any{
			"candidateId": metadata["candidateId"],
			"meta": map[string]any{
				"clientId": metadata["clientId"], "kind": metadata["kind"],
				"title": metadata["title"], "ordinal": metadata["ordinal"], "total": metadata["total"],
				"position": map[string]any{"x": metadata["x"], "y": metadata["y"]},
			},
			"mode": "collaborative-proposal",
		}, now); err != nil {
			return err
		}
	}
	if _, err := appendEvent(tx, run.ID, warmagent.EventRunCompleted, map[string]any{
		"candidateIds": created,
		"mode":         "collaborative-proposal",
	}, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit collaborative proposal: %w", err)
	}
	return nil
}

type archiveProposal struct {
	NodeID      string  `json:"nodeId"`
	Kind        string  `json:"kind"`
	Title       string  `json:"title"`
	Content     string  `json:"content"`
	ChangeScore float64 `json:"changeScore"`
	Reason      string  `json:"reason"`
}

type archiveSectionResult struct {
	SectionOutlineNodeID string `json:"sectionOutlineNodeId"`
	ChapterSectionNodeID string `json:"chapterSectionNodeId"`
	NodeRevision         int64  `json:"nodeRevision"`
	Ordinal              int    `json:"ordinal"`
	Summary              string `json:"summary"`
}

type archiveContentResult struct {
	Summary  string                 `json:"summary"`
	Sections []archiveSectionResult `json:"sections"`
}

type archiveResult struct {
	Archive   archiveContentResult `json:"archive"`
	Proposals []archiveProposal    `json:"proposals"`
}

type archiveSectionSource struct {
	SectionOutlineNodeID    string
	ChapterSectionNodeID    string
	ChapterSectionVersionID string
	NodeRevision            int64
	Title                   string
	Content                 string
	Ordinal                 int
	Summary                 string
}

func (r *AgentRepository) CompleteChapterArchive(ctx context.Context, run warmagent.Run, result warmagent.RunResult) error {
	var decoded archiveResult
	decoder := json.NewDecoder(strings.NewReader(result.Content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("decode chapter archive result: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: archive result must contain one JSON object", canvas.ErrInvalidChapterArchive)
	}
	decoded.Archive.Summary = strings.TrimSpace(decoded.Archive.Summary)
	if decoded.Archive.Summary == "" {
		return fmt.Errorf("%w: archive summary is required", canvas.ErrInvalidChapterArchive)
	}
	now := time.Now().UTC()
	tx, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	locked, err := isNodeArchiveLocked(ctx, tx, run.WorkID, run.TargetNodeID)
	if err != nil {
		return err
	}
	if locked {
		return canvas.ErrArchivedNodeLocked
	}
	var chapterKind canvas.NodeKind
	var outlineVersionID, outlineTitle, outlineContent string
	var outlineRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT kind,current_version_id,revision,title,content FROM canvas_nodes WHERE work_id=? AND id=?`, run.WorkID, run.TargetNodeID).Scan(&chapterKind, &outlineVersionID, &outlineRevision, &outlineTitle, &outlineContent); err != nil {
		return err
	}
	if chapterKind != canvas.NodeKindChapterOutline {
		return canvas.ErrInvalidNode
	}
	if outlineRevision != result.ExpectedRevision {
		return fmt.Errorf("%w: chapter outline changed before archive completion", canvas.ErrRevisionConflict)
	}
	sections, err := readChapterArchiveSections(ctx, tx, run.WorkID, run.TargetNodeID, decoded.Archive.Sections)
	if err != nil {
		return err
	}
	var archiveRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM chapter_archives WHERE work_id=? AND chapter_outline_node_id=?`, run.WorkID, run.TargetNodeID).Scan(&archiveRevision); err != nil {
		return err
	}
	archiveID := uuid.NewString()
	sourceDigest := chapterArchiveDigest(run.TargetNodeID, outlineRevision, outlineContent, sections)
	if _, err := tx.ExecContext(ctx, `UPDATE chapter_archives SET is_current=0,superseded_at=? WHERE work_id=? AND chapter_outline_node_id=? AND is_current=1`, now.Format(time.RFC3339Nano), run.WorkID, run.TargetNodeID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO chapter_archives (id,work_id,chapter_outline_node_id,revision,run_id,outline_version_id,outline_revision,outline_title,outline_content,summary,source_digest,is_current,projection_status,created_at,superseded_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, archiveID, run.WorkID, run.TargetNodeID, archiveRevision, run.ID, outlineVersionID, outlineRevision, outlineTitle, outlineContent, decoded.Archive.Summary, sourceDigest, 1, "pending", now.Format(time.RFC3339Nano), ""); err != nil {
		return fmt.Errorf("create chapter archive: %w", err)
	}
	archiveSections := make([]canvas.ChapterArchiveSection, 0, len(sections))
	for _, section := range sections {
		contentHash := fmt.Sprintf("%x", sha256.Sum256([]byte(section.Content)))
		if _, err := tx.ExecContext(ctx, `INSERT INTO chapter_archive_sections (archive_id,work_id,ordinal,section_outline_node_id,chapter_section_node_id,chapter_section_version_id,node_revision,title,summary,content,content_hash) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, archiveID, run.WorkID, section.Ordinal, section.SectionOutlineNodeID, section.ChapterSectionNodeID, section.ChapterSectionVersionID, section.NodeRevision, section.Title, section.Summary, section.Content, contentHash); err != nil {
			return fmt.Errorf("create chapter archive section: %w", err)
		}
		archiveSections = append(archiveSections, canvas.ChapterArchiveSection{
			ArchiveID: archiveID, Ordinal: section.Ordinal, SectionOutlineNodeID: section.SectionOutlineNodeID,
			ChapterSectionNodeID: section.ChapterSectionNodeID, ChapterSectionVersionID: section.ChapterSectionVersionID,
			NodeRevision: section.NodeRevision, Title: section.Title, Summary: section.Summary,
			Content: section.Content, ContentHash: contentHash,
		})
	}
	layoutSections := make([]chapterLayoutSection, len(sections))
	for index, section := range sections {
		layoutSections[index] = chapterLayoutSection{
			SectionOutlineNodeID: section.SectionOutlineNodeID,
			ChapterSectionNodeID: section.ChapterSectionNodeID,
		}
	}
	layoutPositions, _, err := layoutChapterNodes(ctx, tx, run.WorkID, run.TargetNodeID, layoutSections)
	if err != nil {
		return fmt.Errorf("layout archived chapter: %w", err)
	}
	if err := applyNodePositions(ctx, tx, run.WorkID, layoutPositions); err != nil {
		return fmt.Errorf("apply archived chapter layout: %w", err)
	}
	created := make([]string, 0, len(decoded.Proposals))
	for _, proposal := range decoded.Proposals {
		if proposal.NodeID == "" || !containsString(run.ContextNodeIDs, proposal.NodeID) || proposal.NodeID == run.TargetNodeID || strings.TrimSpace(proposal.Content) == "" {
			continue
		}
		var kind canvas.NodeKind
		var title string
		var baseVersionID string
		if err := tx.QueryRowContext(ctx, `SELECT kind,title,current_version_id FROM canvas_nodes WHERE work_id=? AND id=?`, run.WorkID, proposal.NodeID).Scan(&kind, &title, &baseVersionID); err != nil {
			continue
		}
		if proposal.Kind != "" && proposal.Kind != string(kind) {
			continue
		}
		x, y := 0.0, 0.0
		if err := tx.QueryRowContext(ctx, `SELECT x,y FROM canvas_nodes WHERE work_id=? AND id=?`, run.WorkID, proposal.NodeID).Scan(&x, &y); err != nil {
			continue
		}
		var pending int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_candidates WHERE work_id=? AND status=?`, run.WorkID, warmagent.CandidateStatusPending).Scan(&pending); err != nil {
			return err
		}
		x += 320 + float64(pending/8)*36
		y += float64(pending%8) * 36
		candidateID := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_candidates (id,run_id,work_id,skill_id,skill_version,status,kind,title,content,x,y,accepted_node_id,created_at,decided_at,candidate_type,node_id,base_version_id,reason,change_score) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, candidateID, run.ID, run.WorkID, result.SkillID, result.SkillVersion, warmagent.CandidateStatusPending, kind, valueOrArchiveTitle(proposal.Title, title), proposal.Content, x, y, "", now.Format(time.RFC3339Nano), "", "version", proposal.NodeID, baseVersionID, proposal.Reason, proposal.ChangeScore); err != nil {
			return err
		}
		created = append(created, candidateID)
	}
	statusResult, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status=?,updated_at=? WHERE id=? AND status=?`, warmagent.RunStatusCompleted, now.Format(time.RFC3339Nano), run.ID, warmagent.RunStatusRunning)
	if err != nil {
		return err
	}
	changed, err := statusResult.RowsAffected()
	if err != nil {
		return fmt.Errorf("read completed chapter archive run: %w", err)
	}
	if changed == 0 {
		return warmagent.ErrRunNotCancellable
	}
	if _, err := appendEvent(tx, run.ID, warmagent.EventCandidateCreated, map[string]any{"candidateIds": created, "candidateType": "version"}, now); err != nil {
		return err
	}
	if _, err := appendEvent(tx, run.ID, warmagent.EventRunCompleted, map[string]any{"archiveId": archiveID, "candidateIds": created}, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM canvas_actions WHERE work_id=?`, run.WorkID); err != nil {
		return fmt.Errorf("clear canvas actions at chapter archive checkpoint: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM canvas_history_state WHERE work_id=?`, run.WorkID); err != nil {
		return fmt.Errorf("clear canvas history at chapter archive checkpoint: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	archive := canvas.ChapterArchive{
		ID: archiveID, WorkID: run.WorkID, ChapterOutlineNodeID: run.TargetNodeID,
		Revision: archiveRevision, RunID: run.ID, OutlineVersionID: outlineVersionID,
		OutlineRevision: outlineRevision, OutlineTitle: outlineTitle, OutlineContent: outlineContent,
		Summary: decoded.Archive.Summary, SourceDigest: sourceDigest, IsCurrent: true,
		ProjectionStatus: "pending", Sections: archiveSections, CreatedAt: now,
	}
	projectionStatus := "ready"
	if err := writeChapterArchiveProjection(r.dataDirectory, archive); err != nil {
		projectionStatus = "pending"
	}
	_, _ = r.database.ExecContext(ctx, `UPDATE chapter_archives SET projection_status=? WHERE id=?`, projectionStatus, archiveID)
	return nil
}

func readChapterArchiveSections(ctx context.Context, tx *sql.Tx, workID, chapterOutlineNodeID string, summaries []archiveSectionResult) ([]archiveSectionSource, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT so.id,cs.id,cs.current_version_id,cs.revision,cs.title,cs.content
FROM canvas_edges chapter_edge
JOIN canvas_nodes so
  ON so.work_id=chapter_edge.work_id AND so.id=chapter_edge.target_node_id AND so.kind=?
LEFT JOIN canvas_edges section_edge
  ON section_edge.work_id=chapter_edge.work_id AND section_edge.source_node_id=so.id AND section_edge.kind='generated_from'
LEFT JOIN canvas_nodes cs
  ON cs.work_id=section_edge.work_id AND cs.id=section_edge.target_node_id AND cs.kind=?
WHERE chapter_edge.work_id=? AND chapter_edge.source_node_id=? AND chapter_edge.kind='generated_from'`, canvas.NodeKindSectionOutline, canvas.NodeKindChapterSection, workID, chapterOutlineNodeID)
	if err != nil {
		return nil, fmt.Errorf("read chapter archive sections: %w", err)
	}
	sourcesByNodeID := make(map[string]archiveSectionSource)
	plannedSectionOutlineIDs := make(map[string]struct{})
	completedSectionOutlineIDs := make(map[string]struct{})
	for rows.Next() {
		var (
			sectionOutlineNodeID    string
			chapterSectionNodeID    sql.NullString
			chapterSectionVersionID sql.NullString
			nodeRevision            sql.NullInt64
			title                   sql.NullString
			content                 sql.NullString
		)
		if err := rows.Scan(&sectionOutlineNodeID, &chapterSectionNodeID, &chapterSectionVersionID, &nodeRevision, &title, &content); err != nil {
			return nil, err
		}
		plannedSectionOutlineIDs[sectionOutlineNodeID] = struct{}{}
		if !chapterSectionNodeID.Valid {
			continue
		}
		source := archiveSectionSource{
			SectionOutlineNodeID:    sectionOutlineNodeID,
			ChapterSectionNodeID:    chapterSectionNodeID.String,
			ChapterSectionVersionID: chapterSectionVersionID.String,
			NodeRevision:            nodeRevision.Int64,
			Title:                   title.String,
			Content:                 content.String,
		}
		sourcesByNodeID[source.ChapterSectionNodeID] = source
		completedSectionOutlineIDs[source.SectionOutlineNodeID] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(sourcesByNodeID) == 0 {
		return nil, fmt.Errorf("%w: chapter has no completed sections", canvas.ErrInvalidChapterArchive)
	}
	if len(plannedSectionOutlineIDs) != len(completedSectionOutlineIDs) {
		return nil, fmt.Errorf("%w: all %d section outlines must have completed chapter sections", canvas.ErrChapterArchiveIncomplete, len(plannedSectionOutlineIDs))
	}
	if len(summaries) != len(sourcesByNodeID) {
		return nil, fmt.Errorf("%w: every completed section must be summarized", canvas.ErrInvalidChapterArchive)
	}
	ordinals := make(map[int]struct{}, len(summaries))
	sections := make([]archiveSectionSource, 0, len(summaries))
	for _, summary := range summaries {
		source, ok := sourcesByNodeID[strings.TrimSpace(summary.ChapterSectionNodeID)]
		if !ok || source.SectionOutlineNodeID != strings.TrimSpace(summary.SectionOutlineNodeID) || summary.Ordinal < 1 || strings.TrimSpace(summary.Summary) == "" {
			return nil, fmt.Errorf("%w: section summary does not match chapter graph", canvas.ErrInvalidChapterArchive)
		}
		if source.NodeRevision != summary.NodeRevision {
			return nil, fmt.Errorf("%w: chapter section %q changed before archive completion", canvas.ErrRevisionConflict, source.ChapterSectionNodeID)
		}
		if _, exists := ordinals[summary.Ordinal]; exists {
			return nil, fmt.Errorf("%w: duplicate section ordinal", canvas.ErrInvalidChapterArchive)
		}
		ordinals[summary.Ordinal] = struct{}{}
		source.Ordinal = summary.Ordinal
		source.Summary = strings.TrimSpace(summary.Summary)
		sections = append(sections, source)
	}
	sort.Slice(sections, func(i, j int) bool { return sections[i].Ordinal < sections[j].Ordinal })
	for index, section := range sections {
		if section.Ordinal != index+1 {
			return nil, fmt.Errorf("%w: section ordinals must be contiguous", canvas.ErrInvalidChapterArchive)
		}
	}
	return sections, nil
}

func chapterArchiveDigest(chapterOutlineNodeID string, outlineRevision int64, outlineContent string, sections []archiveSectionSource) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%s\x00%d\x00%s", chapterOutlineNodeID, outlineRevision, outlineContent)
	for _, section := range sections {
		_, _ = fmt.Fprintf(hash, "\x00%s\x00%s\x00%d\x00%d\x00%s", section.SectionOutlineNodeID, section.ChapterSectionNodeID, section.Ordinal, section.NodeRevision, section.Content)
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func renderChapterArchiveProjection(archive canvas.ChapterArchive) []byte {
	var content strings.Builder
	fmt.Fprintf(&content, "---\narchiveId: %q\nworkId: %q\nchapterOutlineNodeId: %q\narchiveRevision: %d\ncontentHash: %q\n---\n\n# %s\n\n%s\n", archive.ID, archive.WorkID, archive.ChapterOutlineNodeID, archive.Revision, archive.SourceDigest, archive.OutlineTitle, archive.Summary)
	for _, section := range archive.Sections {
		fmt.Fprintf(&content, "\n## %d. %s\n\n%s\n\n<!-- sectionOutlineNodeId: %s; chapterSectionNodeId: %s; chapterSectionVersionId: %s; nodeRevision: %d; contentHash: %s -->\n", section.Ordinal, section.Title, section.Summary, section.SectionOutlineNodeID, section.ChapterSectionNodeID, section.ChapterSectionVersionID, section.NodeRevision, section.ContentHash)
	}
	return []byte(content.String())
}

func chapterArchiveProjectionPath(dataDirectory string, archive canvas.ChapterArchive) string {
	return filepath.Join(dataDirectory, "works", safepath.Component(archive.WorkID), "story-spine", "chapters", safepath.Component(archive.ChapterOutlineNodeID)+".md")
}

func writeChapterArchiveProjection(dataDirectory string, archive canvas.ChapterArchive) error {
	projectionPath := chapterArchiveProjectionPath(dataDirectory, archive)
	directory := filepath.Dir(projectionPath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".chapter-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(renderChapterArchiveProjection(archive)); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, projectionPath)
}

func valueOrArchiveTitle(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func (r *AgentRepository) CompleteNodeUpdate(
	ctx context.Context,
	run warmagent.Run,
	nodeID string,
	result warmagent.RunResult,
) error {
	now := time.Now().UTC()
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin complete node update: %w", err)
	}
	defer transaction.Rollback()
	locked, err := isNodeArchiveLocked(ctx, transaction, run.WorkID, nodeID)
	if err != nil {
		return err
	}
	if locked {
		return canvas.ErrArchivedNodeLocked
	}
	before, err := scanCanvasNode(transaction.QueryRowContext(ctx, `
SELECT id, work_id, revision, kind, title, content, x, y, created_at, updated_at
FROM canvas_nodes WHERE work_id = ? AND id = ?`, run.WorkID, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return canvas.ErrNodeNotFound
	}
	if err != nil {
		return fmt.Errorf("read canvas node before agent update: %w", err)
	}
	if before.Revision != result.ExpectedRevision {
		return fmt.Errorf("%w: target node %q changed before agent update", canvas.ErrRevisionConflict, nodeID)
	}
	var beforeVersionID string
	if err := transaction.QueryRowContext(ctx, `SELECT current_version_id FROM canvas_nodes WHERE work_id=? AND id=?`, run.WorkID, nodeID).Scan(&beforeVersionID); err != nil {
		return fmt.Errorf("read canvas node version before agent update: %w", err)
	}
	updated, err := transaction.ExecContext(ctx, `
UPDATE canvas_nodes
SET title = ?, content = ?, revision = revision + 1, updated_at = ?
WHERE work_id = ? AND id = ? AND revision = ?`,
		result.Title, result.Content, now.Format(time.RFC3339Nano), run.WorkID, nodeID, result.ExpectedRevision)
	if err != nil {
		return fmt.Errorf("update canvas node from agent run: %w", err)
	}
	changed, err := updated.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated canvas node from agent run: %w", err)
	}
	if changed == 0 {
		return fmt.Errorf("%w: target node %q changed before agent update", canvas.ErrRevisionConflict, nodeID)
	}
	if before.Title != result.Title || before.Content != result.Content {
		versionNode := before
		versionNode.Title = result.Title
		versionNode.Content = result.Content
		versionNode.Revision++
		versionNode.UpdatedAt = now
		if _, err := createNodeVersion(ctx, transaction, versionNode, beforeVersionID, run.ID); err != nil {
			return err
		}
		payload := updateNodeActionPayload{
			Before: nodeContentState{NodeID: before.ID, Title: before.Title, Content: before.Content},
			After:  nodeContentState{NodeID: before.ID, Title: result.Title, Content: result.Content},
		}
		if err := appendCanvasAction(ctx, transaction, run.WorkID, actionUpdateNode, "Agent 更新节点", payload); err != nil {
			return err
		}
	}
	statusResult, err := transaction.ExecContext(ctx, `
UPDATE agent_runs SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		warmagent.RunStatusCompleted, now.Format(time.RFC3339Nano), run.ID, warmagent.RunStatusRunning)
	if err != nil {
		return fmt.Errorf("complete node update run: %w", err)
	}
	statusChanged, err := statusResult.RowsAffected()
	if err != nil {
		return fmt.Errorf("read completed node update run: %w", err)
	}
	if statusChanged == 0 {
		return warmagent.ErrRunNotCancellable
	}
	if _, err := appendEvent(transaction, run.ID, warmagent.EventNodeUpdated, map[string]any{"nodeId": nodeID}, now); err != nil {
		return err
	}
	if _, err := appendEvent(transaction, run.ID, warmagent.EventRunCompleted, map[string]any{"nodeId": nodeID}, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit node update run: %w", err)
	}
	return nil
}

func (r *AgentRepository) CompleteDerivation(
	ctx context.Context,
	run warmagent.Run,
	parentNodeID string,
	result warmagent.RunResult,
) error {
	now := time.Now().UTC()
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin complete node derivation: %w", err)
	}
	defer transaction.Rollback()

	parent, err := scanCanvasNode(transaction.QueryRowContext(ctx, `
SELECT id, work_id, revision, kind, title, content, x, y, created_at, updated_at
FROM canvas_nodes WHERE work_id = ? AND id = ?`, run.WorkID, parentNodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return canvas.ErrNodeNotFound
	}
	if err != nil {
		return fmt.Errorf("read derivation parent node: %w", err)
	}
	if parent.Revision != result.ExpectedRevision {
		return fmt.Errorf("%w: parent node %q changed before derivation", canvas.ErrRevisionConflict, parentNodeID)
	}

	derivedKind := canvas.NodeKindSectionOutline
	if run.Target == warmagent.TargetChapterSection {
		derivedKind = canvas.NodeKindChapterSection
	}
	var existingChildren int
	if err := transaction.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM canvas_edges e
JOIN canvas_nodes n ON n.work_id = e.work_id AND n.id = e.target_node_id
WHERE e.work_id = ? AND e.source_node_id = ? AND e.kind = 'generated_from' AND n.kind = ?`,
		run.WorkID, parentNodeID, derivedKind).Scan(&existingChildren); err != nil {
		return fmt.Errorf("read existing derived nodes: %w", err)
	}
	if existingChildren > 0 {
		return canvas.ErrDerivationExists
	}

	inputs, err := derivationNodeInputs(run, parent, result.Content)
	if err != nil {
		return err
	}
	nodes := make([]canvas.Node, 0, len(inputs))
	edges := make([]canvas.Edge, 0, len(inputs)*2)
	startY := parent.Y - float64(len(inputs)-1)*110
	for index, input := range inputs {
		node := canvas.Node{
			ID: uuid.NewString(), WorkID: run.WorkID, Revision: 1, Kind: input.Kind,
			Title: input.Title, Content: input.Content, X: parent.X + 360,
			Y: startY + float64(index)*220, CreatedAt: now, UpdatedAt: now,
		}
		if _, err := transaction.ExecContext(ctx, `
INSERT INTO canvas_nodes (id, work_id, revision, kind, title, content, x, y, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, node.ID, node.WorkID, node.Revision, node.Kind, node.Title,
			node.Content, node.X, node.Y, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("create derived canvas node: %w", err)
		}
		if _, err := createInitialNodeVersion(ctx, transaction, node); err != nil {
			return err
		}
		sourceNodeIDs := uniqueStrings(append([]string{parentNodeID}, input.ContextNodeIDs...))
		for _, sourceNodeID := range sourceNodeIDs {
			if !containsString(run.ContextNodeIDs, sourceNodeID) {
				return fmt.Errorf("%w: derived context node %q was not supplied to the run", canvas.ErrInvalidSectionOutline, sourceNodeID)
			}
			var sourceExists int
			if err := transaction.QueryRowContext(ctx, `
SELECT 1 FROM canvas_nodes WHERE work_id = ? AND id = ?`, run.WorkID, sourceNodeID).Scan(&sourceExists); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return canvas.ErrNodeNotFound
				}
				return fmt.Errorf("read derived context node: %w", err)
			}
			edge := canvas.Edge{
				ID: uuid.NewString(), WorkID: run.WorkID, SourceNodeID: sourceNodeID,
				TargetNodeID: node.ID, Kind: "generated_from", CreatedAt: now,
			}
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO canvas_edges (id, work_id, source_node_id, target_node_id, kind, created_at)
VALUES (?, ?, ?, ?, ?, ?)`, edge.ID, edge.WorkID, edge.SourceNodeID, edge.TargetNodeID,
				edge.Kind, now.Format(time.RFC3339Nano)); err != nil {
				return fmt.Errorf("create derived canvas edge: %w", err)
			}
			edges = append(edges, edge)
		}
		nodes = append(nodes, node)
	}
	if err := appendCanvasAction(ctx, transaction, run.WorkID, actionCreateNodes, "Agent 派生节点", createNodesActionPayload{
		Nodes: nodes, Edges: edges,
	}); err != nil {
		return err
	}
	statusResult, err := transaction.ExecContext(ctx, `
UPDATE agent_runs SET status = ?, updated_at = ? WHERE id = ? AND status = ?`,
		warmagent.RunStatusCompleted, now.Format(time.RFC3339Nano), run.ID, warmagent.RunStatusRunning)
	if err != nil {
		return fmt.Errorf("complete node derivation run: %w", err)
	}
	changed, err := statusResult.RowsAffected()
	if err != nil {
		return fmt.Errorf("read completed node derivation run: %w", err)
	}
	if changed == 0 {
		return warmagent.ErrRunNotCancellable
	}
	nodeIDs := make([]string, len(nodes))
	for index, node := range nodes {
		nodeIDs[index] = node.ID
	}
	if _, err := appendEvent(transaction, run.ID, warmagent.EventNodesCreated, map[string]any{
		"parentNodeId": parentNodeID, "nodeIds": nodeIDs, "kind": derivedKind,
	}, now); err != nil {
		return err
	}
	if _, err := appendEvent(transaction, run.ID, warmagent.EventRunCompleted, map[string]any{
		"parentNodeId": parentNodeID, "nodeIds": nodeIDs,
	}, now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit node derivation: %w", err)
	}
	return nil
}

type derivationNodeInput struct {
	Kind           canvas.NodeKind
	Title          string
	Content        string
	ContextNodeIDs []string
}

func derivationNodeInputs(run warmagent.Run, parent canvas.Node, content string) ([]derivationNodeInput, error) {
	if run.Target == warmagent.TargetSectionOutlineBatch {
		if parent.Kind != canvas.NodeKindChapterOutline {
			return nil, fmt.Errorf("%w: section outlines require a chapter outline", canvas.ErrInvalidNode)
		}
		batch, err := canvas.DecodeSectionOutlineBatch(content)
		if err != nil {
			return nil, err
		}
		if batch.ChapterOutlineNodeID != parent.ID {
			return nil, fmt.Errorf("%w: chapterOutlineNodeId does not match target node", canvas.ErrInvalidSectionOutline)
		}
		sort.Slice(batch.Sections, func(left, right int) bool {
			return batch.Sections[left].Outline.Ordinal < batch.Sections[right].Outline.Ordinal
		})
		inputs := make([]derivationNodeInput, 0, len(batch.Sections))
		for _, section := range batch.Sections {
			inputs = append(inputs, derivationNodeInput{
				Kind: canvas.NodeKindSectionOutline, Title: strings.TrimSpace(section.Title),
				Content: canvas.FormatSectionOutline(section.Outline),
			})
		}
		return inputs, nil
	}
	if run.Target != warmagent.TargetChapterSection || parent.Kind != canvas.NodeKindSectionOutline {
		return nil, fmt.Errorf("%w: unsupported derivation target", canvas.ErrInvalidNode)
	}
	var section struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&section); err != nil {
		return nil, fmt.Errorf("%w: decode chapter section: %v", canvas.ErrInvalidNode, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: chapter section must contain one JSON object", canvas.ErrInvalidNode)
	}
	section.Title = strings.TrimSpace(section.Title)
	section.Content = strings.TrimSpace(section.Content)
	if section.Title == "" || section.Content == "" {
		return nil, fmt.Errorf("%w: chapter section title and content are required", canvas.ErrInvalidNode)
	}
	return []derivationNodeInput{{
		Kind: canvas.NodeKindChapterSection, Title: section.Title, Content: section.Content,
		ContextNodeIDs: append([]string(nil), run.ContextNodeIDs...),
	}}, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func (r *AgentRepository) Fail(runID, message string) error {
	return r.setStatusWithEvent(runID, warmagent.RunStatusFailed, message, warmagent.EventRunFailed, map[string]string{"message": message})
}

func (r *AgentRepository) Cancel(runID string) error {
	transaction, err := r.database.Begin()
	if err != nil {
		return fmt.Errorf("begin cancel agent run: %w", err)
	}
	defer transaction.Rollback()
	now := time.Now().UTC()
	result, err := transaction.Exec(`
UPDATE agent_runs SET status = ?, updated_at = ?
	WHERE id = ? AND status IN (?, ?, ?)`, warmagent.RunStatusCancelled, now.Format(time.RFC3339Nano), runID,
		warmagent.RunStatusQueued, warmagent.RunStatusRunning, warmagent.RunStatusWaitingInput)
	if err != nil {
		return fmt.Errorf("cancel agent run: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read cancelled agent run: %w", err)
	}
	if changed == 0 {
		var status warmagent.RunStatus
		if err := transaction.QueryRow(`SELECT status FROM agent_runs WHERE id = ?`, runID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
			return warmagent.ErrRunNotFound
		} else if err != nil {
			return fmt.Errorf("query cancelled agent run: %w", err)
		}
		return warmagent.ErrRunNotCancellable
	}
	if _, err := appendEvent(transaction, runID, warmagent.EventRunCancelled, nil, now); err != nil {
		return err
	}
	return transaction.Commit()
}

func (r *AgentRepository) FailInterruptedRuns() error {
	rows, err := r.database.Query(`SELECT id FROM agent_runs WHERE status IN (?, ?)`, warmagent.RunStatusQueued, warmagent.RunStatusRunning)
	if err != nil {
		return fmt.Errorf("query interrupted agent runs: %w", err)
	}
	runIDs := make([]string, 0)
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			rows.Close()
			return fmt.Errorf("scan interrupted agent run: %w", err)
		}
		runIDs = append(runIDs, runID)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close interrupted agent runs: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate interrupted agent runs: %w", err)
	}
	for _, runID := range runIDs {
		if err := r.Fail(runID, "Core restarted before this run completed"); err != nil {
			return err
		}
	}
	return nil
}

func (r *AgentRepository) setStatusWithEvent(runID string, status warmagent.RunStatus, message string, eventType warmagent.EventType, data any) error {
	transaction, err := r.database.Begin()
	if err != nil {
		return fmt.Errorf("begin update agent run: %w", err)
	}
	defer transaction.Rollback()
	now := time.Now().UTC()
	result, err := transaction.Exec(`
UPDATE agent_runs SET status = ?, error_message = ?, updated_at = ?
WHERE id = ? AND status IN (?, ?)`, status, message, now.Format(time.RFC3339Nano), runID,
		warmagent.RunStatusQueued, warmagent.RunStatusRunning)
	if err != nil {
		return fmt.Errorf("update agent run: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated agent run: %w", err)
	}
	if changed == 0 {
		var currentStatus warmagent.RunStatus
		if err := transaction.QueryRow(`SELECT status FROM agent_runs WHERE id = ?`, runID).Scan(&currentStatus); errors.Is(err, sql.ErrNoRows) {
			return warmagent.ErrRunNotFound
		} else if err != nil {
			return fmt.Errorf("query agent run status: %w", err)
		}
		return warmagent.ErrRunNotCancellable
	}
	if _, err := appendEvent(transaction, runID, eventType, data, now); err != nil {
		return err
	}
	return transaction.Commit()
}

func appendEvent(transaction *sql.Tx, runID string, eventType warmagent.EventType, data any, now time.Time) (warmagent.Event, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return warmagent.Event{}, fmt.Errorf("encode agent event: %w", err)
	}
	var sequence int64
	if err := transaction.QueryRow(`SELECT COALESCE(MAX(sequence), 0) + 1 FROM agent_run_events WHERE run_id = ?`, runID).Scan(&sequence); err != nil {
		return warmagent.Event{}, fmt.Errorf("allocate agent event sequence: %w", err)
	}
	event := warmagent.Event{
		ID: uuid.NewString(), RunID: runID, Sequence: sequence, Type: eventType,
		Timestamp: now, Data: dataJSON,
	}
	_, err = transaction.Exec(`
INSERT INTO agent_run_events (id, run_id, sequence, type, data_json, created_at)
VALUES (?, ?, ?, ?, ?, ?)`, event.ID, event.RunID, event.Sequence, event.Type, string(event.Data), now.Format(time.RFC3339Nano))
	if err != nil {
		return warmagent.Event{}, fmt.Errorf("insert agent event: %w", err)
	}
	return event, nil
}

func scanRun(scanner rowScanner) (warmagent.Run, error) {
	var run warmagent.Run
	var contextNodeIDsJSON, createdAt, updatedAt string
	if err := scanner.Scan(&run.ID, &run.WorkID, &run.Status, &run.Prompt, &run.Target, &run.TargetNodeID, &run.ProviderID,
		&run.ModelID, &contextNodeIDsJSON, &run.ErrorMessage, &createdAt, &updatedAt); errors.Is(err, sql.ErrNoRows) {
		return warmagent.Run{}, warmagent.ErrRunNotFound
	} else if err != nil {
		return warmagent.Run{}, fmt.Errorf("scan agent run: %w", err)
	}
	if err := json.Unmarshal([]byte(contextNodeIDsJSON), &run.ContextNodeIDs); err != nil {
		return warmagent.Run{}, fmt.Errorf("decode context node ids: %w", err)
	}
	var err error
	run.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return warmagent.Run{}, fmt.Errorf("parse agent run created time: %w", err)
	}
	run.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return warmagent.Run{}, fmt.Errorf("parse agent run updated time: %w", err)
	}
	return run, nil
}

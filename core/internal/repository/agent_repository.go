package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	warmagent "warmnote/core/internal/agent"
	"warmnote/core/internal/canvas"
)

type AgentRepository struct {
	database *sql.DB
}

func NewAgentRepository(providerRepository *ProviderRepository) *AgentRepository {
	return &AgentRepository{database: providerRepository.database}
}

func (r *AgentRepository) GetCanvasNodeKind(workID, nodeID string) (canvas.NodeKind, error) {
	var kind canvas.NodeKind
	err := r.database.QueryRow(`SELECT kind FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, nodeID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", canvas.ErrNodeNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read target canvas node kind: %w", err)
	}
	return kind, nil
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

func (r *AgentRepository) QueueResponse(runID, approvalEventID, answer string) error {
	transaction, err := r.database.Begin()
	if err != nil {
		return fmt.Errorf("begin queue agent response: %w", err)
	}
	defer transaction.Rollback()
	var status warmagent.RunStatus
	if err := transaction.QueryRow(`SELECT status FROM agent_runs WHERE id = ?`, runID).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return warmagent.ErrRunNotFound
	} else if err != nil {
		return fmt.Errorf("read agent response status: %w", err)
	}
	if status != warmagent.RunStatusWaitingInput {
		return warmagent.ErrRunNotWaitingInput
	}
	var latestID, dataJSON string
	if err := transaction.QueryRow(`
SELECT id, data_json FROM agent_run_events
WHERE run_id = ? AND type = ? ORDER BY sequence DESC LIMIT 1`, runID, warmagent.EventApprovalRequired).Scan(&latestID, &dataJSON); errors.Is(err, sql.ErrNoRows) {
		return warmagent.ErrInvalidUserResponse
	} else if err != nil {
		return fmt.Errorf("read pending agent question: %w", err)
	}
	if latestID != approvalEventID {
		return warmagent.ErrInvalidUserResponse
	}
	var approval struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(dataJSON), &approval); err != nil {
		return fmt.Errorf("decode pending agent question: %w", err)
	}
	now := time.Now().UTC()
	if _, err := appendEvent(transaction, runID, warmagent.EventUserResponseReceived, map[string]string{
		"approvalEventId": approvalEventID, "question": approval.Question, "answer": answer,
	}, now); err != nil {
		return err
	}
	result, err := transaction.Exec(`
UPDATE agent_runs SET status = ?, error_message = '', updated_at = ?
WHERE id = ? AND status = ?`, warmagent.RunStatusQueued, now.Format(time.RFC3339Nano), runID, warmagent.RunStatusWaitingInput)
	if err != nil {
		return fmt.Errorf("queue agent response: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read queued agent response: %w", err)
	}
	if changed == 0 {
		return warmagent.ErrRunNotWaitingInput
	}
	return transaction.Commit()
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

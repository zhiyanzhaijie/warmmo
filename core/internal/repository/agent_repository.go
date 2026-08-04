package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func (r *AgentRepository) GetCanvasNodeKind(workID, nodeID string) (string, error) {
	var kind string
	err := r.database.QueryRow(`SELECT kind FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, nodeID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", canvas.ErrNodeNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read target canvas node kind: %w", err)
	}
	return kind, nil
}

func (r *AgentRepository) CreateRun(input warmagent.RunInput) (warmagent.Run, error) {
	contextNodeIDs, err := json.Marshal(input.ContextNodeIDs)
	if err != nil {
		return warmagent.Run{}, fmt.Errorf("encode context node ids: %w", err)
	}
	now := time.Now().UTC()
	run := warmagent.Run{
		ID: input.RunID, WorkID: input.WorkID, Status: warmagent.RunStatusQueued,
		Prompt: input.Prompt, Target: input.Target, ProviderID: input.ProviderID, ModelID: input.ModelID,
		ContextNodeIDs: append([]string(nil), input.ContextNodeIDs...), CreatedAt: now, UpdatedAt: now,
	}
	transaction, err := r.database.Begin()
	if err != nil {
		return warmagent.Run{}, fmt.Errorf("begin create agent run: %w", err)
	}
	defer transaction.Rollback()
	_, err = transaction.Exec(`
INSERT INTO agent_runs (
    id, work_id, status, prompt, target, provider_id, model_id, context_node_ids_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.WorkID, run.Status, run.Prompt, run.Target, run.ProviderID, run.ModelID,
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
SELECT id, work_id, status, prompt, target, provider_id, model_id, context_node_ids_json,
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

func (r *AgentRepository) CompleteNodeUpdate(run warmagent.Run, nodeID string, result warmagent.RunResult) error {
	now := time.Now().UTC()
	transaction, err := r.database.Begin()
	if err != nil {
		return fmt.Errorf("begin complete node update: %w", err)
	}
	defer transaction.Rollback()
	updated, err := transaction.Exec(`
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
	statusResult, err := transaction.Exec(`
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
WHERE id = ? AND status IN (?, ?)`, warmagent.RunStatusCancelled, now.Format(time.RFC3339Nano), runID,
		warmagent.RunStatusQueued, warmagent.RunStatusRunning)
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
	if err := scanner.Scan(&run.ID, &run.WorkID, &run.Status, &run.Prompt, &run.Target, &run.ProviderID,
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

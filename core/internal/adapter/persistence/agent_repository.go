package persistence

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
	"gorm.io/gorm"

	appagent "warmmo/core/internal/application/agent"
	"warmmo/core/internal/application/safepath"
	"warmmo/core/internal/domain/canvas"
)

type AgentRepository struct {
	database      *gorm.DB
	dataDirectory string
}

func (r *AgentRepository) GetRunByCandidate(candidateID, workID string) (appagent.Run, appagent.Candidate, error) {
	db := r.database
	var candidateModel agentCandidateModel
	if err := db.Where("id = ? AND work_id = ?", candidateID, workID).First(&candidateModel).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return appagent.Run{}, appagent.Candidate{}, canvas.ErrCandidateNotFound
	} else if err != nil {
		return appagent.Run{}, appagent.Candidate{}, fmt.Errorf("read candidate run: %w", err)
	}
	var runModel agentRunModel
	if err := db.First(&runModel, "id = ?", candidateModel.RunID).Error; err != nil {
		return appagent.Run{}, appagent.Candidate{}, fmt.Errorf("read candidate run: %w", err)
	}
	return runFromModel(runModel), candidateFromStoredModel(candidateModel, runModel.ContextNodeIDs), nil
}

func (r *AgentRepository) ListCollaborativeCandidates(runID string) ([]appagent.CollaborativeCandidate, error) {
	var models []agentCandidateModel
	if err := r.database.Select("id", "status", "kind", "title", "accepted_node_id").Where("run_id = ?", runID).Order("created_at, id").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list collaborative candidates: %w", err)
	}
	candidates := make([]appagent.CollaborativeCandidate, len(models))
	for i, model := range models {
		candidates[i] = appagent.CollaborativeCandidate{CandidateID: model.ID, Status: appagent.CandidateStatus(model.Status), Kind: normalizeLegacyCanvasKind(model.Kind), Title: model.Title, AcceptedNodeID: model.AcceptedNodeID}
	}
	return candidates, nil
}

func (r *AgentRepository) RequeueAfterCandidateDecision(runID, candidateID string, accepted bool, acceptedNodeID string) (bool, error) {
	requeued := false
	err := r.database.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var lastResume agentRunEventModel
		batchStartedAt := time.Time{}
		if err := tx.Where("run_id = ? AND type = ?", runID, appagent.EventRunResumed).Order("created_at DESC").First(&lastResume).Error; err == nil {
			batchStartedAt = lastResume.CreatedAt
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("read candidate batch start: %w", err)
		}
		var pending, rejected int64
		base := tx.Model(&agentCandidateModel{}).Where("run_id = ? AND created_at > ?", runID, batchStartedAt)
		if err := base.Where("status = ?", appagent.CandidateStatusPending).Count(&pending).Error; err != nil {
			return err
		}
		if err := base.Where("status = ?", appagent.CandidateStatusRejected).Count(&rejected).Error; err != nil {
			return err
		}
		requeued = pending == 0
		if requeued {
			result := tx.Model(&agentRunModel{}).Where("id = ? AND status = ?", runID, appagent.RunStatusCompleted).Updates(map[string]any{"status": appagent.RunStatusQueued, "error_message": "", "updated_at": now})
			if result.Error != nil {
				return fmt.Errorf("requeue agent run: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				return appagent.ErrRunNotCancellable
			}
		}
		_, err := appendEvent(tx, runID, appagent.EventCandidateDecision, map[string]any{"candidateId": candidateID, "accepted": accepted, "acceptedNodeId": acceptedNodeID, "pending": pending, "rejected": rejected}, now)
		return err
	})
	return requeued, err
}

func (r *AgentRepository) RequestCandidateDecisionReason(runID, candidateID, title string) error {
	return r.database.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Model(&agentRunModel{}).Where("id = ? AND status = ?", runID, appagent.RunStatusCompleted).Updates(map[string]any{"status": appagent.RunStatusWaitingInput, "error_message": "", "updated_at": now})
		if result.Error != nil {
			return fmt.Errorf("wait for candidate rejection reason: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return appagent.ErrRunNotCancellable
		}
		if _, err := appendEvent(tx, runID, appagent.EventCandidateDecision, map[string]any{"candidateId": candidateID, "accepted": false, "awaitingReason": true}, now); err != nil {
			return err
		}
		_, err := appendEvent(tx, runID, appagent.EventApprovalRequired, map[string]any{"candidateId": candidateID, "question": fmt.Sprintf("你拒绝了候选节点“%s”。请告诉我拒绝原因，我会据此重新生成。", title), "reason": "candidate_rejected"}, now)
		return err
	})
}

func NewAgentRepository(providerRepository *ProviderRepository) *AgentRepository {
	return NewAgentRepositoryWithDatabase(providerRepository.databaseHost)
}

func NewAgentRepositoryWithDatabase(database *Database) *AgentRepository {
	return &AgentRepository{database: database.DB, dataDirectory: database.DataDirectory()}
}

func (r *AgentRepository) GetCanvasNodeMetadata(workID, nodeID string) (canvas.NodeKind, int64, error) {
	var model canvasNodeModel
	if err := r.database.Select("kind", "revision").Where("work_id = ? AND id = ?", workID, nodeID).First(&model).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return "", 0, canvas.ErrNodeNotFound
	} else if err != nil {
		return "", 0, fmt.Errorf("read target canvas node metadata: %w", err)
	}
	return canvas.NodeKind(model.Kind), model.Revision, nil
}

func (r *AgentRepository) GetNodeAttachments(workID, targetNodeID string) ([]appagent.NodeReference, error) {
	var edges []canvasEdgeModel
	if err := r.database.Where("work_id = ? AND target_node_id = ? AND kind = ?", workID, targetNodeID, "generated_from").Order("created_at, source_node_id").Find(&edges).Error; err != nil {
		return nil, fmt.Errorf("read node attachments: %w", err)
	}
	ids := make([]string, len(edges))
	for i, edge := range edges {
		ids[i] = edge.SourceNodeID
	}
	return r.GetNodeReferences(workID, ids)
}

func nodeReferencesFromModels(models []canvasNodeModel) map[string]appagent.NodeReference {
	result := make(map[string]appagent.NodeReference, len(models))
	for _, model := range models {
		result[model.ID] = appagent.NodeReference{ID: model.ID, Type: model.Kind}
	}
	return result
}

func (r *AgentRepository) GetNodeReferences(workID string, nodeIDs []string) ([]appagent.NodeReference, error) {
	nodeIDs = uniqueStrings(nodeIDs)
	if len(nodeIDs) == 0 {
		return []appagent.NodeReference{}, nil
	}
	var models []canvasNodeModel
	if err := r.database.Select("id", "kind").Where("work_id = ? AND id IN ?", workID, nodeIDs).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("read canvas node references: %w", err)
	}
	referencesByID := nodeReferencesFromModels(models)
	references := make([]appagent.NodeReference, 0, len(nodeIDs))
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
func (r *AgentRepository) GetGlobalContextNodeReferences(workID string) ([]appagent.NodeReference, error) {
	var models []canvasNodeModel
	if err := r.database.Select("id", "kind").Where("work_id = ? AND kind IN ?", workID, []canvas.NodeKind{canvas.NodeKindWorld, canvas.NodeKindMechanism, canvas.NodeKindEvent}).Order("CASE kind WHEN 'world' THEN 1 WHEN 'mechanism' THEN 2 WHEN 'event' THEN 3 ELSE 4 END, created_at, id").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("read global context node references: %w", err)
	}
	global := make([]appagent.NodeReference, len(models))
	for i, model := range models {
		global[i] = appagent.NodeReference{ID: model.ID, Type: model.Kind}
	}
	return global, nil
}

// BuildContext returns a compact authoritative canvas snapshot for the model
// context layer. It is deliberately bounded; the canvas.get_nodes and
// story_spine.context tools remain available for deeper, targeted reads.
func (r *AgentRepository) BuildContext(ctx context.Context, workID string, maxBytes int) (string, error) {
	if r == nil || r.database == nil || strings.TrimSpace(workID) == "" || maxBytes <= 0 {
		return "", nil
	}
	type contextNode struct {
		Kind    string
		Title   string
		Content string
	}
	var nodes []contextNode
	if err := r.database.WithContext(ctx).Model(&canvasNodeModel{}).
		Select("kind, title, content").Where("work_id = ? AND kind IN ?", workID,
		[]canvas.NodeKind{canvas.NodeKindWorld, canvas.NodeKindMechanism, canvas.NodeKindEvent}).
		Order("CASE kind WHEN 'world' THEN 1 WHEN 'mechanism' THEN 2 WHEN 'event' THEN 3 ELSE 4 END, created_at, id").
		Find(&nodes).Error; err != nil {
		return "", fmt.Errorf("read canvas global context: %w", err)
	}
	var archives []chapterArchiveModel
	if err := r.database.WithContext(ctx).Where("work_id = ? AND is_current = ?", workID, true).
		Select("outline_title, summary").Order("created_at DESC, revision DESC, id DESC").Limit(8).Find(&archives).Error; err != nil {
		return "", fmt.Errorf("read canvas story spine context: %w", err)
	}
	var builder strings.Builder
	appendLine := func(line string) bool {
		line = strings.TrimSpace(line)
		if line == "" || builder.Len()+len(line)+1 > maxBytes {
			return false
		}
		builder.WriteString(line)
		builder.WriteByte('\n')
		return true
	}
	appendLine("# Canvas Global Context")
	appendLine("Authoritative current Canvas context. Prefer this over conversation history and recalled memory.")
	if len(archives) > 0 && appendLine("## Story Spine") {
		for _, archive := range archives {
			if !appendLine("- " + truncateStorySpineDatabaseContext(archive.OutlineTitle, 512) + ": " + truncateStorySpineDatabaseContext(archive.Summary, 1800)) {
				break
			}
		}
	}
	if len(nodes) > 0 && appendLine("## Worldview") {
		for _, node := range nodes {
			if !appendLine("- [" + node.Kind + "] " + truncateStorySpineDatabaseContext(node.Title, 512) + ": " + truncateStorySpineDatabaseContext(node.Content, 1800)) {
				break
			}
		}
	}
	return strings.TrimSpace(builder.String()), nil
}

func (r *AgentRepository) SearchStorySpineDatabase(ctx context.Context, workID, query string, limit int) ([]appagent.StorySpineSearchResult, error) {
	statement := `SELECT archive.id,archive.chapter_outline_node_id,archive.outline_title,archive.summary || COALESCE((SELECT '\n' || GROUP_CONCAT(section.summary, '\n') FROM chapter_archive_sections section WHERE section.archive_id=archive.id),'') FROM chapter_archives archive WHERE archive.work_id=? AND archive.is_current=1`
	arguments := []any{workID}
	if query != "" {
		pattern := "%" + query + "%"
		statement += ` AND (archive.outline_title LIKE ? OR archive.summary LIKE ? OR archive.outline_content LIKE ? OR EXISTS (SELECT 1 FROM chapter_archive_sections section WHERE section.archive_id=archive.id AND section.summary LIKE ?))`
		arguments = append(arguments, pattern, pattern, pattern, pattern)
	}
	statement += ` ORDER BY archive.created_at DESC,archive.revision DESC,archive.id DESC LIMIT ?`
	arguments = append(arguments, limit)
	rows, err := r.database.WithContext(ctx).Raw(statement, arguments...).Rows()
	if err != nil {
		return nil, fmt.Errorf("search story spine database: %w", err)
	}
	results := make([]appagent.StorySpineSearchResult, 0, limit)
	for rows.Next() {
		var result appagent.StorySpineSearchResult
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
	var incoming []canvasEdgeModel
	if err := r.database.Where("work_id = ? AND target_node_id = ? AND kind = ?", workID, sectionOutlineNodeID, "generated_from").Order("created_at").Find(&incoming).Error; err != nil {
		return nil, err
	}
	sourceIDs := make([]string, len(incoming))
	for i, edge := range incoming {
		sourceIDs[i] = edge.SourceNodeID
	}
	var chapters []canvasNodeModel
	if len(sourceIDs) > 0 {
		if err := r.database.Select("id").Where("work_id = ? AND id IN ? AND kind = ?", workID, sourceIDs, canvas.NodeKindChapterOutline).Find(&chapters).Error; err != nil {
			return nil, err
		}
	}
	chapterSet := make(map[string]struct{}, len(chapters))
	for _, chapter := range chapters {
		chapterSet[chapter.ID] = struct{}{}
	}
	chapterOutlineNodeIDs := make([]string, 0, len(chapters))
	for _, id := range sourceIDs {
		if _, ok := chapterSet[id]; ok {
			chapterOutlineNodeIDs = append(chapterOutlineNodeIDs, id)
		}
	}
	if len(chapterOutlineNodeIDs) == 0 {
		return nil, fmt.Errorf("%w: section outline has no chapter outline parent", canvas.ErrInvalidNode)
	}
	contextNodeIDs = append(contextNodeIDs, chapterOutlineNodeIDs...)
	var inherited []canvasEdgeModel
	if err := r.database.Select("source_node_id").Where("work_id = ? AND target_node_id IN ? AND kind = ?", workID, chapterOutlineNodeIDs, "generated_from").Order("created_at").Find(&inherited).Error; err != nil {
		return nil, err
	}
	for _, edge := range inherited {
		contextNodeIDs = append(contextNodeIDs, edge.SourceNodeID)
	}
	return uniqueStrings(contextNodeIDs), nil
}

func (r *AgentRepository) GetChapterArchiveContext(workID, chapterOutlineNodeID string) ([]string, error) {
	contextNodeIDs := []string{chapterOutlineNodeID}
	var inherited, childEdges []canvasEdgeModel
	if err := r.database.Where("work_id = ? AND target_node_id = ? AND kind = ?", workID, chapterOutlineNodeID, "generated_from").Order("created_at").Find(&inherited).Error; err != nil {
		return nil, err
	}
	for _, edge := range inherited {
		contextNodeIDs = append(contextNodeIDs, edge.SourceNodeID)
	}
	if err := r.database.Where("work_id = ? AND source_node_id = ? AND kind = ?", workID, chapterOutlineNodeID, "generated_from").Order("created_at").Find(&childEdges).Error; err != nil {
		return nil, err
	}
	childIDs := make([]string, len(childEdges))
	for i, edge := range childEdges {
		childIDs[i] = edge.TargetNodeID
	}
	var children []canvasNodeModel
	if len(childIDs) > 0 {
		if err := r.database.Select("id", "kind").Where("work_id = ? AND id IN ? AND kind IN ?", workID, childIDs, []canvas.NodeKind{canvas.NodeKindSectionOutline, canvas.NodeKindChapterSection}).Find(&children).Error; err != nil {
			return nil, err
		}
	}
	sectionIDs := make([]string, 0)
	for _, child := range children {
		contextNodeIDs = append(contextNodeIDs, child.ID)
		if canvas.NodeKind(child.Kind) == canvas.NodeKindSectionOutline {
			sectionIDs = append(sectionIDs, child.ID)
		}
	}
	if len(sectionIDs) > 0 {
		var sectionEdges []canvasEdgeModel
		if err := r.database.Where("work_id = ? AND source_node_id IN ? AND kind = ?", workID, sectionIDs, "generated_from").Find(&sectionEdges).Error; err != nil {
			return nil, err
		}
		ids := make([]string, len(sectionEdges))
		for i, edge := range sectionEdges {
			ids[i] = edge.TargetNodeID
		}
		var sections []canvasNodeModel
		if len(ids) > 0 {
			if err := r.database.Select("id").Where("work_id = ? AND id IN ? AND kind = ?", workID, ids, canvas.NodeKindChapterSection).Find(&sections).Error; err != nil {
				return nil, err
			}
		}
		for _, section := range sections {
			contextNodeIDs = append(contextNodeIDs, section.ID)
		}
	}
	return uniqueStrings(contextNodeIDs), nil
}

func (r *AgentRepository) CreateRun(input appagent.RunInput) (appagent.Run, error) {
	now := time.Now().UTC()
	run := appagent.Run{
		ID: input.RunID, WorkID: input.WorkID, Status: appagent.RunStatusQueued,
		Prompt: input.Prompt, Target: input.Target, TargetNodeID: input.TargetNodeID, TargetNodeRevision: input.TargetNodeRevision, ProviderID: input.ProviderID, ModelID: input.ModelID, ConversationSessionID: input.ConversationSessionID,
		ContextNodeIDs: append([]string{}, input.ContextNodeIDs...), CreatedAt: now, UpdatedAt: now,
	}
	model := runModelFromDomain(run)
	err := r.database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model).Error; err != nil {
			return fmt.Errorf("insert agent run: %w", err)
		}
		_, err := appendEvent(tx, run.ID, appagent.EventRunQueued, map[string]any{"status": run.Status}, now)
		return err
	})
	return run, err
}

func (r *AgentRepository) GetRun(runID string) (appagent.Run, error) {
	var model agentRunModel
	if err := r.database.First(&model, "id = ?", runID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return appagent.Run{}, appagent.ErrRunNotFound
	} else if err != nil {
		return appagent.Run{}, err
	}
	return runFromModel(model), nil
}

func (r *AgentRepository) ListInterruptedRuns() ([]appagent.Run, error) {
	var models []agentRunModel
	if err := r.database.Where("status IN ?", []appagent.RunStatus{appagent.RunStatusQueued, appagent.RunStatusRunning}).Order("created_at, id").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list interrupted agent runs: %w", err)
	}
	runs := make([]appagent.Run, len(models))
	for index, model := range models {
		runs[index] = runFromModel(model)
	}
	return runs, nil
}

func (r *AgentRepository) ListEvents(runID string, afterSequence int64) ([]appagent.Event, error) {
	var models []agentRunEventModel
	if err := r.database.Where("run_id = ? AND sequence > ?", runID, afterSequence).Order("sequence").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("query agent events: %w", err)
	}
	events := make([]appagent.Event, len(models))
	for i, model := range models {
		events[i] = eventFromModel(model)
	}
	return events, nil
}

func (r *AgentRepository) AppendEvent(runID string, eventType appagent.EventType, data any) (appagent.Event, error) {
	var event appagent.Event
	err := r.database.Transaction(func(tx *gorm.DB) error {
		var err error
		event, err = appendEvent(tx, runID, eventType, data, time.Now().UTC())
		if err != nil {
			return err
		}
		if eventType == appagent.EventApprovalRequired {
			result := tx.Model(&agentRunModel{}).Where("id = ? AND status = ?", runID, appagent.RunStatusRunning).Updates(map[string]any{"status": appagent.RunStatusWaitingInput, "error_message": "", "updated_at": event.Timestamp})
			if result.Error != nil {
				return fmt.Errorf("wait for agent input: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				return appagent.ErrRunNotCancellable
			}
		}
		return nil
	})
	return event, err
}

func (r *AgentRepository) MarkStarted(runID string) error {
	return r.transitionRun(runID, appagent.RunStatusQueued, appagent.RunStatusRunning, appagent.EventRunStarted)
}

func (r *AgentRepository) QueueResponse(runID, approvalEventID, answer string) (appagent.UserResponse, error) {
	var response appagent.UserResponse
	err := r.database.Transaction(func(tx *gorm.DB) error {
		var run agentRunModel
		if err := tx.Select("status").First(&run, "id = ?", runID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return appagent.ErrRunNotFound
		} else if err != nil {
			return err
		}
		if appagent.RunStatus(run.Status) != appagent.RunStatusWaitingInput {
			return appagent.ErrRunNotWaitingInput
		}
		var event agentRunEventModel
		if err := tx.Where("run_id = ? AND type = ?", runID, appagent.EventApprovalRequired).Order("sequence DESC").First(&event).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return appagent.ErrInvalidUserResponse
		} else if err != nil {
			return err
		}
		if event.ID != approvalEventID {
			return appagent.ErrInvalidUserResponse
		}
		var approval struct {
			Question string `json:"question"`
			Reason   string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(event.DataJSON), &approval); err != nil {
			return fmt.Errorf("decode pending agent question: %w", err)
		}
		response = appagent.UserResponse{
			ApprovalEventID: approvalEventID, Question: approval.Question, Answer: answer, Reason: approval.Reason,
		}
		now := time.Now().UTC()
		if _, err := appendEvent(tx, runID, appagent.EventUserResponseReceived, response, now); err != nil {
			return err
		}
		result := tx.Model(&agentRunModel{}).Where("id = ? AND status = ?", runID, appagent.RunStatusWaitingInput).Updates(map[string]any{"status": appagent.RunStatusQueued, "error_message": "", "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return appagent.ErrRunNotWaitingInput
		}
		return nil
	})
	return response, err
}

func (r *AgentRepository) MarkResumed(runID string) error {
	return r.transitionRun(runID, appagent.RunStatusQueued, appagent.RunStatusRunning, appagent.EventRunResumed)
}

func (r *AgentRepository) MarkRecovered(runID string) error {
	return r.transitionRunFromStatuses(runID, []appagent.RunStatus{appagent.RunStatusQueued, appagent.RunStatusRunning}, appagent.RunStatusRunning, appagent.EventRunRecovered)
}

func (r *AgentRepository) ListUserResponses(runID string) ([]appagent.UserResponse, error) {
	events, err := r.ListEvents(runID, 0)
	if err != nil {
		return nil, err
	}
	responses := make([]appagent.UserResponse, 0)
	for _, event := range events {
		if event.Type != appagent.EventUserResponseReceived {
			continue
		}
		var response appagent.UserResponse
		if err := json.Unmarshal(event.Data, &response); err != nil {
			return nil, fmt.Errorf("decode agent user response: %w", err)
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func (r *AgentRepository) Complete(run appagent.Run, result appagent.RunResult) (appagent.Candidate, error) {
	now := time.Now().UTC()
	candidate := appagent.Candidate{
		ID: result.CandidateID, RunID: run.ID, WorkID: run.WorkID,
		SkillID: result.SkillID, SkillVersion: result.SkillVersion, Content: result.Content, CreatedAt: now,
	}
	err := r.database.Transaction(func(tx *gorm.DB) error {
		completed, err := productProjectionCompleted(tx, run.ID, result)
		if err != nil || completed {
			return err
		}
		if err := completeRun(tx, run.ID, now); err != nil {
			return err
		}
		if err := completeProductProjection(tx, run.ID, result, now); err != nil {
			return err
		}
		_, err = appendEvent(tx, run.ID, appagent.EventRunCompleted, map[string]any{"candidateId": candidate.ID}, now)
		return err
	})
	return candidate, err
}

// CompleteReadOnly finishes a work-level exploration without creating a
// canvas candidate. The result is already exposed through message.delta; this
// event only closes the durable run lifecycle.
func (r *AgentRepository) CompleteReadOnly(run appagent.Run, result appagent.RunResult) error {
	now := time.Now().UTC()
	return r.database.Transaction(func(tx *gorm.DB) error {
		completed, err := productProjectionCompleted(tx, run.ID, result)
		if err != nil || completed {
			return err
		}
		if err := completeRun(tx, run.ID, now); err != nil {
			return err
		}
		if err := completeProductProjection(tx, run.ID, result, now); err != nil {
			return err
		}
		if message := strings.TrimSpace(result.Message); message != "" {
			if _, err := appendEvent(tx, run.ID, appagent.EventMessageDelta, map[string]string{"delta": message}, now); err != nil {
				return err
			}
		}
		_, err = appendEvent(tx, run.ID, appagent.EventRunCompleted, map[string]any{"mode": "read-only", "role": result.Role, "skillId": result.SkillID}, now)
		return err
	})
}

// CompleteCollaborativeProposal persists proposed nodes and existing-node
// revisions as independent pending candidates. The proposal remains
// reviewable; accepting a candidate is what changes the durable canvas.
func (r *AgentRepository) CompleteCollaborativeProposal(run appagent.Run, result appagent.RunResult) error {
	var proposal appagent.ProposalSet
	decoder := json.NewDecoder(strings.NewReader(result.Content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil {
		return fmt.Errorf("%w: decode collaborative proposal: %v", appagent.ErrProjectionTerminal, err)
	}
	if len(proposal.Nodes) == 0 && len(proposal.Updates) == 0 {
		return fmt.Errorf("%w: collaborative proposal has no nodes or updates", appagent.ErrProjectionTerminal)
	}
	if len(proposal.Nodes) > appagent.MaxProposalNodes {
		return fmt.Errorf("%w: collaborative proposal exceeds %d nodes", appagent.ErrProjectionTerminal, appagent.MaxProposalNodes)
	}
	now := time.Now().UTC()
	return r.database.Transaction(func(tx *gorm.DB) error {
		completed, err := productProjectionCompleted(tx, run.ID, result)
		if err != nil || completed {
			return err
		}
		created := make([]map[string]any, 0, len(proposal.Nodes)+len(proposal.Updates))
		models := make([]agentCandidateModel, 0, len(proposal.Nodes)+len(proposal.Updates))
		clientCandidates := make(map[string]string, len(proposal.Nodes))
		var pending int64
		if err := tx.Model(&agentCandidateModel{}).
			Where("work_id = ? AND status = ?", run.WorkID, appagent.CandidateStatusPending).
			Count(&pending).Error; err != nil {
			return fmt.Errorf("count pending collaborative candidates: %w", err)
		}
		clientIDs := make(map[string]struct{}, len(proposal.Nodes))
		for index, node := range proposal.Nodes {
			clientID := strings.TrimSpace(node.ClientID)
			kind, validKind := canvas.ParseNodeKind(node.Kind)
			title, content := strings.TrimSpace(node.Title), strings.TrimSpace(node.Content)
			if clientID == "" || title == "" || content == "" {
				return fmt.Errorf("%w: collaborative proposal node %d is incomplete", appagent.ErrProjectionTerminal, index)
			}
			if !validKind || !canvas.IsManuallyCreatableNodeKind(kind) {
				return fmt.Errorf("%w: collaborative proposal node %d has unsupported creatable kind %q", appagent.ErrProjectionTerminal, index, node.Kind)
			}
			if _, exists := clientIDs[clientID]; exists {
				return fmt.Errorf("%w: collaborative proposal node %d repeats clientId %q", appagent.ErrProjectionTerminal, index, clientID)
			}
			clientIDs[clientID] = struct{}{}
			kindValue := string(kind)
			candidateID := uuid.NewString()
			x := 520 + float64(pending%8)*36
			y := 80 + float64(pending/8)*220
			models = append(models, agentCandidateModel{ID: candidateID, RunID: run.ID, WorkID: run.WorkID, SkillID: result.SkillID, SkillVersion: result.SkillVersion, Status: string(appagent.CandidateStatusPending), Kind: kindValue, Title: title, Content: content, X: x, Y: y, CreatedAt: now, CandidateType: "node"})
			clientCandidates[clientID] = candidateID
			created = append(created, map[string]any{"candidateId": candidateID, "candidateType": "node", "clientId": clientID, "kind": kindValue, "title": title, "ordinal": index + 1, "total": len(proposal.Nodes), "x": x, "y": y})
			pending++
		}
		for index, update := range proposal.Updates {
			nodeID := strings.TrimSpace(update.NodeID)
			title, content := strings.TrimSpace(update.Title), strings.TrimSpace(update.Content)
			if nodeID == "" || update.BaseRevision < 1 || title == "" || content == "" {
				return fmt.Errorf("%w: collaborative proposal update %d is incomplete", appagent.ErrProjectionTerminal, index)
			}
			node, err := getCanvasNode(tx, run.WorkID, nodeID)
			if err != nil {
				return fmt.Errorf("collaborative proposal update %d target node: %w", index, err)
			}
			if node.Revision != update.BaseRevision {
				return fmt.Errorf("%w: collaborative proposal update %d targets node %q at revision %d, expected %d", canvas.ErrRevisionConflict, index, nodeID, node.Revision, update.BaseRevision)
			}
			kind, validKind := canvas.ParseNodeKind(normalizeLegacyCanvasKind(node.Kind))
			if !validKind {
				return fmt.Errorf("%w: collaborative proposal update %d targets node %q with unsupported kind %q", appagent.ErrProjectionTerminal, index, nodeID, node.Kind)
			}
			candidateID := uuid.NewString()
			x := node.X + 320 + float64(pending%8)*36
			y := node.Y + float64(pending/8)*36
			models = append(models, agentCandidateModel{
				ID: candidateID, RunID: run.ID, WorkID: run.WorkID,
				SkillID: result.SkillID, SkillVersion: result.SkillVersion,
				Status: string(appagent.CandidateStatusPending), Kind: string(kind),
				Title: title, Content: content, X: x, Y: y, CreatedAt: now,
				CandidateType: "version", NodeID: node.ID, BaseVersionID: node.CurrentVersionID,
				Reason: strings.Join(proposal.Reasons, "\n"),
			})
			created = append(created, map[string]any{"candidateId": candidateID, "candidateType": "version", "nodeId": node.ID, "kind": string(kind), "title": title, "baseRevision": update.BaseRevision, "x": x, "y": y})
			pending++
		}
		if err := tx.Create(&models).Error; err != nil {
			return fmt.Errorf("create collaborative candidates: %w", err)
		}
		if err := persistProposalEdges(tx, run, proposal, clientCandidates, now); err != nil {
			return fmt.Errorf("validate collaborative proposal edges: %w", err)
		}
		if err := completeRun(tx, run.ID, now); err != nil {
			return err
		}
		if err := completeProductProjection(tx, run.ID, result, now); err != nil {
			return err
		}
		for _, metadata := range created {
			if _, err := appendEvent(tx, run.ID, appagent.EventCandidateCreated, map[string]any{"candidateId": metadata["candidateId"], "meta": metadata, "mode": "collaborative-proposal"}, now); err != nil {
				return err
			}
		}
		_, err = appendEvent(tx, run.ID, appagent.EventRunCompleted, map[string]any{"candidateIds": created, "mode": "collaborative-proposal"}, now)
		return err
	})
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

func (r *AgentRepository) CompleteChapterArchive(ctx context.Context, run appagent.Run, result appagent.RunResult) error {
	var decoded archiveResult
	decoder := json.NewDecoder(strings.NewReader(result.Content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("%w: decode chapter archive result: %v", appagent.ErrProjectionTerminal, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: archive result must contain one JSON object", canvas.ErrInvalidChapterArchive)
	}
	decoded.Archive.Summary = strings.TrimSpace(decoded.Archive.Summary)
	if decoded.Archive.Summary == "" {
		return fmt.Errorf("%w: archive summary is required", canvas.ErrInvalidChapterArchive)
	}
	now := time.Now().UTC()
	archiveID := uuid.NewString()
	var archive chapterArchiveModel
	alreadyProjected := false
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		completed, err := productProjectionCompleted(tx, run.ID, result)
		if err != nil {
			return err
		}
		if completed {
			alreadyProjected = true
			return nil
		}
		locked, err := isNodeArchiveLocked(tx, run.WorkID, run.TargetNodeID)
		if err != nil {
			return err
		}
		if locked {
			return canvas.ErrArchivedNodeLocked
		}
		outline, err := getCanvasNode(tx, run.WorkID, run.TargetNodeID)
		if err != nil {
			return err
		}
		if canvas.NodeKind(outline.Kind) != canvas.NodeKindChapterOutline {
			return canvas.ErrInvalidNode
		}
		if outline.Revision != result.ExpectedRevision {
			return fmt.Errorf("%w: chapter outline changed before archive completion", canvas.ErrRevisionConflict)
		}
		sections, err := readChapterArchiveSections(tx, run.WorkID, run.TargetNodeID, decoded.Archive.Sections)
		if err != nil {
			return err
		}
		var revision int64
		if err := tx.Model(&chapterArchiveModel{}).Where("work_id = ? AND chapter_outline_node_id = ?", run.WorkID, run.TargetNodeID).Select("COALESCE(MAX(revision), 0) + 1").Scan(&revision).Error; err != nil {
			return err
		}
		if err := tx.Model(&chapterArchiveModel{}).Where("work_id = ? AND chapter_outline_node_id = ? AND is_current = ?", run.WorkID, run.TargetNodeID, true).Updates(map[string]any{"is_current": false, "superseded_at": now}).Error; err != nil {
			return err
		}
		archive = chapterArchiveModel{ID: archiveID, WorkID: run.WorkID, ChapterOutlineNodeID: run.TargetNodeID, Revision: revision, RunID: run.ID, OutlineVersionID: outline.CurrentVersionID, OutlineRevision: outline.Revision, OutlineTitle: outline.Title, OutlineContent: outline.Content, Summary: decoded.Archive.Summary, SourceDigest: chapterArchiveDigest(run.TargetNodeID, outline.Revision, outline.Content, sections), IsCurrent: true, ProjectionStatus: "pending", CreatedAt: now}
		archive.Sections = make([]chapterArchiveSectionModel, len(sections))
		layoutSections := make([]chapterLayoutSection, len(sections))
		for index, section := range sections {
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(section.Content)))
			archive.Sections[index] = chapterArchiveSectionModel{ArchiveID: archiveID, WorkID: run.WorkID, Ordinal: section.Ordinal, SectionOutlineNodeID: section.SectionOutlineNodeID, ChapterSectionNodeID: section.ChapterSectionNodeID, ChapterSectionVersionID: section.ChapterSectionVersionID, NodeRevision: section.NodeRevision, Title: section.Title, Summary: section.Summary, Content: section.Content, ContentHash: hash}
			layoutSections[index] = chapterLayoutSection{SectionOutlineNodeID: section.SectionOutlineNodeID, ChapterSectionNodeID: section.ChapterSectionNodeID}
		}
		if err := tx.Create(&archive).Error; err != nil {
			return fmt.Errorf("create chapter archive: %w", err)
		}
		positions, _, err := layoutChapterNodes(tx, run.WorkID, run.TargetNodeID, layoutSections)
		if err != nil {
			return fmt.Errorf("layout archived chapter: %w", err)
		}
		if err := applyNodePositions(tx, run.WorkID, positions); err != nil {
			return fmt.Errorf("apply archived chapter layout: %w", err)
		}
		created := make([]string, 0, len(decoded.Proposals))
		for _, proposal := range decoded.Proposals {
			if proposal.NodeID == "" || !containsString(run.ContextNodeIDs, proposal.NodeID) || proposal.NodeID == run.TargetNodeID || strings.TrimSpace(proposal.Content) == "" {
				continue
			}
			node, err := getCanvasNode(tx, run.WorkID, proposal.NodeID)
			if err != nil {
				continue
			}
			if proposal.Kind != "" && proposal.Kind != node.Kind {
				continue
			}
			var pending int64
			if err := tx.Model(&agentCandidateModel{}).Where("work_id = ? AND status = ?", run.WorkID, appagent.CandidateStatusPending).Count(&pending).Error; err != nil {
				return err
			}
			id := uuid.NewString()
			candidate := agentCandidateModel{ID: id, RunID: run.ID, WorkID: run.WorkID, SkillID: result.SkillID, SkillVersion: result.SkillVersion, Status: string(appagent.CandidateStatusPending), Kind: node.Kind, Title: valueOrArchiveTitle(proposal.Title, node.Title), Content: proposal.Content, X: node.X + 320 + float64(pending/8)*36, Y: node.Y + float64(pending%8)*36, CreatedAt: now, CandidateType: "version", NodeID: proposal.NodeID, BaseVersionID: node.CurrentVersionID, Reason: proposal.Reason, ChangeScore: proposal.ChangeScore}
			if err := tx.Create(&candidate).Error; err != nil {
				return err
			}
			created = append(created, id)
		}
		if err := completeRun(tx, run.ID, now); err != nil {
			return err
		}
		if err := completeProductProjection(tx, run.ID, result, now); err != nil {
			return err
		}
		if _, err := appendEvent(tx, run.ID, appagent.EventCandidateCreated, map[string]any{"candidateIds": created, "candidateType": "version"}, now); err != nil {
			return err
		}
		if _, err := appendEvent(tx, run.ID, appagent.EventRunCompleted, map[string]any{"archiveId": archiveID, "candidateIds": created}, now); err != nil {
			return err
		}
		if err := tx.Where("work_id = ?", run.WorkID).Delete(&canvasActionModel{}).Error; err != nil {
			return fmt.Errorf("clear canvas actions at chapter archive checkpoint: %w", err)
		}
		return tx.Where("work_id = ?", run.WorkID).Delete(&canvasHistoryStateModel{}).Error
	})
	if err != nil {
		return err
	}
	if alreadyProjected {
		return nil
	}
	archiveDomain := chapterArchiveFromModel(archive)
	projectionStatus := "ready"
	if err := writeChapterArchiveProjection(r.dataDirectory, archiveDomain); err != nil {
		projectionStatus = "pending"
	}
	_ = r.database.WithContext(ctx).Model(&chapterArchiveModel{}).Where("id = ?", archiveID).Update("projection_status", projectionStatus).Error
	return nil
}

func readChapterArchiveSections(tx *gorm.DB, workID, chapterOutlineNodeID string, summaries []archiveSectionResult) ([]archiveSectionSource, error) {
	rows, err := tx.Raw(`
SELECT so.id,cs.id,cs.current_version_id,cs.revision,cs.title,cs.content
FROM canvas_edges chapter_edge
JOIN canvas_nodes so
  ON so.work_id=chapter_edge.work_id AND so.id=chapter_edge.target_node_id AND so.kind=?
LEFT JOIN canvas_edges section_edge
  ON section_edge.work_id=chapter_edge.work_id AND section_edge.source_node_id=so.id AND section_edge.kind='generated_from'
LEFT JOIN canvas_nodes cs
  ON cs.work_id=section_edge.work_id AND cs.id=section_edge.target_node_id AND cs.kind=?
WHERE chapter_edge.work_id=? AND chapter_edge.source_node_id=? AND chapter_edge.kind='generated_from'`, canvas.NodeKindSectionOutline, canvas.NodeKindChapterSection, workID, chapterOutlineNodeID).Rows()
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
	run appagent.Run,
	nodeID string,
	result appagent.RunResult,
) error {
	now := time.Now().UTC()
	return r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		completed, err := productProjectionCompleted(tx, run.ID, result)
		if err != nil || completed {
			return err
		}
		locked, err := isNodeArchiveLocked(tx, run.WorkID, nodeID)
		if err != nil {
			return err
		}
		if locked {
			return canvas.ErrArchivedNodeLocked
		}
		before, err := getCanvasNode(tx, run.WorkID, nodeID)
		if err != nil {
			return err
		}
		if before.Revision != result.ExpectedRevision {
			return fmt.Errorf("%w: target node %q changed before agent update", canvas.ErrRevisionConflict, nodeID)
		}
		update := tx.Model(&canvasNodeModel{}).Where("work_id = ? AND id = ? AND revision = ?", run.WorkID, nodeID, result.ExpectedRevision).Updates(map[string]any{"title": result.Title, "content": result.Content, "revision": gorm.Expr("revision + 1"), "updated_at": now})
		if update.Error != nil {
			return fmt.Errorf("update canvas node from agent run: %w", update.Error)
		}
		if update.RowsAffected == 0 {
			return canvas.ErrRevisionConflict
		}
		if before.Title != result.Title || before.Content != result.Content {
			if err := appendCanvasAction(tx, run.WorkID, actionUpdateNode, "Agent 更新节点", updateNodeActionPayload{Before: nodeContentState{NodeID: before.ID, Title: before.Title, Content: before.Content}, After: nodeContentState{NodeID: before.ID, Title: result.Title, Content: result.Content}}); err != nil {
				return err
			}
		}
		if err := completeRun(tx, run.ID, now); err != nil {
			return err
		}
		if err := completeProductProjection(tx, run.ID, result, now); err != nil {
			return err
		}
		if _, err := appendEvent(tx, run.ID, appagent.EventNodeUpdated, map[string]any{"nodeId": nodeID}, now); err != nil {
			return err
		}
		_, err = appendEvent(tx, run.ID, appagent.EventRunCompleted, map[string]any{"nodeId": nodeID}, now)
		return err
	})
}

func (r *AgentRepository) CompleteDerivation(
	ctx context.Context,
	run appagent.Run,
	parentNodeID string,
	result appagent.RunResult,
) error {
	now := time.Now().UTC()
	return r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		completed, err := productProjectionCompleted(tx, run.ID, result)
		if err != nil || completed {
			return err
		}
		parentModel, err := getCanvasNode(tx, run.WorkID, parentNodeID)
		if err != nil {
			return err
		}
		parent := canvasNodeFromModel(parentModel)
		if parent.Revision != result.ExpectedRevision {
			return fmt.Errorf("%w: parent node %q changed before derivation", canvas.ErrRevisionConflict, parentNodeID)
		}
		derivedKind := canvas.NodeKindSectionOutline
		if run.Target == appagent.TargetChapterSection {
			derivedKind = canvas.NodeKindChapterSection
		}
		var existing int64
		if err := tx.Model(&canvasEdgeModel{}).Joins("JOIN canvas_nodes ON canvas_nodes.work_id = canvas_edges.work_id AND canvas_nodes.id = canvas_edges.target_node_id").Where("canvas_edges.work_id = ? AND canvas_edges.source_node_id = ? AND canvas_edges.kind = ? AND canvas_nodes.kind = ?", run.WorkID, parentNodeID, "generated_from", derivedKind).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return canvas.ErrDerivationExists
		}
		inputs, err := derivationNodeInputs(run, parent, result.Content)
		if err != nil {
			return err
		}
		nodeModels := make([]canvasNodeModel, 0, len(inputs))
		edgeModels := make([]canvasEdgeModel, 0, len(inputs)*2)
		startY := parent.Y - float64(len(inputs)-1)*110
		for index, input := range inputs {
			node := canvasNodeModel{ID: uuid.NewString(), WorkID: run.WorkID, Revision: 1, Kind: string(input.Kind), Title: input.Title, Content: input.Content, X: parent.X + 360, Y: startY + float64(index)*220, CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&node).Error; err != nil {
				return fmt.Errorf("create derived canvas node: %w", err)
			}
			if _, err := createInitialNodeVersionModel(tx, &node); err != nil {
				return err
			}
			sources := uniqueStrings(append([]string{parentNodeID}, input.ContextNodeIDs...))
			for _, sourceID := range sources {
				if !containsString(run.ContextNodeIDs, sourceID) {
					return fmt.Errorf("%w: derived context node %q was not supplied to the run", canvas.ErrInvalidSectionOutline, sourceID)
				}
				var count int64
				if err := tx.Model(&canvasNodeModel{}).Where("work_id = ? AND id = ?", run.WorkID, sourceID).Count(&count).Error; err != nil {
					return err
				}
				if count == 0 {
					return canvas.ErrNodeNotFound
				}
				edgeModels = append(edgeModels, canvasEdgeModel{ID: uuid.NewString(), WorkID: run.WorkID, SourceNodeID: sourceID, TargetNodeID: node.ID, Kind: "generated_from", CreatedAt: now})
			}
			nodeModels = append(nodeModels, node)
		}
		if len(edgeModels) > 0 {
			if err := tx.Create(&edgeModels).Error; err != nil {
				return err
			}
		}
		if err := appendCanvasAction(tx, run.WorkID, actionCreateNodes, "Agent 派生节点", createNodesActionPayload{Nodes: canvasNodesFromModels(nodeModels), Edges: canvasEdgesFromModels(edgeModels)}); err != nil {
			return err
		}
		if err := completeRun(tx, run.ID, now); err != nil {
			return err
		}
		if err := completeProductProjection(tx, run.ID, result, now); err != nil {
			return err
		}
		ids := make([]string, len(nodeModels))
		for i, node := range nodeModels {
			ids[i] = node.ID
		}
		if _, err := appendEvent(tx, run.ID, appagent.EventNodesCreated, map[string]any{"parentNodeId": parentNodeID, "nodeIds": ids, "kind": derivedKind}, now); err != nil {
			return err
		}
		_, err = appendEvent(tx, run.ID, appagent.EventRunCompleted, map[string]any{"parentNodeId": parentNodeID, "nodeIds": ids}, now)
		return err
	})
}

type derivationNodeInput struct {
	Kind           canvas.NodeKind
	Title          string
	Content        string
	ContextNodeIDs []string
}

func derivationNodeInputs(run appagent.Run, parent canvas.Node, content string) ([]derivationNodeInput, error) {
	if run.Target == appagent.TargetSectionOutlineBatch {
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
	if run.Target != appagent.TargetChapterSection || parent.Kind != canvas.NodeKindSectionOutline {
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
		ContextNodeIDs: append([]string{}, run.ContextNodeIDs...),
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
	return r.setStatusWithEvent(runID, appagent.RunStatusFailed, message, appagent.EventRunFailed, map[string]string{"message": message})
}

func (r *AgentRepository) Cancel(runID string) error {
	return r.database.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Model(&agentRunModel{}).Where("id = ? AND status IN ?", runID, []appagent.RunStatus{appagent.RunStatusQueued, appagent.RunStatusRunning, appagent.RunStatusWaitingInput}).Updates(map[string]any{"status": appagent.RunStatusCancelled, "updated_at": now})
		if result.Error != nil {
			return fmt.Errorf("cancel agent run: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return runMutationError(tx, runID)
		}
		if err := tx.Model(&agentProductProjectionModel{}).
			Where("run_id = ? AND status = ?", runID, appagent.ProductProjectionPending).
			Updates(map[string]any{
				"status": appagent.ProductProjectionFailed, "last_error": "run cancelled",
				"updated_at": now,
			}).Error; err != nil {
			return fmt.Errorf("cancel pending agent product projections: %w", err)
		}
		_, err := appendEvent(tx, runID, appagent.EventRunCancelled, nil, now)
		return err
	})
}

func (r *AgentRepository) setStatusWithEvent(runID string, status appagent.RunStatus, message string, eventType appagent.EventType, data any) error {
	return r.database.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Model(&agentRunModel{}).Where("id = ? AND status IN ?", runID, []appagent.RunStatus{appagent.RunStatusQueued, appagent.RunStatusRunning}).Updates(map[string]any{"status": status, "error_message": message, "updated_at": now})
		if result.Error != nil {
			return fmt.Errorf("update agent run: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return runMutationError(tx, runID)
		}
		if status == appagent.RunStatusFailed {
			if err := tx.Model(&agentProductProjectionModel{}).
				Where("run_id = ? AND status = ?", runID, appagent.ProductProjectionPending).
				Updates(map[string]any{
					"status": appagent.ProductProjectionFailed, "last_error": message,
					"updated_at": now,
				}).Error; err != nil {
				return fmt.Errorf("fail pending agent product projections: %w", err)
			}
		}
		_, err := appendEvent(tx, runID, eventType, data, now)
		return err
	})
}

func (r *AgentRepository) transitionRun(runID string, from, to appagent.RunStatus, eventType appagent.EventType) error {
	return r.database.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Model(&agentRunModel{}).Where("id = ? AND status = ?", runID, from).Updates(map[string]any{"status": to, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return appagent.ErrRunNotCancellable
		}
		_, err := appendEvent(tx, runID, eventType, nil, now)
		return err
	})
}

func (r *AgentRepository) transitionRunFromStatuses(runID string, from []appagent.RunStatus, to appagent.RunStatus, eventType appagent.EventType) error {
	return r.database.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Model(&agentRunModel{}).Where("id = ? AND status IN ?", runID, from).Updates(map[string]any{"status": to, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return appagent.ErrRunNotCancellable
		}
		_, err := appendEvent(tx, runID, eventType, nil, now)
		return err
	})
}

func completeRun(tx *gorm.DB, runID string, now time.Time) error {
	result := tx.Model(&agentRunModel{}).Where("id = ? AND status = ?", runID, appagent.RunStatusRunning).Updates(map[string]any{"status": appagent.RunStatusCompleted, "updated_at": now})
	if result.Error != nil {
		return fmt.Errorf("complete agent run: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return appagent.ErrRunNotCancellable
	}
	return nil
}

func runMutationError(tx *gorm.DB, runID string) error {
	var count int64
	if err := tx.Model(&agentRunModel{}).Where("id = ?", runID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return appagent.ErrRunNotFound
	}
	return appagent.ErrRunNotCancellable
}

func appendEvent(tx *gorm.DB, runID string, eventType appagent.EventType, data any, now time.Time) (appagent.Event, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return appagent.Event{}, fmt.Errorf("encode agent event: %w", err)
	}
	var sequence int64
	if err := tx.Model(&agentRunEventModel{}).Where("run_id = ?", runID).Select("COALESCE(MAX(sequence), 0) + 1").Scan(&sequence).Error; err != nil {
		return appagent.Event{}, fmt.Errorf("allocate agent event sequence: %w", err)
	}
	model := agentRunEventModel{ID: uuid.NewString(), RunID: runID, Sequence: sequence, Type: string(eventType), DataJSON: string(dataJSON), CreatedAt: now}
	if err := tx.Create(&model).Error; err != nil {
		return appagent.Event{}, fmt.Errorf("insert agent event: %w", err)
	}
	return eventFromModel(model), nil
}

func runFromModel(model agentRunModel) appagent.Run {
	return appagent.Run{ID: model.ID, WorkID: model.WorkID, Status: appagent.RunStatus(model.Status), Prompt: model.Prompt, Target: model.Target, TargetNodeID: model.TargetNodeID, TargetNodeRevision: model.TargetNodeRevision, ProviderID: model.ProviderID, ModelID: model.ModelID, ConversationSessionID: model.ConversationSessionID, ContextNodeIDs: append([]string{}, model.ContextNodeIDs...), ErrorMessage: model.ErrorMessage, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
}

func runModelFromDomain(run appagent.Run) agentRunModel {
	return agentRunModel{ID: run.ID, WorkID: run.WorkID, Status: string(run.Status), Prompt: run.Prompt, Target: run.Target, TargetNodeID: run.TargetNodeID, TargetNodeRevision: run.TargetNodeRevision, ProviderID: run.ProviderID, ModelID: run.ModelID, ConversationSessionID: run.ConversationSessionID, ContextNodeIDs: append([]string{}, run.ContextNodeIDs...), ErrorMessage: run.ErrorMessage, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt}
}

func eventFromModel(model agentRunEventModel) appagent.Event {
	return appagent.Event{ID: model.ID, RunID: model.RunID, Sequence: model.Sequence, Type: appagent.EventType(model.Type), Timestamp: model.CreatedAt, Data: json.RawMessage(model.DataJSON)}
}

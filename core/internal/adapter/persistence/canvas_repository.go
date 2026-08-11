package persistence

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	agent "warmmo/core/internal/application/agent"
	"warmmo/core/internal/domain/canvas"
)

type CanvasRepository struct {
	database      *gorm.DB
	dataDirectory string
}

func NewCanvasRepository(providerRepository *ProviderRepository) *CanvasRepository {
	return NewCanvasRepositoryWithDatabase(providerRepository.databaseHost)
}

func NewCanvasRepositoryWithDatabase(database *Database) *CanvasRepository {
	return &CanvasRepository{database: database.DB, dataDirectory: database.DataDirectory()}
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
	model := canvasNodeModel{ID: uuid.NewString(), WorkID: input.WorkID, Revision: 1, Kind: string(input.Kind), Title: input.Title, Content: input.Content, X: input.X, Y: input.Y, CreatedAt: now, UpdatedAt: now}
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model).Error; err != nil {
			return fmt.Errorf("create canvas node: %w", err)
		}
		if _, err := createInitialNodeVersionModel(tx, &model); err != nil {
			return err
		}
		contextIDs := uniqueStrings(input.ContextNodeIDs)
		if len(contextIDs) > 0 {
			var count int64
			if err := tx.Model(&canvasNodeModel{}).Where("work_id = ? AND id IN ?", model.WorkID, contextIDs).Count(&count).Error; err != nil {
				return fmt.Errorf("read canvas context nodes: %w", err)
			}
			if count != int64(len(contextIDs)) {
				return canvas.ErrNodeNotFound
			}
		}
		edges := make([]canvasEdgeModel, 0, len(contextIDs))
		for _, sourceID := range contextIDs {
			if sourceID != model.ID {
				edges = append(edges, canvasEdgeModel{ID: uuid.NewString(), WorkID: model.WorkID, SourceNodeID: sourceID, TargetNodeID: model.ID, Kind: "generated_from", CreatedAt: now})
			}
		}
		if len(edges) > 0 {
			if err := tx.Create(&edges).Error; err != nil {
				return fmt.Errorf("create canvas context edges: %w", err)
			}
		}
		return appendCanvasAction(tx, model.WorkID, actionCreateNodes, "创建节点", createNodesActionPayload{Nodes: []canvas.Node{canvasNodeFromModel(model)}, Edges: canvasEdgesFromModels(edges)})
	})
	if err != nil {
		return canvas.Node{}, err
	}
	return canvasNodeFromModel(model), nil
}

func createInitialNodeVersionModel(tx *gorm.DB, node *canvasNodeModel) (string, error) {
	version := canvasNodeVersionModel{ID: uuid.NewString(), NodeID: node.ID, WorkID: node.WorkID, VersionNumber: 1, Title: node.Title, Content: node.Content, CreatedAt: node.CreatedAt}
	if err := tx.Create(&version).Error; err != nil {
		return "", fmt.Errorf("create initial node version: %w", err)
	}
	if err := tx.Model(node).Update("current_version_id", version.ID).Error; err != nil {
		return "", fmt.Errorf("set initial node version: %w", err)
	}
	node.CurrentVersionID = version.ID
	return version.ID, nil
}

func createNodeVersionModel(tx *gorm.DB, node *canvasNodeModel, parentVersionID, sourceRunID string) (string, error) {
	var next int64
	if err := tx.Model(&canvasNodeVersionModel{}).Where("work_id = ? AND node_id = ?", node.WorkID, node.ID).Select("COALESCE(MAX(version_number), 0) + 1").Scan(&next).Error; err != nil {
		return "", fmt.Errorf("read next canvas node version: %w", err)
	}
	version := canvasNodeVersionModel{ID: uuid.NewString(), NodeID: node.ID, WorkID: node.WorkID, VersionNumber: next, ParentVersionID: parentVersionID, Title: node.Title, Content: node.Content, SourceRunID: sourceRunID, CreatedAt: node.UpdatedAt}
	if err := tx.Create(&version).Error; err != nil {
		return "", fmt.Errorf("create canvas node version: %w", err)
	}
	if err := tx.Model(node).Update("current_version_id", version.ID).Error; err != nil {
		return "", fmt.Errorf("set current canvas node version: %w", err)
	}
	node.CurrentVersionID = version.ID
	return version.ID, nil
}

func candidateByID(tx *gorm.DB, workID, candidateID string) (agent.Candidate, error) {
	var model agentCandidateModel
	if err := tx.Where("work_id = ? AND id = ?", workID, candidateID).First(&model).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return agent.Candidate{}, canvas.ErrCandidateNotFound
	} else if err != nil {
		return agent.Candidate{}, fmt.Errorf("read canvas candidate: %w", err)
	}
	return candidateFromModel(tx, model)
}

func candidateFromModel(db *gorm.DB, model agentCandidateModel) (agent.Candidate, error) {
	var run agentRunModel
	if err := db.Select("id", "context_node_ids_json").First(&run, "id = ?", model.RunID).Error; err != nil {
		return agent.Candidate{}, fmt.Errorf("read candidate run: %w", err)
	}
	return candidateFromStoredModel(model, run.ContextNodeIDs), nil
}

func (r *CanvasRepository) initialCandidatePosition(tx *gorm.DB, workID string, contextNodeIDs []string) (float64, float64, error) {
	x, y := 520.0, 80.0
	if len(contextNodeIDs) > 0 {
		var nodes []canvasNodeModel
		if err := tx.Select("id", "x", "y").Where("work_id = ? AND id IN ?", workID, contextNodeIDs).Find(&nodes).Error; err != nil {
			return 0, 0, fmt.Errorf("read candidate context positions: %w", err)
		}
		if len(nodes) > 0 {
			maxX, totalY := -math.MaxFloat64, 0.0
			for _, node := range nodes {
				maxX = math.Max(maxX, node.X)
				totalY += node.Y
			}
			x, y = maxX+320, totalY/float64(len(nodes))
		}
	}
	var pending int64
	if err := tx.Model(&agentCandidateModel{}).Where("work_id = ? AND status = ?", workID, agent.CandidateStatusPending).Count(&pending).Error; err != nil {
		return 0, 0, fmt.Errorf("count pending canvas candidates: %w", err)
	}
	x += float64(pending/8) * 36
	y += float64(pending%8) * 36
	return x, y, nil
}

func (r *CanvasRepository) candidateMutationError(ctx context.Context, workID, candidateID string) error {
	var model agentCandidateModel
	err := r.database.WithContext(ctx).Select("status").Where("work_id = ? AND id = ?", workID, candidateID).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return canvas.ErrCandidateNotFound
	}
	if err != nil {
		return fmt.Errorf("read canvas candidate status: %w", err)
	}
	return canvas.ErrCandidateResolved
}

func candidateTitle(content, kind string) string {
	for _, line := range strings.Split(content, "\n") {
		title := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#*-"))
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
	var models []canvasNodeModel
	if err := r.database.WithContext(ctx).Where("work_id = ?", workID).Order("created_at").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list canvas nodes: %w", err)
	}
	return canvasNodesFromModels(models), nil
}

func (r *CanvasRepository) GetNode(ctx context.Context, workID, nodeID string) (canvas.Node, error) {
	model, err := getCanvasNode(r.database.WithContext(ctx), workID, nodeID)
	if err != nil {
		return canvas.Node{}, err
	}
	return canvasNodeFromModel(model), nil
}

func getCanvasNode(db *gorm.DB, workID, nodeID string) (canvasNodeModel, error) {
	var model canvasNodeModel
	if err := db.Where("work_id = ? AND id = ?", workID, nodeID).First(&model).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return canvasNodeModel{}, canvas.ErrNodeNotFound
	} else if err != nil {
		return canvasNodeModel{}, fmt.Errorf("get canvas node: %w", err)
	}
	return model, nil
}

func (r *CanvasRepository) GetNodes(ctx context.Context, workID string, nodeIDs []string) ([]canvas.Node, error) {
	if len(nodeIDs) == 0 {
		return []canvas.Node{}, nil
	}
	if len(nodeIDs) > 64 {
		return nil, fmt.Errorf("%w: at most 64 nodes can be read", canvas.ErrInvalidNode)
	}
	var models []canvasNodeModel
	if err := r.database.WithContext(ctx).Where("work_id = ? AND id IN ?", workID, nodeIDs).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("get canvas nodes: %w", err)
	}
	byID := make(map[string]canvasNodeModel, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}
	nodes := make([]canvas.Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		model, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: %s", canvas.ErrNodeNotFound, id)
		}
		nodes = append(nodes, canvasNodeFromModel(model))
	}
	return nodes, nil
}

func (r *CanvasRepository) UpdateNode(ctx context.Context, input canvas.UpdateNodeInput) (canvas.Node, error) {
	var result canvasNodeModel
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, err := isNodeArchiveLocked(tx, input.WorkID, input.NodeID)
		if err != nil || locked {
			if locked {
				return canvas.ErrArchivedNodeLocked
			}
			return err
		}
		before, err := getCanvasNode(tx, input.WorkID, input.NodeID)
		if err != nil {
			return err
		}
		if before.Revision != input.ExpectedRevision {
			return canvas.ErrRevisionConflict
		}
		now := time.Now().UTC()
		update := tx.Model(&canvasNodeModel{}).Where("work_id = ? AND id = ? AND revision = ?", input.WorkID, input.NodeID, input.ExpectedRevision).Updates(map[string]any{"title": input.Title, "content": input.Content, "revision": gorm.Expr("revision + 1"), "updated_at": now})
		if update.Error != nil {
			return fmt.Errorf("update canvas node: %w", update.Error)
		}
		if update.RowsAffected == 0 {
			return canvas.ErrRevisionConflict
		}
		result = before
		result.Title, result.Content, result.Revision, result.UpdatedAt = input.Title, input.Content, before.Revision+1, now
		if before.Title != result.Title || before.Content != result.Content {
			return appendCanvasAction(tx, input.WorkID, actionUpdateNode, "编辑节点", updateNodeActionPayload{Before: nodeContentState{NodeID: before.ID, Title: before.Title, Content: before.Content}, After: nodeContentState{NodeID: result.ID, Title: result.Title, Content: result.Content}})
		}
		return nil
	})
	if err != nil {
		return canvas.Node{}, err
	}
	return canvasNodeFromModel(result), nil
}

func (r *CanvasRepository) UpdateNodePosition(ctx context.Context, workID, nodeID string, x, y float64) error {
	return r.UpdateNodePositions(ctx, workID, []canvas.NodePosition{{NodeID: nodeID, X: x, Y: y}})
}

func (r *CanvasRepository) ListEdges(ctx context.Context, workID string) ([]canvas.Edge, error) {
	var models []canvasEdgeModel
	if err := r.database.WithContext(ctx).Where("work_id = ?", workID).Order("created_at").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list canvas edges: %w", err)
	}
	return canvasEdgesFromModels(models), nil
}

func (r *CanvasRepository) CreateEdge(ctx context.Context, input canvas.CreateEdgeInput) (canvas.Edge, error) {
	var edge canvasEdgeModel
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&canvasNodeModel{}).Where("work_id = ? AND id IN ?", input.WorkID, []string{input.SourceNodeID, input.TargetNodeID}).Count(&count).Error; err != nil {
			return fmt.Errorf("read canvas edge nodes: %w", err)
		}
		if count != 2 {
			return canvas.ErrNodeNotFound
		}
		locked, err := isNodeArchiveLocked(tx, input.WorkID, input.TargetNodeID)
		if err != nil || locked {
			if locked {
				return canvas.ErrArchivedNodeLocked
			}
			return err
		}
		lookup := canvasEdgeModel{WorkID: input.WorkID, SourceNodeID: input.SourceNodeID, TargetNodeID: input.TargetNodeID, Kind: "generated_from"}
		if err := tx.Where(&lookup).First(&edge).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("read existing canvas edge: %w", err)
		}
		edge = canvasEdgeModel{ID: uuid.NewString(), WorkID: input.WorkID, SourceNodeID: input.SourceNodeID, TargetNodeID: input.TargetNodeID, Kind: "generated_from", CreatedAt: time.Now().UTC()}
		if err := tx.Create(&edge).Error; err != nil {
			return fmt.Errorf("create canvas edge: %w", err)
		}
		return appendCanvasAction(tx, input.WorkID, actionCreateEdge, "创建连接", createEdgeActionPayload{Edge: canvasEdgeFromModel(edge)})
	})
	return canvasEdgeFromModel(edge), err
}

func (r *CanvasRepository) CreateCandidate(ctx context.Context, candidate agent.Candidate) (agent.Candidate, error) {
	if existing, err := r.candidateByRun(ctx, candidate.RunID); err == nil {
		return existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return agent.Candidate{}, err
	}
	if candidate.ID == "" {
		candidate.ID = uuid.NewString()
	}
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = time.Now().UTC()
	}
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run agentRunModel
		if err := tx.Where("id = ? AND work_id = ?", candidate.RunID, candidate.WorkID).First(&run).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("create canvas candidate: %w", agent.ErrRunNotFound)
		} else if err != nil {
			return fmt.Errorf("read candidate run: %w", err)
		}
		candidate.Kind = run.Target
		candidate.ContextNodeIDs = append([]string{}, run.ContextNodeIDs...)
		candidate.Status = agent.CandidateStatusPending
		candidate.Title = candidateTitle(candidate.Content, candidate.Kind)
		var err error
		candidate.X, candidate.Y, err = r.initialCandidatePosition(tx, candidate.WorkID, candidate.ContextNodeIDs)
		if err != nil {
			return err
		}
		model := candidateModelFromDomain(candidate)
		return tx.Create(&model).Error
	})
	if err != nil {
		return agent.Candidate{}, fmt.Errorf("create canvas candidate: %w", err)
	}
	return candidate, nil
}

func (r *CanvasRepository) candidateByRun(ctx context.Context, runID string) (agent.Candidate, error) {
	var model agentCandidateModel
	if err := r.database.WithContext(ctx).Where("run_id = ?", runID).First(&model).Error; err != nil {
		return agent.Candidate{}, err
	}
	return candidateFromModel(r.database.WithContext(ctx), model)
}

func (r *CanvasRepository) ListCandidates(ctx context.Context, workID string) ([]agent.Candidate, error) {
	var models []agentCandidateModel
	db := r.database.WithContext(ctx)
	if err := db.Where("work_id = ? AND status = ?", workID, agent.CandidateStatusPending).Order("created_at DESC").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list canvas candidates: %w", err)
	}
	var runs []agentRunModel
	runIDs := make([]string, len(models))
	for i, model := range models {
		runIDs[i] = model.RunID
	}
	if len(runIDs) > 0 {
		if err := db.Select("id", "context_node_ids_json").Where("id IN ?", runIDs).Find(&runs).Error; err != nil {
			return nil, fmt.Errorf("read candidate runs: %w", err)
		}
	}
	contexts := make(map[string][]string, len(runs))
	for _, run := range runs {
		contexts[run.ID] = run.ContextNodeIDs
	}
	result := make([]agent.Candidate, 0, len(models))
	for _, model := range models {
		candidate := candidateFromStoredModel(model, contexts[model.RunID])
		result = append(result, candidate)
	}
	return result, nil
}

func (r *CanvasRepository) UpdateCandidatePosition(ctx context.Context, workID, candidateID string, x, y float64) error {
	result := r.database.WithContext(ctx).Model(&agentCandidateModel{}).Where("work_id = ? AND id = ? AND status = ?", workID, candidateID, agent.CandidateStatusPending).Updates(map[string]any{"x": x, "y": y})
	if result.Error != nil {
		return fmt.Errorf("update canvas candidate position: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return r.candidateMutationError(ctx, workID, candidateID)
	}
	return nil
}

func (r *CanvasRepository) AcceptCandidate(ctx context.Context, input canvas.AcceptCandidateInput) (canvas.Node, error) {
	var accepted canvasNodeModel
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		candidate, err := candidateByID(tx, input.WorkID, input.CandidateID)
		if err != nil {
			return err
		}
		if candidate.Status == agent.CandidateStatusAccepted {
			accepted, err = getCanvasNode(tx, input.WorkID, candidate.AcceptedNodeID)
			if errors.Is(err, canvas.ErrNodeNotFound) {
				return canvas.ErrCandidateResolved
			}
			return err
		}
		if candidate.Status == agent.CandidateStatusRejected {
			return canvas.ErrCandidateResolved
		}
		if candidate.CandidateType == "version" {
			accepted, err = acceptVersionCandidate(tx, input, candidate)
			return err
		}
		title, now := strings.TrimSpace(input.Title), time.Now().UTC()
		if title == "" {
			title = candidate.Title
		}
		accepted = canvasNodeModel{ID: uuid.NewString(), WorkID: candidate.WorkID, Revision: 1, Kind: candidate.Kind, Title: title, Content: candidate.Content, X: candidate.X, Y: candidate.Y, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&accepted).Error; err != nil {
			return fmt.Errorf("create accepted canvas node: %w", err)
		}
		if _, err := createInitialNodeVersionModel(tx, &accepted); err != nil {
			return err
		}
		edges := make([]canvasEdgeModel, 0, len(candidate.ContextNodeIDs))
		for _, sourceID := range uniqueStrings(candidate.ContextNodeIDs) {
			if sourceID != accepted.ID {
				edges = append(edges, canvasEdgeModel{ID: uuid.NewString(), WorkID: candidate.WorkID, SourceNodeID: sourceID, TargetNodeID: accepted.ID, Kind: "generated_from", CreatedAt: now})
			}
		}
		if len(edges) > 0 {
			if err := tx.Create(&edges).Error; err != nil {
				return fmt.Errorf("create accepted canvas edges: %w", err)
			}
		}
		update := tx.Model(&agentCandidateModel{}).Where("work_id = ? AND id = ? AND status = ?", input.WorkID, input.CandidateID, agent.CandidateStatusPending).Updates(map[string]any{"status": agent.CandidateStatusAccepted, "title": title, "accepted_node_id": accepted.ID, "decided_at": now})
		if update.Error != nil {
			return fmt.Errorf("accept canvas candidate: %w", update.Error)
		}
		if update.RowsAffected == 0 {
			return canvas.ErrCandidateResolved
		}
		return nil
	})
	return canvasNodeFromModel(accepted), err
}

func acceptVersionCandidate(tx *gorm.DB, input canvas.AcceptCandidateInput, candidate agent.Candidate) (canvasNodeModel, error) {
	locked, err := isNodeArchiveLocked(tx, candidate.WorkID, candidate.NodeID)
	if err != nil || locked {
		if locked {
			return canvasNodeModel{}, canvas.ErrArchivedNodeLocked
		}
		return canvasNodeModel{}, err
	}
	node, err := getCanvasNode(tx, candidate.WorkID, candidate.NodeID)
	if err != nil {
		return canvasNodeModel{}, err
	}
	now, title := time.Now().UTC(), strings.TrimSpace(input.Title)
	if title == "" {
		title = candidate.Title
	}
	node.Title, node.Content, node.Revision, node.UpdatedAt = title, candidate.Content, node.Revision+1, now
	if _, err := createNodeVersionModel(tx, &node, node.CurrentVersionID, candidate.RunID); err != nil {
		return canvasNodeModel{}, err
	}
	if err := tx.Model(&canvasNodeModel{}).Where("work_id = ? AND id = ?", node.WorkID, node.ID).Updates(map[string]any{"title": title, "content": candidate.Content, "revision": node.Revision, "current_version_id": node.CurrentVersionID, "updated_at": now}).Error; err != nil {
		return canvasNodeModel{}, err
	}
	update := tx.Model(&agentCandidateModel{}).Where("work_id = ? AND id = ? AND status = ?", input.WorkID, input.CandidateID, agent.CandidateStatusPending).Updates(map[string]any{"status": agent.CandidateStatusAccepted, "title": title, "accepted_node_id": candidate.NodeID, "decided_at": now})
	if update.Error != nil {
		return canvasNodeModel{}, update.Error
	}
	if update.RowsAffected == 0 {
		return canvasNodeModel{}, canvas.ErrCandidateResolved
	}
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
	result := r.database.WithContext(ctx).Model(&agentCandidateModel{}).Where("work_id = ? AND id = ? AND status = ?", workID, candidateID, agent.CandidateStatusPending).Updates(map[string]any{"status": agent.CandidateStatusRejected, "decided_at": now})
	if result.Error != nil {
		return fmt.Errorf("reject canvas candidate: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var model agentCandidateModel
	if err := r.database.WithContext(ctx).Select("status").Where("work_id = ? AND id = ?", workID, candidateID).First(&model).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return canvas.ErrCandidateNotFound
	} else if err != nil {
		return fmt.Errorf("read canvas candidate status: %w", err)
	}
	if model.Status == string(agent.CandidateStatusRejected) {
		return nil
	}
	return canvas.ErrCandidateResolved
}

func (r *CanvasRepository) ListNodeVersions(ctx context.Context, workID, nodeID string) ([]canvas.NodeVersion, error) {
	var models []canvasNodeVersionModel
	if err := r.database.WithContext(ctx).Where("work_id = ? AND node_id = ?", workID, nodeID).Order("version_number DESC").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list node versions: %w", err)
	}
	versions := make([]canvas.NodeVersion, len(models))
	for i, model := range models {
		versions[i] = canvasNodeVersionFromModel(model)
	}
	return versions, nil
}

func (r *CanvasRepository) SwitchNodeVersion(ctx context.Context, workID, nodeID, versionID string) (canvas.Node, error) {
	var node canvasNodeModel
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, err := isNodeArchiveLocked(tx, workID, nodeID)
		if err != nil || locked {
			if locked {
				return canvas.ErrArchivedNodeLocked
			}
			return err
		}
		var version canvasNodeVersionModel
		if err := tx.Where("id = ? AND work_id = ? AND node_id = ?", versionID, workID, nodeID).First(&version).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return canvas.ErrNodeNotFound
		} else if err != nil {
			return err
		}
		node, err = getCanvasNode(tx, workID, nodeID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := tx.Model(&canvasNodeModel{}).Where("work_id = ? AND id = ?", workID, nodeID).Updates(map[string]any{"title": version.Title, "content": version.Content, "current_version_id": versionID, "revision": gorm.Expr("revision + 1"), "updated_at": now}).Error; err != nil {
			return err
		}
		node.Title, node.Content, node.CurrentVersionID, node.Revision, node.UpdatedAt = version.Title, version.Content, versionID, node.Revision+1, now
		return nil
	})
	return canvasNodeFromModel(node), err
}

func canvasNodeFromModel(model canvasNodeModel) canvas.Node {
	return canvas.Node{ID: model.ID, WorkID: model.WorkID, Revision: model.Revision, Kind: canvas.NodeKind(model.Kind), Title: model.Title, Content: model.Content, X: model.X, Y: model.Y, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt, CurrentVersionID: model.CurrentVersionID}
}

func canvasNodesFromModels(models []canvasNodeModel) []canvas.Node {
	result := make([]canvas.Node, len(models))
	for i, model := range models {
		result[i] = canvasNodeFromModel(model)
	}
	return result
}

func canvasEdgeFromModel(model canvasEdgeModel) canvas.Edge {
	return canvas.Edge{ID: model.ID, WorkID: model.WorkID, SourceNodeID: model.SourceNodeID, TargetNodeID: model.TargetNodeID, Kind: model.Kind, CreatedAt: model.CreatedAt}
}

func canvasEdgesFromModels(models []canvasEdgeModel) []canvas.Edge {
	result := make([]canvas.Edge, len(models))
	for i, model := range models {
		result[i] = canvasEdgeFromModel(model)
	}
	return result
}

func canvasNodeVersionFromModel(model canvasNodeVersionModel) canvas.NodeVersion {
	return canvas.NodeVersion{ID: model.ID, NodeID: model.NodeID, WorkID: model.WorkID, VersionNumber: model.VersionNumber, ParentVersionID: model.ParentVersionID, Title: model.Title, Content: model.Content, SourceRunID: model.SourceRunID, CreatedAt: model.CreatedAt}
}

func candidateModelFromDomain(candidate agent.Candidate) agentCandidateModel {
	return agentCandidateModel{ID: candidate.ID, RunID: candidate.RunID, WorkID: candidate.WorkID, SkillID: candidate.SkillID, SkillVersion: candidate.SkillVersion, Status: string(candidate.Status), Kind: candidate.Kind, Title: candidate.Title, Content: candidate.Content, X: candidate.X, Y: candidate.Y, AcceptedNodeID: candidate.AcceptedNodeID, CreatedAt: candidate.CreatedAt, DecidedAt: candidate.DecidedAt, CandidateType: valueOrDefault(candidate.CandidateType, "node"), NodeID: candidate.NodeID, BaseVersionID: candidate.BaseVersionID, Reason: candidate.Reason, ChangeScore: candidate.ChangeScore}
}

func candidateFromStoredModel(model agentCandidateModel, contextIDs []string) agent.Candidate {
	if model.CandidateType == "version" && model.NodeID != "" {
		contextIDs = []string{model.NodeID}
	}
	return agent.Candidate{ID: model.ID, RunID: model.RunID, WorkID: model.WorkID, SkillID: model.SkillID, SkillVersion: model.SkillVersion, Status: agent.CandidateStatus(model.Status), Kind: model.Kind, CandidateType: model.CandidateType, NodeID: model.NodeID, BaseVersionID: model.BaseVersionID, Reason: model.Reason, ChangeScore: model.ChangeScore, Title: model.Title, Content: model.Content, X: model.X, Y: model.Y, ContextNodeIDs: append([]string{}, contextIDs...), AcceptedNodeID: model.AcceptedNodeID, CreatedAt: model.CreatedAt, DecidedAt: model.DecidedAt}
}

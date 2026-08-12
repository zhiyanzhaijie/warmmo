package persistence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"warmmo/core/internal/domain/work"
)

const workPreviewLimit = 6

type WorkRepository struct{ database *gorm.DB }

func NewWorkRepository(provider *ProviderRepository) *WorkRepository {
	return &WorkRepository{database: provider.database}
}

func NewWorkRepositoryWithDatabase(database *Database) *WorkRepository {
	return &WorkRepository{database: database.DB}
}

func (r *WorkRepository) Create(ctx context.Context, input work.CreateInput) (work.Summary, error) {
	folder, err := r.findFolder(ctx, input.FolderID)
	if err != nil {
		return work.Summary{}, err
	}
	model := workModel{
		ID: uuid.NewString(), Title: input.Title, Description: input.Description,
		FolderID: input.FolderID, Status: "active", Revision: 1,
	}
	if err := r.database.WithContext(ctx).Create(&model).Error; err != nil {
		return work.Summary{}, databaseError("create work", err)
	}
	return workSummaryFromModel(model, folder, 0), nil
}

func (r *WorkRepository) Get(ctx context.Context, workID string) (work.Detail, error) {
	var model workModel
	err := r.database.WithContext(ctx).Preload("Folder").First(&model, "id = ?", workID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return work.Detail{}, work.ErrNotFound
	}
	if err != nil {
		return work.Detail{}, databaseError("get work", err)
	}
	return workDetailFromModel(model), nil
}

func (r *WorkRepository) List(ctx context.Context) ([]work.Summary, error) {
	var models []workModel
	if err := r.database.WithContext(ctx).Preload("Folder").Order("updated_at DESC, id").Find(&models).Error; err != nil {
		return nil, databaseError("list works", err)
	}
	works := make([]work.Summary, len(models))
	workIndex := make(map[string]int, len(models))
	for index, model := range models {
		works[index] = workSummaryFromModel(model, model.Folder, 0)
		workIndex[model.ID] = index
	}
	if len(works) == 0 {
		return works, nil
	}
	if err := r.loadNodeCounts(ctx, works, workIndex); err != nil {
		return nil, err
	}
	if err := r.loadPreviewNodes(ctx, works, workIndex); err != nil {
		return nil, err
	}
	if err := r.loadPreviewEdges(ctx, works, workIndex); err != nil {
		return nil, err
	}
	return works, nil
}

func (r *WorkRepository) Update(ctx context.Context, input work.UpdateInput) (work.Detail, error) {
	if _, err := r.findFolder(ctx, input.FolderID); err != nil {
		return work.Detail{}, err
	}
	var detail work.Detail
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&workModel{}).
			Where("id = ? AND revision = ?", input.ID, input.ExpectedRevision).
			Updates(map[string]any{
				"title": input.Title, "description": input.Description,
				"folder_id": input.FolderID, "status": input.Status,
				"revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC(),
			})
		if result.Error != nil {
			return databaseError("update work", result.Error)
		}
		if result.RowsAffected == 0 {
			var count int64
			if err := tx.Model(&workModel{}).Where("id = ?", input.ID).Count(&count).Error; err != nil {
				return databaseError("find work after update conflict", err)
			}
			if count == 0 {
				return work.ErrNotFound
			}
			return work.ErrRevisionConflict
		}
		var model workModel
		if err := tx.Preload("Folder").First(&model, "id = ?", input.ID).Error; err != nil {
			return databaseError("reload updated work", err)
		}
		detail = workDetailFromModel(model)
		return nil
	})
	return detail, err
}

func (r *WorkRepository) Delete(ctx context.Context, workID string) error {
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&workModel{}).Where("id = ?", workID).Count(&count).Error; err != nil {
			return databaseError("find work before delete", err)
		}
		if count == 0 {
			return work.ErrNotFound
		}

		if err := tx.Model(&agentRunModel{}).
			Where("work_id = ? AND status IN ?", workID, []string{"queued", "running"}).
			Count(&count).Error; err != nil {
			return databaseError("check active agent runs", err)
		}
		if count > 0 {
			return work.ErrActiveRun
		}

		var documents []knowledgeVectorDocumentModel
		if err := tx.Select("vector_row_id").Where("work_id = ?", workID).Find(&documents).Error; err != nil {
			return databaseError("list work vectors", err)
		}
		for _, document := range documents {
			if err := tx.Exec(`DELETE FROM knowledge_vectors_partitioned WHERE rowid = ?`, document.VectorRowID).Error; err != nil {
				return databaseError("delete work vector", err)
			}
		}

		runIDs := tx.Model(&agentRunModel{}).Select("id").Where("work_id = ?", workID)
		if err := tx.Where("run_id IN (?)", runIDs).Delete(&agentResponseModel{}).Error; err != nil {
			return databaseError("delete work agent responses", err)
		}
		if err := tx.Where("run_id IN (?)", runIDs).Delete(&agentRunEventModel{}).Error; err != nil {
			return databaseError("delete work agent events", err)
		}

		scopedModels := []struct {
			label string
			model any
		}{
			{"agent proposal edges", &agentProposalEdgeModel{}},
			{"agent candidates", &agentCandidateModel{}},
			{"agent runs", &agentRunModel{}},
			{"chapter archive sections", &chapterArchiveSectionModel{}},
			{"chapter archives", &chapterArchiveModel{}},
			{"canvas edges", &canvasEdgeModel{}},
			{"canvas actions", &canvasActionModel{}},
			{"canvas history", &canvasHistoryStateModel{}},
			{"canvas node versions", &canvasNodeVersionModel{}},
			{"canvas nodes", &canvasNodeModel{}},
			{"knowledge documents", &knowledgeVectorDocumentModel{}},
			{"knowledge jobs", &knowledgeIndexJobModel{}},
		}
		for _, scoped := range scopedModels {
			if err := tx.Where("work_id = ?", workID).Delete(scoped.model).Error; err != nil {
				return databaseError(fmt.Sprintf("delete work %s", scoped.label), err)
			}
		}

		result := tx.Where("id = ?", workID).Delete(&workModel{})
		if result.Error != nil {
			return databaseError("delete work", result.Error)
		}
		if result.RowsAffected == 0 {
			return work.ErrNotFound
		}
		return nil
	})
	return err
}

func (r *WorkRepository) CreateFolder(ctx context.Context, name string) (work.Folder, error) {
	model := workFolderModel{ID: uuid.NewString(), Name: name}
	err := r.database.WithContext(ctx).Create(&model).Error
	if isUniqueConstraint(err) {
		return work.Folder{}, work.ErrFolderConflict
	}
	if err != nil {
		return work.Folder{}, databaseError("create work folder", err)
	}
	return workFolderFromModel(model), nil
}

func (r *WorkRepository) ListFolders(ctx context.Context) ([]work.Folder, error) {
	var models []workFolderModel
	if err := r.database.WithContext(ctx).Order("sort_order, name").Find(&models).Error; err != nil {
		return nil, databaseError("list work folders", err)
	}
	folders := make([]work.Folder, len(models))
	for index, model := range models {
		folders[index] = workFolderFromModel(model)
	}
	return folders, nil
}

func (r *WorkRepository) findFolder(ctx context.Context, folderID string) (*workFolderModel, error) {
	if folderID == "" {
		return nil, nil
	}
	var model workFolderModel
	err := r.database.WithContext(ctx).First(&model, "id = ?", folderID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, work.ErrFolderNotFound
	}
	if err != nil {
		return nil, databaseError("get work folder", err)
	}
	return &model, nil
}

func workDetailFromModel(model workModel) work.Detail {
	folderName := ""
	if model.Folder != nil {
		folderName = model.Folder.Name
	}
	return work.Detail{
		ID: model.ID, Title: model.Title, Description: model.Description,
		FolderID: model.FolderID, FolderName: folderName, Status: model.Status,
		Revision: model.Revision, UpdatedAt: model.UpdatedAt,
	}
}

func workSummaryFromModel(model workModel, folder *workFolderModel, nodeCount int) work.Summary {
	model.Folder = folder
	detail := workDetailFromModel(model)
	return work.Summary{
		ID: detail.ID, Title: detail.Title, Description: detail.Description,
		FolderID: detail.FolderID, FolderName: detail.FolderName, Status: detail.Status,
		Revision: detail.Revision, UpdatedAt: detail.UpdatedAt, NodeCount: nodeCount,
		PreviewNodes: []work.PreviewNode{}, PreviewEdges: []work.PreviewEdge{},
	}
}

func workFolderFromModel(model workFolderModel) work.Folder {
	return work.Folder{
		ID: model.ID, Name: model.Name, SortOrder: model.SortOrder,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

func (r *WorkRepository) loadNodeCounts(ctx context.Context, works []work.Summary, index map[string]int) error {
	var counts []struct {
		WorkID string
		Count  int
	}
	if err := r.database.WithContext(ctx).Model(&canvasNodeModel{}).
		Select("work_id, count(*) AS count").Group("work_id").Scan(&counts).Error; err != nil {
		return databaseError("count work nodes", err)
	}
	for _, count := range counts {
		if workIndex, exists := index[count.WorkID]; exists {
			works[workIndex].NodeCount = count.Count
		}
	}
	return nil
}

func (r *WorkRepository) loadPreviewNodes(ctx context.Context, works []work.Summary, index map[string]int) error {
	var rows []struct {
		WorkID, ID, Title, Kind string
		X, Y                    float64
	}
	err := r.database.WithContext(ctx).Raw(`
SELECT work_id,id,title,kind,x,y FROM (
    SELECT work_id,id,title,kind,x,y,
           ROW_NUMBER() OVER (PARTITION BY work_id ORDER BY updated_at DESC,id) row_number
    FROM canvas_nodes
) WHERE row_number <= ? ORDER BY work_id,row_number`, workPreviewLimit).Scan(&rows).Error
	if err != nil {
		return databaseError("list work preview nodes", err)
	}
	for _, row := range rows {
		if workIndex, exists := index[row.WorkID]; exists {
			works[workIndex].PreviewNodes = append(works[workIndex].PreviewNodes, work.PreviewNode{
				ID: row.ID, Label: row.Title, Kind: row.Kind, X: row.X, Y: row.Y,
			})
		}
	}
	return nil
}

func (r *WorkRepository) loadPreviewEdges(ctx context.Context, works []work.Summary, index map[string]int) error {
	var rows []struct{ WorkID, Source, Target string }
	err := r.database.WithContext(ctx).Raw(`
WITH preview_nodes AS (
    SELECT work_id,id,ROW_NUMBER() OVER (PARTITION BY work_id ORDER BY updated_at DESC,id) row_number
    FROM canvas_nodes
)
SELECT e.work_id,e.source_node_id AS source,e.target_node_id AS target
FROM canvas_edges e
JOIN preview_nodes source ON source.work_id=e.work_id AND source.id=e.source_node_id AND source.row_number<=?
JOIN preview_nodes target ON target.work_id=e.work_id AND target.id=e.target_node_id AND target.row_number<=?
ORDER BY e.work_id,e.created_at`, workPreviewLimit, workPreviewLimit).Scan(&rows).Error
	if err != nil {
		return databaseError("list work preview edges", err)
	}
	for _, row := range rows {
		if workIndex, exists := index[row.WorkID]; exists {
			works[workIndex].PreviewEdges = append(works[workIndex].PreviewEdges, work.PreviewEdge{Source: row.Source, Target: row.Target})
		}
	}
	return nil
}

func isUniqueConstraint(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "UNIQUE constraint failed") || strings.Contains(err.Error(), "constraint failed"))
}

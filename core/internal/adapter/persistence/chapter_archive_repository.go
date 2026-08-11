package persistence

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"gorm.io/gorm"

	"warmmo/core/internal/application/pagination"
	"warmmo/core/internal/domain/canvas"
)

func isNodeArchiveLocked(db *gorm.DB, workID, nodeID string) (bool, error) {
	var direct, section int64
	base := db.Model(&chapterArchiveModel{}).Where("work_id = ? AND is_current = ? AND retracted_at IS NULL", workID, true)
	if err := base.Where("chapter_outline_node_id = ?", nodeID).Count(&direct).Error; err != nil {
		return false, fmt.Errorf("read chapter archive lock: %w", err)
	}
	if direct > 0 {
		return true, nil
	}
	err := db.Model(&chapterArchiveSectionModel{}).
		Joins("JOIN chapter_archives ON chapter_archives.id = chapter_archive_sections.archive_id").
		Where("chapter_archives.work_id = ? AND chapter_archives.is_current = ? AND chapter_archives.retracted_at IS NULL", workID, true).
		Where("chapter_archive_sections.section_outline_node_id = ? OR chapter_archive_sections.chapter_section_node_id = ?", nodeID, nodeID).
		Count(&section).Error
	if err != nil {
		return false, fmt.Errorf("read chapter archive section lock: %w", err)
	}
	return section > 0, nil
}

func (r *CanvasRepository) ListCurrentChapterArchives(ctx context.Context, workID string) ([]canvas.ChapterArchive, error) {
	return r.listChapterArchives(ctx, workID, "", true, nil)
}

func (r *CanvasRepository) ListChapterArchiveVisibility(ctx context.Context, workID string) ([]canvas.ChapterArchiveVisibility, error) {
	var models []chapterArchiveModel
	if err := r.database.WithContext(ctx).Select("id", "chapter_outline_node_id").Preload("Sections", func(db *gorm.DB) *gorm.DB {
		return db.Select("archive_id", "chapter_section_node_id", "section_outline_node_id").Order("ordinal")
	}).Where("work_id = ? AND is_current = ? AND retracted_at IS NULL", workID, true).Order("created_at, revision, id").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list chapter archive visibility: %w", err)
	}
	result := make([]canvas.ChapterArchiveVisibility, len(models))
	for i, model := range models {
		sections := make([]canvas.ChapterArchiveVisibilitySection, len(model.Sections))
		for j, section := range model.Sections {
			sections[j] = canvas.ChapterArchiveVisibilitySection{SectionOutlineNodeID: section.SectionOutlineNodeID, ChapterSectionNodeID: section.ChapterSectionNodeID}
		}
		result[i] = canvas.ChapterArchiveVisibility{ChapterOutlineNodeID: model.ChapterOutlineNodeID, Sections: sections}
	}
	return result, nil
}

func (r *CanvasRepository) ListCurrentChapterArchivesPage(ctx context.Context, workID string, pageable pagination.Pageable) (pagination.Page[canvas.ChapterArchive], error) {
	window, err := pagination.WindowFor(pageable)
	if err != nil {
		return pagination.Page[canvas.ChapterArchive]{}, err
	}
	total, err := r.countChapterArchives(ctx, workID, "", false)
	if err != nil {
		return pagination.Page[canvas.ChapterArchive]{}, err
	}
	archives, err := r.listChapterArchives(ctx, workID, "", false, &window)
	if err != nil {
		return pagination.Page[canvas.ChapterArchive]{}, err
	}
	return pagination.NewPage(archives, total, pageable)
}

func (r *CanvasRepository) ListChapterArchiveTimelinePage(ctx context.Context, workID string, pageable pagination.Pageable) (pagination.Page[canvas.ChapterArchiveTimeline], error) {
	window, err := pagination.WindowFor(pageable)
	if err != nil {
		return pagination.Page[canvas.ChapterArchiveTimeline]{}, err
	}
	total, err := r.countChapterArchives(ctx, workID, "", false)
	if err != nil {
		return pagination.Page[canvas.ChapterArchiveTimeline]{}, err
	}
	var models []chapterArchiveModel
	err = r.database.WithContext(ctx).Select("id", "chapter_outline_node_id", "revision", "outline_title", "summary", "projection_status").Preload("Sections", func(db *gorm.DB) *gorm.DB {
		return db.Order("ordinal")
	}).Where("work_id = ? AND retracted_at IS NULL", workID).Order("created_at, revision, id").Limit(window.Limit).Offset(window.Offset).Find(&models).Error
	if err != nil {
		return pagination.Page[canvas.ChapterArchiveTimeline]{}, fmt.Errorf("list chapter archive timeline: %w", err)
	}
	items := make([]canvas.ChapterArchiveTimeline, len(models))
	for i, model := range models {
		sections := make([]canvas.ChapterArchiveTimelineSection, len(model.Sections))
		for j, section := range model.Sections {
			sections[j] = canvas.ChapterArchiveTimelineSection{ArchiveID: section.ArchiveID, Ordinal: section.Ordinal, SectionOutlineNodeID: section.SectionOutlineNodeID, ChapterSectionNodeID: section.ChapterSectionNodeID, NodeRevision: section.NodeRevision, Title: section.Title, Summary: section.Summary}
		}
		items[i] = canvas.ChapterArchiveTimeline{ID: model.ID, ChapterOutlineNodeID: model.ChapterOutlineNodeID, Revision: model.Revision, OutlineTitle: model.OutlineTitle, Summary: model.Summary, ProjectionStatus: model.ProjectionStatus, Sections: sections}
	}
	return pagination.NewPage(items, total, pageable)
}

func (r *CanvasRepository) ListChapterArchiveHistory(ctx context.Context, workID, chapterOutlineNodeID string) ([]canvas.ChapterArchive, error) {
	return r.listChapterArchives(ctx, workID, chapterOutlineNodeID, false, nil)
}

func (r *CanvasRepository) listChapterArchives(ctx context.Context, workID, chapterOutlineNodeID string, currentOnly bool, window *pagination.Window) ([]canvas.ChapterArchive, error) {
	db := r.database.WithContext(ctx).Preload("Sections", func(db *gorm.DB) *gorm.DB { return db.Order("ordinal") }).Where("work_id = ?", workID)
	if chapterOutlineNodeID != "" {
		db = db.Where("chapter_outline_node_id = ?", chapterOutlineNodeID)
	}
	if currentOnly {
		db = db.Where("is_current = ? AND retracted_at IS NULL", true)
	} else if chapterOutlineNodeID == "" {
		db = db.Where("retracted_at IS NULL")
	}
	if window != nil {
		db = db.Limit(window.Limit).Offset(window.Offset)
	}
	var models []chapterArchiveModel
	if err := db.Order("created_at, revision, id").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list chapter archives: %w", err)
	}
	archives := make([]canvas.ChapterArchive, len(models))
	for i, model := range models {
		archives[i] = chapterArchiveFromModel(model)
	}
	if currentOnly {
		r.ensureCurrentChapterArchiveProjections(ctx, archives)
	}
	return archives, nil
}

func (r *CanvasRepository) countChapterArchives(ctx context.Context, workID, chapterOutlineNodeID string, currentOnly bool) (int64, error) {
	db := r.database.WithContext(ctx).Model(&chapterArchiveModel{}).Where("work_id = ?", workID)
	if chapterOutlineNodeID != "" {
		db = db.Where("chapter_outline_node_id = ?", chapterOutlineNodeID)
	}
	if currentOnly {
		db = db.Where("is_current = ? AND retracted_at IS NULL", true)
	} else if chapterOutlineNodeID == "" {
		db = db.Where("retracted_at IS NULL")
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return 0, fmt.Errorf("count chapter archives: %w", err)
	}
	return total, nil
}

func (r *CanvasRepository) ensureCurrentChapterArchiveProjections(ctx context.Context, archives []canvas.ChapterArchive) {
	for i := range archives {
		archive := &archives[i]
		projection, err := os.ReadFile(chapterArchiveProjectionPath(r.dataDirectory, *archive))
		status := "pending"
		if err == nil && bytes.Equal(projection, renderChapterArchiveProjection(*archive)) {
			status = "ready"
		} else if writeChapterArchiveProjection(r.dataDirectory, *archive) == nil {
			status = "ready"
		}
		if archive.ProjectionStatus != status {
			if updateErr := r.database.WithContext(ctx).Model(&chapterArchiveModel{}).Where("id = ?", archive.ID).Update("projection_status", status).Error; updateErr == nil {
				archive.ProjectionStatus = status
			}
		}
	}
}

func (r *CanvasRepository) RetractChapterArchive(ctx context.Context, workID, archiveID string) error {
	var archive chapterArchiveModel
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("work_id = ? AND id = ?", workID, archiveID).First(&archive).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return canvas.ErrChapterArchiveNotFound
		} else if err != nil {
			return fmt.Errorf("read chapter archive for retraction: %w", err)
		}
		if !archive.IsCurrent || archive.RetractedAt != nil {
			return canvas.ErrChapterArchiveNotCurrent
		}
		now := time.Now().UTC()
		if err := tx.Model(&agentCandidateModel{}).Where("work_id = ? AND run_id = ? AND status = ?", workID, archive.RunID, "pending").Updates(map[string]any{"status": "rejected", "decided_at": now}).Error; err != nil {
			return fmt.Errorf("reject pending archive candidates: %w", err)
		}
		result := tx.Model(&chapterArchiveModel{}).Where("work_id = ? AND id = ? AND is_current = ? AND retracted_at IS NULL", workID, archiveID, true).Updates(map[string]any{"is_current": false, "retracted_at": now})
		if result.Error != nil {
			return fmt.Errorf("retract chapter archive: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return canvas.ErrChapterArchiveNotCurrent
		}
		return nil
	})
	if err != nil {
		return err
	}
	path := chapterArchiveProjectionPath(r.dataDirectory, canvas.ChapterArchive{WorkID: workID, ChapterOutlineNodeID: archive.ChapterOutlineNodeID})
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove chapter archive projection: %w", err)
	}
	return nil
}

func chapterArchiveFromModel(model chapterArchiveModel) canvas.ChapterArchive {
	sections := make([]canvas.ChapterArchiveSection, len(model.Sections))
	for i, section := range model.Sections {
		sections[i] = canvas.ChapterArchiveSection{ArchiveID: section.ArchiveID, Ordinal: section.Ordinal, SectionOutlineNodeID: section.SectionOutlineNodeID, ChapterSectionNodeID: section.ChapterSectionNodeID, ChapterSectionVersionID: section.ChapterSectionVersionID, NodeRevision: section.NodeRevision, Title: section.Title, Summary: section.Summary, Content: section.Content, ContentHash: section.ContentHash}
	}
	return canvas.ChapterArchive{ID: model.ID, WorkID: model.WorkID, ChapterOutlineNodeID: model.ChapterOutlineNodeID, Revision: model.Revision, RunID: model.RunID, OutlineVersionID: model.OutlineVersionID, OutlineRevision: model.OutlineRevision, OutlineTitle: model.OutlineTitle, OutlineContent: model.OutlineContent, Summary: model.Summary, SourceDigest: model.SourceDigest, IsCurrent: model.IsCurrent, ProjectionStatus: model.ProjectionStatus, Sections: sections, CreatedAt: model.CreatedAt, SupersededAt: model.SupersededAt, RetractedAt: model.RetractedAt}
}

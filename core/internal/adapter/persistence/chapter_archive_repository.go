package persistence

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"warmmo/core/internal/application/pagination"
	"warmmo/core/internal/domain/canvas"
)

type archiveLockQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func isNodeArchiveLocked(ctx context.Context, querier archiveLockQuerier, workID, nodeID string) (bool, error) {
	var locked int
	err := querier.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM chapter_archives archive
    WHERE archive.work_id=? AND archive.is_current=1 AND archive.retracted_at=''
      AND (
          archive.chapter_outline_node_id=?
          OR EXISTS (
              SELECT 1
              FROM chapter_archive_sections section
              WHERE section.archive_id=archive.id
                AND (section.section_outline_node_id=? OR section.chapter_section_node_id=?)
          )
      )
)`, workID, nodeID, nodeID, nodeID).Scan(&locked)
	if err != nil {
		return false, fmt.Errorf("read chapter archive lock: %w", err)
	}
	return locked != 0, nil
}

func (r *CanvasRepository) ListCurrentChapterArchives(ctx context.Context, workID string) ([]canvas.ChapterArchive, error) {
	return r.listChapterArchives(ctx, workID, "", true, nil)
}

func (r *CanvasRepository) ListChapterArchiveVisibility(ctx context.Context, workID string) ([]canvas.ChapterArchiveVisibility, error) {
	rows, err := r.database.QueryContext(ctx, `SELECT id,chapter_outline_node_id FROM chapter_archives WHERE work_id=? AND is_current=1 AND retracted_at='' ORDER BY created_at, revision, id`, workID)
	if err != nil {
		return nil, fmt.Errorf("list chapter archive visibility: %w", err)
	}
	archives := make([]canvas.ChapterArchiveVisibility, 0)
	archiveIDs := make([]string, 0)
	for rows.Next() {
		var archiveID string
		var archive canvas.ChapterArchiveVisibility
		if err := rows.Scan(&archiveID, &archive.ChapterOutlineNodeID); err != nil {
			rows.Close()
			return nil, err
		}
		archives = append(archives, archive)
		archiveIDs = append(archiveIDs, archiveID)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, archiveID := range archiveIDs {
		sections, err := r.listChapterArchiveVisibilitySections(ctx, archiveID)
		if err != nil {
			return nil, err
		}
		archives[i].Sections = sections
	}
	return archives, nil
}

func (r *CanvasRepository) listChapterArchiveVisibilitySections(ctx context.Context, archiveID string) ([]canvas.ChapterArchiveVisibilitySection, error) {
	rows, err := r.database.QueryContext(ctx, `SELECT section_outline_node_id,chapter_section_node_id FROM chapter_archive_sections WHERE archive_id=? ORDER BY ordinal`, archiveID)
	if err != nil {
		return nil, fmt.Errorf("list chapter archive visibility sections: %w", err)
	}
	defer rows.Close()
	sections := make([]canvas.ChapterArchiveVisibilitySection, 0)
	for rows.Next() {
		var section canvas.ChapterArchiveVisibilitySection
		if err := rows.Scan(&section.SectionOutlineNodeID, &section.ChapterSectionNodeID); err != nil {
			return nil, err
		}
		sections = append(sections, section)
	}
	return sections, rows.Err()
}

func (r *CanvasRepository) ListCurrentChapterArchivesPage(
	ctx context.Context,
	workID string,
	pageable pagination.Pageable,
) (pagination.Page[canvas.ChapterArchive], error) {
	window, err := pagination.WindowFor(pageable)
	if err != nil {
		return pagination.Page[canvas.ChapterArchive]{}, err
	}
	// Story spine is an archive timeline, so superseded revisions remain
	// visible even though only current revisions participate in canvas locks.
	total, err := r.countChapterArchives(ctx, workID, "", false)
	if err != nil {
		return pagination.Page[canvas.ChapterArchive]{}, err
	}
	archives, err := r.listChapterArchives(ctx, workID, "", false, &window)
	if err != nil {
		return pagination.Page[canvas.ChapterArchive]{}, err
	}
	page, err := pagination.NewPage(archives, total, pageable)
	if err != nil {
		return pagination.Page[canvas.ChapterArchive]{}, err
	}
	return page, nil
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
	rows, err := r.database.QueryContext(ctx, `SELECT id,chapter_outline_node_id,revision,outline_title,summary,projection_status FROM chapter_archives WHERE work_id=? AND retracted_at='' ORDER BY created_at, revision, id LIMIT ? OFFSET ?`, workID, window.Limit, window.Offset)
	if err != nil {
		return pagination.Page[canvas.ChapterArchiveTimeline]{}, fmt.Errorf("list chapter archive timeline: %w", err)
	}
	items := make([]canvas.ChapterArchiveTimeline, 0)
	for rows.Next() {
		var item canvas.ChapterArchiveTimeline
		if err := rows.Scan(&item.ID, &item.ChapterOutlineNodeID, &item.Revision, &item.OutlineTitle, &item.Summary, &item.ProjectionStatus); err != nil {
			rows.Close()
			return pagination.Page[canvas.ChapterArchiveTimeline]{}, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return pagination.Page[canvas.ChapterArchiveTimeline]{}, err
	}
	if err := rows.Err(); err != nil {
		return pagination.Page[canvas.ChapterArchiveTimeline]{}, err
	}
	for index := range items {
		items[index].Sections, err = r.listChapterArchiveTimelineSections(ctx, items[index].ID)
		if err != nil {
			return pagination.Page[canvas.ChapterArchiveTimeline]{}, err
		}
	}
	return pagination.NewPage(items, total, pageable)
}

func (r *CanvasRepository) listChapterArchiveTimelineSections(ctx context.Context, archiveID string) ([]canvas.ChapterArchiveTimelineSection, error) {
	rows, err := r.database.QueryContext(ctx, `SELECT archive_id,ordinal,section_outline_node_id,chapter_section_node_id,node_revision,title,summary FROM chapter_archive_sections WHERE archive_id=? ORDER BY ordinal`, archiveID)
	if err != nil {
		return nil, fmt.Errorf("list chapter archive timeline sections: %w", err)
	}
	defer rows.Close()
	sections := make([]canvas.ChapterArchiveTimelineSection, 0)
	for rows.Next() {
		var section canvas.ChapterArchiveTimelineSection
		if err := rows.Scan(&section.ArchiveID, &section.Ordinal, &section.SectionOutlineNodeID, &section.ChapterSectionNodeID, &section.NodeRevision, &section.Title, &section.Summary); err != nil {
			return nil, err
		}
		sections = append(sections, section)
	}
	return sections, rows.Err()
}

func (r *CanvasRepository) ListChapterArchiveHistory(ctx context.Context, workID, chapterOutlineNodeID string) ([]canvas.ChapterArchive, error) {
	return r.listChapterArchives(ctx, workID, chapterOutlineNodeID, false, nil)
}

func (r *CanvasRepository) listChapterArchives(
	ctx context.Context,
	workID string,
	chapterOutlineNodeID string,
	currentOnly bool,
	window *pagination.Window,
) ([]canvas.ChapterArchive, error) {
	query := `SELECT id,work_id,chapter_outline_node_id,revision,run_id,outline_version_id,outline_revision,outline_title,outline_content,summary,source_digest,is_current,projection_status,created_at,superseded_at,retracted_at FROM chapter_archives WHERE work_id=?`
	arguments := []any{workID}
	if chapterOutlineNodeID != "" {
		query += " AND chapter_outline_node_id=?"
		arguments = append(arguments, chapterOutlineNodeID)
	}
	if currentOnly {
		query += " AND is_current=1 AND retracted_at=''"
	} else if chapterOutlineNodeID == "" {
		query += " AND retracted_at=''"
	}
	query += " ORDER BY created_at, revision, id"
	if window != nil {
		query += " LIMIT ? OFFSET ?"
		arguments = append(arguments, window.Limit, window.Offset)
	}
	rows, err := r.database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list chapter archives: %w", err)
	}
	archives := make([]canvas.ChapterArchive, 0)
	for rows.Next() {
		archive, err := scanChapterArchive(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		archives = append(archives, archive)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range archives {
		sections, err := r.listChapterArchiveSections(ctx, archives[index].ID)
		if err != nil {
			return nil, err
		}
		archives[index].Sections = sections
	}
	if currentOnly {
		r.ensureCurrentChapterArchiveProjections(ctx, archives)
	}
	return archives, nil
}

func (r *CanvasRepository) countChapterArchives(
	ctx context.Context,
	workID string,
	chapterOutlineNodeID string,
	currentOnly bool,
) (int64, error) {
	query := "SELECT COUNT(*) FROM chapter_archives WHERE work_id=?"
	arguments := []any{workID}
	if chapterOutlineNodeID != "" {
		query += " AND chapter_outline_node_id=?"
		arguments = append(arguments, chapterOutlineNodeID)
	}
	if currentOnly {
		query += " AND is_current=1 AND retracted_at=''"
	} else if chapterOutlineNodeID == "" {
		query += " AND retracted_at=''"
	}
	var total int64
	if err := r.database.QueryRowContext(ctx, query, arguments...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count chapter archives: %w", err)
	}
	return total, nil
}

func (r *CanvasRepository) ensureCurrentChapterArchiveProjections(ctx context.Context, archives []canvas.ChapterArchive) {
	for index := range archives {
		archive := &archives[index]
		projection, err := os.ReadFile(chapterArchiveProjectionPath(r.dataDirectory, *archive))
		if err == nil && bytes.Equal(projection, renderChapterArchiveProjection(*archive)) {
			if archive.ProjectionStatus != "ready" {
				if _, updateErr := r.database.ExecContext(ctx, `UPDATE chapter_archives SET projection_status='ready' WHERE id=?`, archive.ID); updateErr == nil {
					archive.ProjectionStatus = "ready"
				}
			}
			continue
		}
		projectionStatus := "pending"
		if writeErr := writeChapterArchiveProjection(r.dataDirectory, *archive); writeErr == nil {
			projectionStatus = "ready"
		}
		if _, updateErr := r.database.ExecContext(ctx, `UPDATE chapter_archives SET projection_status=? WHERE id=?`, projectionStatus, archive.ID); updateErr == nil {
			archive.ProjectionStatus = projectionStatus
		}
	}
}

func (r *CanvasRepository) listChapterArchiveSections(ctx context.Context, archiveID string) ([]canvas.ChapterArchiveSection, error) {
	rows, err := r.database.QueryContext(ctx, `SELECT archive_id,ordinal,section_outline_node_id,chapter_section_node_id,chapter_section_version_id,node_revision,title,summary,content,content_hash FROM chapter_archive_sections WHERE archive_id=? ORDER BY ordinal`, archiveID)
	if err != nil {
		return nil, fmt.Errorf("list chapter archive sections: %w", err)
	}
	defer rows.Close()
	sections := make([]canvas.ChapterArchiveSection, 0)
	for rows.Next() {
		var section canvas.ChapterArchiveSection
		if err := rows.Scan(&section.ArchiveID, &section.Ordinal, &section.SectionOutlineNodeID, &section.ChapterSectionNodeID, &section.ChapterSectionVersionID, &section.NodeRevision, &section.Title, &section.Summary, &section.Content, &section.ContentHash); err != nil {
			return nil, err
		}
		sections = append(sections, section)
	}
	return sections, rows.Err()
}

func scanChapterArchive(scanner rowScanner) (canvas.ChapterArchive, error) {
	var archive canvas.ChapterArchive
	var isCurrent int
	var createdAt, supersededAt, retractedAt string
	if err := scanner.Scan(&archive.ID, &archive.WorkID, &archive.ChapterOutlineNodeID, &archive.Revision, &archive.RunID, &archive.OutlineVersionID, &archive.OutlineRevision, &archive.OutlineTitle, &archive.OutlineContent, &archive.Summary, &archive.SourceDigest, &isCurrent, &archive.ProjectionStatus, &createdAt, &supersededAt, &retractedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return canvas.ChapterArchive{}, canvas.ErrNodeNotFound
		}
		return canvas.ChapterArchive{}, err
	}
	archive.IsCurrent = isCurrent != 0
	var err error
	archive.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return canvas.ChapterArchive{}, fmt.Errorf("parse chapter archive created time: %w", err)
	}
	if supersededAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, supersededAt)
		if err != nil {
			return canvas.ChapterArchive{}, fmt.Errorf("parse chapter archive superseded time: %w", err)
		}
		archive.SupersededAt = &parsed
	}
	if retractedAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, retractedAt)
		if err != nil {
			return canvas.ChapterArchive{}, fmt.Errorf("parse chapter archive retracted time: %w", err)
		}
		archive.RetractedAt = &parsed
	}
	return archive, nil
}

func (r *CanvasRepository) RetractChapterArchive(ctx context.Context, workID, archiveID string) error {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin retract chapter archive: %w", err)
	}
	defer transaction.Rollback()

	var chapterOutlineNodeID, runID, retractedAt string
	var isCurrent int
	err = transaction.QueryRowContext(ctx, `
SELECT chapter_outline_node_id,run_id,is_current,retracted_at
FROM chapter_archives
WHERE work_id=? AND id=?`, workID, archiveID).Scan(&chapterOutlineNodeID, &runID, &isCurrent, &retractedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return canvas.ErrChapterArchiveNotFound
	}
	if err != nil {
		return fmt.Errorf("read chapter archive for retraction: %w", err)
	}
	if isCurrent == 0 || retractedAt != "" {
		return canvas.ErrChapterArchiveNotCurrent
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := transaction.ExecContext(ctx, `
UPDATE agent_candidates
SET status='rejected',decided_at=?
WHERE work_id=? AND run_id=? AND status='pending'`, now, workID, runID); err != nil {
		return fmt.Errorf("reject pending archive candidates: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE chapter_archives
SET is_current=0,retracted_at=?
WHERE work_id=? AND id=? AND is_current=1 AND retracted_at=''`, now, workID, archiveID)
	if err != nil {
		return fmt.Errorf("retract chapter archive: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read retracted chapter archive count: %w", err)
	}
	if updated != 1 {
		return canvas.ErrChapterArchiveNotCurrent
	}

	projectionPath := chapterArchiveProjectionPath(r.dataDirectory, canvas.ChapterArchive{
		WorkID: workID, ChapterOutlineNodeID: chapterOutlineNodeID,
	})
	if err := os.Remove(projectionPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove chapter archive projection: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit chapter archive retraction: %w", err)
	}
	return nil
}

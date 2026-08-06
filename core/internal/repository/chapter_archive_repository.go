package repository

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"warmnote/core/internal/canvas"
)

func (r *CanvasRepository) ListCurrentChapterArchives(ctx context.Context, workID string) ([]canvas.ChapterArchive, error) {
	return r.listChapterArchives(ctx, workID, "", true)
}

func (r *CanvasRepository) ListChapterArchiveHistory(ctx context.Context, workID, chapterOutlineNodeID string) ([]canvas.ChapterArchive, error) {
	return r.listChapterArchives(ctx, workID, chapterOutlineNodeID, false)
}

func (r *CanvasRepository) listChapterArchives(ctx context.Context, workID, chapterOutlineNodeID string, currentOnly bool) ([]canvas.ChapterArchive, error) {
	query := `SELECT id,work_id,chapter_outline_node_id,revision,run_id,outline_version_id,outline_revision,outline_title,outline_content,summary,source_digest,is_current,projection_status,created_at,superseded_at FROM chapter_archives WHERE work_id=?`
	arguments := []any{workID}
	if chapterOutlineNodeID != "" {
		query += " AND chapter_outline_node_id=?"
		arguments = append(arguments, chapterOutlineNodeID)
	}
	if currentOnly {
		query += " AND is_current=1"
	}
	query += " ORDER BY created_at, revision"
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
	var createdAt, supersededAt string
	if err := scanner.Scan(&archive.ID, &archive.WorkID, &archive.ChapterOutlineNodeID, &archive.Revision, &archive.RunID, &archive.OutlineVersionID, &archive.OutlineRevision, &archive.OutlineTitle, &archive.OutlineContent, &archive.Summary, &archive.SourceDigest, &isCurrent, &archive.ProjectionStatus, &createdAt, &supersededAt); err != nil {
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
	return archive, nil
}

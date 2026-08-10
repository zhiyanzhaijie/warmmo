package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"warmmo/core/internal/domain/canvas"
)

const (
	chapterLayoutColumnGap = 352.0
	chapterLayoutRowGap    = 216.0
)

type chapterLayoutSection struct {
	SectionOutlineNodeID string
	ChapterSectionNodeID string
}

func (r *CanvasRepository) LayoutChapter(
	ctx context.Context,
	workID string,
	chapterOutlineNodeID string,
) ([]canvas.NodePosition, error) {
	transaction, err := r.database.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin chapter layout: %w", err)
	}
	defer transaction.Rollback()

	sections, err := readCurrentChapterLayoutSections(ctx, transaction, workID, chapterOutlineNodeID)
	if err != nil {
		return nil, err
	}
	positions, before, err := layoutChapterNodes(ctx, transaction, workID, chapterOutlineNodeID, sections)
	if err != nil {
		return nil, err
	}
	if len(positions) > 0 {
		if err := applyNodePositions(ctx, transaction, workID, positions); err != nil {
			return nil, err
		}
		if err := appendCanvasAction(ctx, transaction, workID, actionMoveNodes, "整理章节布局", moveNodesActionPayload{
			Before: before,
			After:  positions,
		}); err != nil {
			return nil, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit chapter layout: %w", err)
	}
	return positions, nil
}

func readCurrentChapterLayoutSections(
	ctx context.Context,
	transaction *sql.Tx,
	workID string,
	chapterOutlineNodeID string,
) ([]chapterLayoutSection, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT section.section_outline_node_id, section.chapter_section_node_id
FROM chapter_archives archive
JOIN chapter_archive_sections section ON section.archive_id = archive.id
WHERE archive.work_id = ? AND archive.chapter_outline_node_id = ? AND archive.is_current = 1
ORDER BY section.ordinal`, workID, chapterOutlineNodeID)
	if err != nil {
		return nil, fmt.Errorf("read chapter layout sections: %w", err)
	}
	defer rows.Close()

	sections := make([]chapterLayoutSection, 0)
	for rows.Next() {
		var section chapterLayoutSection
		if err := rows.Scan(&section.SectionOutlineNodeID, &section.ChapterSectionNodeID); err != nil {
			return nil, fmt.Errorf("scan chapter layout section: %w", err)
		}
		sections = append(sections, section)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chapter layout sections: %w", err)
	}
	if len(sections) == 0 {
		return nil, canvas.ErrInvalidChapterArchive
	}
	return sections, nil
}

func layoutChapterNodes(
	ctx context.Context,
	transaction *sql.Tx,
	workID string,
	chapterOutlineNodeID string,
	sections []chapterLayoutSection,
) ([]canvas.NodePosition, []canvas.NodePosition, error) {
	var chapterKind canvas.NodeKind
	var chapterX, chapterY float64
	err := transaction.QueryRowContext(ctx, `
SELECT kind, x, y FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, chapterOutlineNodeID).
		Scan(&chapterKind, &chapterX, &chapterY)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, canvas.ErrNodeNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read chapter layout origin: %w", err)
	}
	if chapterKind != canvas.NodeKindChapterOutline {
		return nil, nil, canvas.ErrInvalidNode
	}

	startY := chapterY - float64(len(sections)-1)*chapterLayoutRowGap/2
	after := make([]canvas.NodePosition, 0, len(sections)*2)
	before := make([]canvas.NodePosition, 0, len(sections)*2)
	for index, section := range sections {
		rowY := startY + float64(index)*chapterLayoutRowGap
		for column, nodeID := range []string{section.SectionOutlineNodeID, section.ChapterSectionNodeID} {
			var current canvas.NodePosition
			current.NodeID = nodeID
			if err := transaction.QueryRowContext(ctx, `
SELECT x, y FROM canvas_nodes WHERE work_id = ? AND id = ?`, workID, nodeID).Scan(&current.X, &current.Y); errors.Is(err, sql.ErrNoRows) {
				return nil, nil, canvas.ErrNodeNotFound
			} else if err != nil {
				return nil, nil, fmt.Errorf("read chapter layout node: %w", err)
			}
			next := canvas.NodePosition{
				NodeID: nodeID,
				X:      chapterX + float64(column+1)*chapterLayoutColumnGap,
				Y:      rowY,
			}
			if current.X == next.X && current.Y == next.Y {
				continue
			}
			before = append(before, current)
			after = append(after, next)
		}
	}
	return after, before, nil
}

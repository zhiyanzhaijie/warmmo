package persistence

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

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

func (r *CanvasRepository) LayoutChapter(ctx context.Context, workID, chapterOutlineNodeID string) ([]canvas.NodePosition, error) {
	var positions []canvas.NodePosition
	err := r.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sections, err := readCurrentChapterLayoutSections(tx, workID, chapterOutlineNodeID)
		if err != nil {
			return err
		}
		var before []canvas.NodePosition
		positions, before, err = layoutChapterNodes(tx, workID, chapterOutlineNodeID, sections)
		if err != nil || len(positions) == 0 {
			return err
		}
		if err := applyNodePositions(tx, workID, positions); err != nil {
			return err
		}
		return appendCanvasAction(tx, workID, actionMoveNodes, "整理章节布局", moveNodesActionPayload{Before: before, After: positions})
	})
	return positions, err
}

func readCurrentChapterLayoutSections(tx *gorm.DB, workID, chapterOutlineNodeID string) ([]chapterLayoutSection, error) {
	var archive chapterArchiveModel
	if err := tx.Preload("Sections", func(db *gorm.DB) *gorm.DB { return db.Order("ordinal") }).Where("work_id = ? AND chapter_outline_node_id = ? AND is_current = ? AND retracted_at IS NULL", workID, chapterOutlineNodeID, true).First(&archive).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, canvas.ErrInvalidChapterArchive
	} else if err != nil {
		return nil, fmt.Errorf("read chapter layout sections: %w", err)
	}
	if len(archive.Sections) == 0 {
		return nil, canvas.ErrInvalidChapterArchive
	}
	sections := make([]chapterLayoutSection, len(archive.Sections))
	for i, section := range archive.Sections {
		sections[i] = chapterLayoutSection{SectionOutlineNodeID: section.SectionOutlineNodeID, ChapterSectionNodeID: section.ChapterSectionNodeID}
	}
	return sections, nil
}

func layoutChapterNodes(tx *gorm.DB, workID, chapterOutlineNodeID string, sections []chapterLayoutSection) ([]canvas.NodePosition, []canvas.NodePosition, error) {
	chapter, err := getCanvasNode(tx, workID, chapterOutlineNodeID)
	if err != nil {
		return nil, nil, err
	}
	if canvas.NodeKind(chapter.Kind) != canvas.NodeKindChapterOutline {
		return nil, nil, canvas.ErrInvalidNode
	}
	ids := make([]string, 0, len(sections)*2)
	for _, section := range sections {
		ids = append(ids, section.SectionOutlineNodeID, section.ChapterSectionNodeID)
	}
	var models []canvasNodeModel
	if err := tx.Select("id", "x", "y").Where("work_id = ? AND id IN ?", workID, ids).Find(&models).Error; err != nil {
		return nil, nil, fmt.Errorf("read chapter layout nodes: %w", err)
	}
	if len(models) != len(uniqueStrings(ids)) {
		return nil, nil, canvas.ErrNodeNotFound
	}
	byID := make(map[string]canvasNodeModel, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}
	startY := chapter.Y - float64(len(sections)-1)*chapterLayoutRowGap/2
	after, before := make([]canvas.NodePosition, 0, len(ids)), make([]canvas.NodePosition, 0, len(ids))
	for index, section := range sections {
		rowY := startY + float64(index)*chapterLayoutRowGap
		for column, nodeID := range []string{section.SectionOutlineNodeID, section.ChapterSectionNodeID} {
			current, ok := byID[nodeID]
			if !ok {
				return nil, nil, canvas.ErrNodeNotFound
			}
			next := canvas.NodePosition{NodeID: nodeID, X: chapter.X + float64(column+1)*chapterLayoutColumnGap, Y: rowY}
			if current.X == next.X && current.Y == next.Y {
				continue
			}
			before = append(before, canvas.NodePosition{NodeID: nodeID, X: current.X, Y: current.Y})
			after = append(after, next)
		}
	}
	return after, before, nil
}

package canvas

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const MaxSectionsPerChapter = 100

var ErrInvalidSectionOutline = errors.New("invalid section outline")

type SectionOutlineData struct {
	Ordinal      int      `json:"ordinal"`
	Purpose      string   `json:"purpose"`
	Viewpoint    string   `json:"viewpoint"`
	TargetLength int      `json:"targetLength"`
	OpeningState string   `json:"openingState"`
	Beats        []string `json:"beats"`
	Conflict     string   `json:"conflict"`
	TurningPoint string   `json:"turningPoint"`
	EndingState  string   `json:"endingState"`
	Hook         string   `json:"hook"`
}

type PlannedSection struct {
	Title   string             `json:"title"`
	Outline SectionOutlineData `json:"outline"`
}

type SectionOutlineBatch struct {
	ChapterOutlineNodeID string           `json:"chapterOutlineNodeId"`
	Sections             []PlannedSection `json:"sections"`
}

func (batch SectionOutlineBatch) Validate() error {
	if strings.TrimSpace(batch.ChapterOutlineNodeID) == "" {
		return fmt.Errorf("%w: chapter outline node id is required", ErrInvalidSectionOutline)
	}
	if len(batch.Sections) == 0 || len(batch.Sections) > MaxSectionsPerChapter {
		return fmt.Errorf("%w: sections must contain 1 to %d items", ErrInvalidSectionOutline, MaxSectionsPerChapter)
	}
	ordinals := make(map[int]struct{}, len(batch.Sections))
	for index, section := range batch.Sections {
		if err := validatePlannedSection(section); err != nil {
			return fmt.Errorf("%w: section %d: %v", ErrInvalidSectionOutline, index+1, err)
		}
		if _, exists := ordinals[section.Outline.Ordinal]; exists {
			return fmt.Errorf("%w: duplicate ordinal %d", ErrInvalidSectionOutline, section.Outline.Ordinal)
		}
		ordinals[section.Outline.Ordinal] = struct{}{}
	}
	for ordinal := 1; ordinal <= len(batch.Sections); ordinal++ {
		if _, exists := ordinals[ordinal]; !exists {
			return fmt.Errorf("%w: ordinals must be contiguous from 1", ErrInvalidSectionOutline)
		}
	}
	return nil
}

func DecodeSectionOutlineBatch(content string) (SectionOutlineBatch, error) {
	var batch SectionOutlineBatch
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&batch); err != nil {
		return SectionOutlineBatch{}, fmt.Errorf("%w: decode batch: %v", ErrInvalidSectionOutline, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return SectionOutlineBatch{}, fmt.Errorf("%w: batch must contain one JSON object", ErrInvalidSectionOutline)
	}
	if err := batch.Validate(); err != nil {
		return SectionOutlineBatch{}, err
	}
	return batch, nil
}

func FormatSectionOutline(outline SectionOutlineData) string {
	var content strings.Builder
	writeOutlineField(&content, "序号", strconv.Itoa(outline.Ordinal))
	writeOutlineField(&content, "叙事目的", outline.Purpose)
	writeOutlineField(&content, "视角", outline.Viewpoint)
	writeOutlineField(&content, "目标字数", strconv.Itoa(outline.TargetLength))
	writeOutlineField(&content, "开场状态", outline.OpeningState)
	content.WriteString("## 情节节拍\n")
	for _, beat := range outline.Beats {
		content.WriteString("- ")
		content.WriteString(strings.TrimSpace(beat))
		content.WriteByte('\n')
	}
	content.WriteByte('\n')
	writeOutlineField(&content, "核心冲突", outline.Conflict)
	writeOutlineField(&content, "转折点", outline.TurningPoint)
	writeOutlineField(&content, "结束状态", outline.EndingState)
	writeOutlineField(&content, "结尾钩子", outline.Hook)
	return strings.TrimSpace(content.String())
}

func writeOutlineField(content *strings.Builder, label, value string) {
	content.WriteString("## ")
	content.WriteString(label)
	content.WriteByte('\n')
	content.WriteString(strings.TrimSpace(value))
	content.WriteString("\n\n")
}

func validatePlannedSection(section PlannedSection) error {
	outline := section.Outline
	if strings.TrimSpace(section.Title) == "" {
		return errors.New("title is required")
	}
	if outline.Ordinal < 1 {
		return errors.New("ordinal must be positive")
	}
	if strings.TrimSpace(outline.Purpose) == "" || strings.TrimSpace(outline.Viewpoint) == "" ||
		strings.TrimSpace(outline.OpeningState) == "" || strings.TrimSpace(outline.Conflict) == "" ||
		strings.TrimSpace(outline.TurningPoint) == "" || strings.TrimSpace(outline.EndingState) == "" ||
		strings.TrimSpace(outline.Hook) == "" {
		return errors.New("purpose, viewpoint, openingState, conflict, turningPoint, endingState and hook are required")
	}
	if len(outline.Beats) == 0 {
		return errors.New("at least one beat is required")
	}
	for _, beat := range outline.Beats {
		if strings.TrimSpace(beat) == "" {
			return errors.New("beats cannot contain empty values")
		}
	}
	if outline.TargetLength < 1 {
		return errors.New("targetLength must be positive")
	}
	return nil
}

package canvas

import (
	"errors"
	"testing"
)

func TestNodeKindCreationPolicy(t *testing.T) {
	t.Parallel()

	for _, kind := range []NodeKind{NodeKindChapterOutline, NodeKindCharacter, NodeKindEvent} {
		if !IsManuallyCreatableNodeKind(kind) || IsDerivedNodeKind(kind) {
			t.Fatalf("manual kind %q has invalid creation policy", kind)
		}
	}
	for _, kind := range []NodeKind{NodeKindSectionOutline, NodeKindChapterSection, NodeKindManuscript} {
		if IsManuallyCreatableNodeKind(kind) || !IsDerivedNodeKind(kind) {
			t.Fatalf("derived kind %q has invalid creation policy", kind)
		}
	}
	if _, valid := ParseNodeKind(" section-outline "); !valid {
		t.Fatal("trimmed section-outline was not parsed")
	}
}

func TestSectionOutlineBatchValidation(t *testing.T) {
	t.Parallel()

	valid := SectionOutlineBatch{
		ChapterOutlineNodeID: "chapter-1",
		Sections: []PlannedSection{
			{Title: "进入工厂", Outline: SectionOutlineData{
				Ordinal: 1, Purpose: "建立目标", Viewpoint: "主角", TargetLength: 1600,
				OpeningState: "主角抵达工厂", Beats: []string{"进入厂区", "检查设备"}, Conflict: "设备拒绝响应",
				TurningPoint: "发现人为修改", EndingState: "发现异常", Hook: "信号指向禁区",
			}},
			{Title: "异常信号", Outline: SectionOutlineData{
				Ordinal: 2, Purpose: "推动冲突", Viewpoint: "主角", TargetLength: 1400,
				OpeningState: "主角开始调查", Beats: []string{"追踪信号"}, Conflict: "守卫开始搜索",
				TurningPoint: "信号来自同伴", EndingState: "确认信号来源", Hook: "同伴发出警告",
			}},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid section outline batch: %v", err)
	}

	invalid := valid
	invalid.Sections = append([]PlannedSection(nil), valid.Sections...)
	invalid.Sections[1].Outline.Ordinal = 3
	if err := invalid.Validate(); !errors.Is(err, ErrInvalidSectionOutline) {
		t.Fatalf("non-contiguous ordinals error = %v", err)
	}
}

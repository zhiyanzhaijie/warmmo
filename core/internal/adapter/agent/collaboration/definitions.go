package collaboration

import (
	"time"

	appharness "warmmo/core/internal/application/harness"
)

const (
	PlannerDefinitionID        = "writing.planner"
	CreatorDefinitionID        = "writing.creator"
	WriterDefinitionID         = "writing.writer"
	NodeUpdateDefinitionID     = "writing.node_update"
	SectionOutlineDefinitionID = "writing.section_outline_batch"
	ChapterSectionDefinitionID = "writing.chapter_section"
	ChapterArchiveDefinitionID = "writing.chapter_archive"

	CollaborationPlanArtifact   = "collaboration_plan_v1"
	ProposalArtifact            = "proposal_v1"
	AdviceArtifact              = "advice_v1"
	DraftArtifact               = "draft_v1"
	PolishedProseArtifact       = "polished_prose_v1"
	NodeUpdateArtifact          = "node_update_v1"
	SectionOutlineBatchArtifact = "section_outline_batch_v1"
	ChapterSectionArtifact      = "chapter_section_v1"
	ChapterArchiveArtifact      = "chapter_archive_v1"
)

func Definitions() []appharness.AgentDefinition {
	readTools := []string{"canvas.get_nodes", "canvas.search_context", "story_spine.context", "workspace.search"}
	definitions := []appharness.AgentDefinition{
		{
			ID: PlannerDefinitionID, Version: "1", Name: "writing_planner",
			Description: "Plans one Warmmo writing collaboration turn.", Tier: appharness.AgentTierReasoning,
			Model: appharness.ModelPolicy{Hint: "reasoning"}, Prompt: appharness.PromptSpec{ID: "writing.planner.v1"},
			Tools: append([]string(nil), readTools...), ControlTools: []string{"ask_user"},
			AllowedChildren: []string{CreatorDefinitionID, WriterDefinitionID},
			Budget:          budget(8, 12, 1, 3*time.Minute),
			Context:         contextPolicy("recall", 64*1024, 8*1024, 8),
			Memory:          appharness.MemoryPolicy{Recall: true, Remember: true},
			Output: appharness.OutputContract{Kind: appharness.OutputKindArtifact, Artifacts: []appharness.ArtifactSchema{
				{Kind: CollaborationPlanArtifact, SchemaVersion: "1", Schema: collaborationPlanSchema()},
			}},
		},
		{
			ID: CreatorDefinitionID, Version: "1", Name: "writing_creator",
			Description: "Creates one typed writing artifact from an approved plan.", Tier: appharness.AgentTierWorker,
			Model: appharness.ModelPolicy{Hint: "worker"}, Prompt: appharness.PromptSpec{ID: "writing.creator.v1"},
			Tools: append([]string(nil), readTools...), ControlTools: []string{"ask_user"},
			Budget:  budget(8, 12, 1, 3*time.Minute),
			Context: contextPolicy("recall", 64*1024, 16*1024, 8),
			Memory:  appharness.MemoryPolicy{Recall: true, Remember: true},
			Output: appharness.OutputContract{Kind: appharness.OutputKindArtifact, Artifacts: []appharness.ArtifactSchema{
				{Kind: ProposalArtifact, SchemaVersion: "1", Schema: proposalSchema()},
				{Kind: AdviceArtifact, SchemaVersion: "1", Schema: nonEmptyStringSchema()},
				{Kind: DraftArtifact, SchemaVersion: "1", Schema: nonEmptyStringSchema()},
			}},
		},
		{
			ID: WriterDefinitionID, Version: "1", Name: "writing_writer",
			Description: "Polishes a prose draft without changing its factual commitments.", Tier: appharness.AgentTierWorker,
			Model: appharness.ModelPolicy{Hint: "worker"}, Prompt: appharness.PromptSpec{ID: "writing.writer.v1"},
			Tools:   []string{"canvas.get_nodes"},
			Budget:  budget(4, 4, 1, 2*time.Minute),
			Context: contextPolicy("none", 32*1024, 16*1024, 6),
			Output: appharness.OutputContract{Kind: appharness.OutputKindArtifact, Artifacts: []appharness.ArtifactSchema{
				{Kind: PolishedProseArtifact, SchemaVersion: "1", Schema: nonEmptyStringSchema()},
			}},
		},
	}
	definitions = append(definitions,
		nodeUpdateDefinition(), sectionOutlineDefinition(), chapterSectionDefinition(), chapterArchiveDefinition(),
	)
	return definitions
}

func nodeUpdateDefinition() appharness.AgentDefinition {
	return appharness.AgentDefinition{
		ID: NodeUpdateDefinitionID, Version: "1", Name: "writing_node_update",
		Description: "Updates one existing canvas node from a typed replacement artifact.", Tier: appharness.AgentTierWorker,
		Model: appharness.ModelPolicy{Hint: "worker"}, Prompt: appharness.PromptSpec{ID: "writing.node_update.v1"},
		Tools: []string{"canvas.get_nodes", "workspace.search", "story_spine.context"}, ControlTools: []string{"ask_user"},
		Budget: budget(8, 12, 1, 3*time.Minute), Context: contextPolicy("recall", 64*1024, 8*1024, 8),
		Memory: appharness.MemoryPolicy{Recall: true, Remember: true},
		Output: appharness.OutputContract{Kind: appharness.OutputKindArtifact, Artifacts: []appharness.ArtifactSchema{{
			Kind: NodeUpdateArtifact, SchemaVersion: "1", Schema: nodeUpdateSchema(),
		}}},
	}
}

func sectionOutlineDefinition() appharness.AgentDefinition {
	return derivationDefinition(SectionOutlineDefinitionID, "writing_section_outline", "writing.section_outline_batch.v1", SectionOutlineBatchArtifact, "Splits one chapter outline into typed section outline nodes.", sectionOutlineBatchSchema())
}

func chapterSectionDefinition() appharness.AgentDefinition {
	return derivationDefinition(ChapterSectionDefinitionID, "writing_chapter_section", "writing.chapter_section.v1", ChapterSectionArtifact, "Writes one typed chapter section node.", nodeUpdateSchema())
}

func chapterArchiveDefinition() appharness.AgentDefinition {
	return derivationDefinition(ChapterArchiveDefinitionID, "writing_chapter_archive", "writing.chapter_archive.v1", ChapterArchiveArtifact, "Archives one chapter and proposes entity version updates.", chapterArchiveSchema())
}

func derivationDefinition(id, name, promptID, artifactKind, description string, schema map[string]any) appharness.AgentDefinition {
	return appharness.AgentDefinition{
		ID: id, Version: "1", Name: name, Description: description, Tier: appharness.AgentTierWorker,
		Model: appharness.ModelPolicy{Hint: "worker"}, Prompt: appharness.PromptSpec{ID: promptID},
		Tools: []string{"canvas.get_nodes", "canvas.search_context", "story_spine.context", "workspace.search"}, ControlTools: []string{"ask_user"},
		Budget: budget(8, 12, 1, 4*time.Minute), Context: contextPolicy("recall", 64*1024, 16*1024, 8),
		Memory: appharness.MemoryPolicy{Recall: true, Remember: true},
		Output: appharness.OutputContract{Kind: appharness.OutputKindArtifact, Artifacts: []appharness.ArtifactSchema{{
			Kind: artifactKind, SchemaVersion: "1", Schema: schema,
		}}},
	}
}

func nodeUpdateSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"title":   map[string]any{"type": "string", "minLength": 1},
		"content": map[string]any{"type": "string", "minLength": 1},
	}, "required": []string{"title", "content"}, "additionalProperties": false}
}

func sectionOutlineBatchSchema() map[string]any {
	nonEmpty := map[string]any{"type": "string", "minLength": 1}
	outline := map[string]any{"type": "object", "properties": map[string]any{
		"ordinal": map[string]any{"type": "integer", "minimum": 1}, "purpose": nonEmpty,
		"viewpoint": nonEmpty, "targetLength": map[string]any{"type": "integer", "minimum": 1},
		"openingState": nonEmpty, "beats": map[string]any{"type": "array", "minItems": 1, "items": nonEmpty},
		"conflict": nonEmpty, "turningPoint": nonEmpty, "endingState": nonEmpty, "hook": nonEmpty,
	}, "required": []string{"ordinal", "purpose", "viewpoint", "targetLength", "openingState", "beats", "conflict", "turningPoint", "endingState", "hook"}, "additionalProperties": false}
	section := map[string]any{"type": "object", "properties": map[string]any{
		"title": nonEmpty, "outline": outline,
	}, "required": []string{"title", "outline"}, "additionalProperties": false}
	return map[string]any{"type": "object", "properties": map[string]any{
		"chapterOutlineNodeId": nonEmpty,
		"sections":             map[string]any{"type": "array", "minItems": 1, "maxItems": 100, "items": section},
	}, "required": []string{"chapterOutlineNodeId", "sections"}, "additionalProperties": false}
}

func chapterArchiveSchema() map[string]any {
	nonEmpty := map[string]any{"type": "string", "minLength": 1}
	section := map[string]any{"type": "object", "properties": map[string]any{
		"sectionOutlineNodeId": nonEmpty, "chapterSectionNodeId": nonEmpty,
		"nodeRevision": map[string]any{"type": "integer", "minimum": 1},
		"ordinal":      map[string]any{"type": "integer", "minimum": 1}, "summary": nonEmpty,
	}, "required": []string{"sectionOutlineNodeId", "chapterSectionNodeId", "nodeRevision", "ordinal", "summary"}, "additionalProperties": false}
	proposal := map[string]any{"type": "object", "properties": map[string]any{
		"nodeId": nonEmpty, "kind": nonEmpty, "title": nonEmpty, "content": nonEmpty,
		"changeScore": map[string]any{"type": "number", "minimum": 0, "maximum": 10}, "reason": nonEmpty,
	}, "required": []string{"nodeId", "kind", "title", "content", "changeScore", "reason"}, "additionalProperties": false}
	archive := map[string]any{"type": "object", "properties": map[string]any{
		"summary": nonEmpty, "sections": map[string]any{"type": "array", "minItems": 1, "items": section},
	}, "required": []string{"summary", "sections"}, "additionalProperties": false}
	return map[string]any{"type": "object", "properties": map[string]any{
		"archive": archive, "proposals": map[string]any{"type": "array", "items": proposal},
	}, "required": []string{"archive", "proposals"}, "additionalProperties": false}
}

func NewDefinitionRegistry() (*appharness.DefinitionRegistry, error) {
	registry, err := appharness.NewDefinitionRegistry(Definitions()...)
	if err != nil {
		return nil, err
	}
	if err := registry.ValidateGraph(); err != nil {
		return nil, err
	}
	return registry, nil
}

func budget(modelCalls, toolCalls, sideEffectCalls int, duration time.Duration) appharness.BudgetPolicy {
	return appharness.BudgetPolicy{
		MaxModelCalls: modelCalls, MaxToolCalls: toolCalls, MaxSideEffectCalls: sideEffectCalls,
		MaxDuration: duration, MaxToolResultBytes: 64 * 1024,
	}
}

func contextPolicy(memory string, toolResultBytes, reservedOutputTokens, recentContents int) appharness.ContextPolicy {
	return appharness.ContextPolicy{
		History: "session", Memory: memory, MaxToolResultBytes: toolResultBytes,
		ModelWindowTokens: 64 * 1024, ReservedOutputTokens: reservedOutputTokens,
		SafetyMarginTokens: 2048, RecentContents: recentContents,
	}
}

func collaborationPlanSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"intent":            map[string]any{"type": "string", "minLength": 1},
			"brief":             map[string]any{"type": "string", "minLength": 1},
			"contextQuery":      map[string]any{"type": "string"},
			"creatorTarget":     map[string]any{"type": "string", "enum": []string{"collaborative-targeted", "collaborative-explore"}},
			"creatorSkillId":    map[string]any{"type": "string", "enum": []string{"chapter-creator", "entity-creator", "prose-creator", "story-brainstorm"}},
			"outputKind":        map[string]any{"type": "string", "enum": []string{"proposal", "advice", "prose"}},
			"writerRequired":    map[string]any{"type": "boolean"},
			"writerInstruction": map[string]any{"type": "string"},
		},
		"required":             []string{"intent", "brief", "contextQuery", "creatorTarget", "creatorSkillId", "outputKind", "writerRequired", "writerInstruction"},
		"additionalProperties": false,
	}
}

func proposalSchema() map[string]any {
	node := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"clientId": map[string]any{"type": "string", "minLength": 1},
			"kind":     map[string]any{"type": "string", "minLength": 1},
			"title":    map[string]any{"type": "string", "minLength": 1},
			"content":  map[string]any{"type": "string", "minLength": 1},
		},
		"required": []string{"clientId", "kind", "title", "content"}, "additionalProperties": false,
	}
	update := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"nodeId":       map[string]any{"type": "string", "minLength": 1},
			"baseRevision": map[string]any{"type": "integer", "minimum": 1},
			"title":        map[string]any{"type": "string", "minLength": 1},
			"content":      map[string]any{"type": "string", "minLength": 1},
		},
		"required": []string{"nodeId", "baseRevision", "title", "content"}, "additionalProperties": false,
	}
	edge := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sourceId": map[string]any{"type": "string", "minLength": 1},
			"targetId": map[string]any{"type": "string", "minLength": 1},
			"kind":     map[string]any{"type": "string", "minLength": 1},
		},
		"required": []string{"sourceId", "targetId", "kind"}, "additionalProperties": false,
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"baseRevisions": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "integer", "minimum": 1}},
			"nodes":         map[string]any{"type": "array", "items": node, "maxItems": 1},
			"updates":       map[string]any{"type": "array", "items": update},
			"edges":         map[string]any{"type": "array", "items": edge},
			"reasons":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"questions":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required":             []string{"baseRevisions", "nodes", "updates", "edges", "reasons", "questions"},
		"additionalProperties": false,
		"anyOf": []any{
			map[string]any{"properties": map[string]any{"nodes": map[string]any{"minItems": 1}}},
			map[string]any{"properties": map[string]any{"updates": map[string]any{"minItems": 1}}},
		},
	}
}

func nonEmptyStringSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1}
}

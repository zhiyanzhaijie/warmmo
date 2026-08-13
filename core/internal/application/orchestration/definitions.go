package orchestration

import (
	"fmt"
	"time"
	"warmmo/core/internal/application/writing"
	"warmmo/core/internal/domain/canvas"

	appharness "warmmo/core/internal/application/harness"
)

const (
	CanvasOrchestratorDefinitionID = "canvas_orchestrator"
	PlannerDefinitionID            = "writing_planner"
	CreatorDefinitionID            = "writing_creator"
	EntityCreatorDefinitionID      = "writing_entity_creator"
	ChapterCreatorDefinitionID     = "writing_chapter_creator"
	ProseCreatorDefinitionID       = "writing_prose_creator"
	BrainstormDefinitionID         = "writing_brainstorm"
	WriterDefinitionID             = "writing_writer"
	NodeUpdateDefinitionID         = "writing_node_update"
	SectionOutlineDefinitionID     = "writing_section_outline_batch"
	ChapterSectionDefinitionID     = "writing_chapter_section"
	ChapterArchiveDefinitionID     = "writing_chapter_archive"

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

type directCreatorRegistration struct {
	AgentID      string
	Name         string
	SkillID      string
	ArtifactKind string
}

var directCreatorRegistrations = []directCreatorRegistration{
	{AgentID: EntityCreatorDefinitionID, Name: "writing_entity_creator", SkillID: "entity-creator", ArtifactKind: ProposalArtifact},
	{AgentID: ChapterCreatorDefinitionID, Name: "writing_chapter_creator", SkillID: "chapter-creator", ArtifactKind: ProposalArtifact},
	{AgentID: ProseCreatorDefinitionID, Name: "writing_prose_creator", SkillID: "prose-creator", ArtifactKind: DraftArtifact},
	{AgentID: BrainstormDefinitionID, Name: "writing_brainstorm", SkillID: "story-brainstorm", ArtifactKind: AdviceArtifact},
}

var rootChildrenByTarget = map[string][]string{
	writing.TargetCollaborativeExplore:  {PlannerDefinitionID, BrainstormDefinitionID},
	writing.TargetCollaborativeTargeted: {PlannerDefinitionID, EntityCreatorDefinitionID, ChapterCreatorDefinitionID, ProseCreatorDefinitionID},
}

func RootChildren(target string) ([]string, error) {
	children, ok := rootChildrenByTarget[target]
	if !ok {
		return nil, fmt.Errorf("no root delegation policy for target %q", target)
	}
	return append([]string(nil), children...), nil
}

// AgentToolName returns the stable model-facing function name for an agent ID.
// IDs are application identifiers; names are the public tool protocol.
func Definitions() []appharness.AgentDefinition {
	readTools := []string{"canvas.get_nodes", "canvas.search_context", "story_spine.context", "workspace.search"}
	definitions := []appharness.AgentDefinition{
		{
			ID: CanvasOrchestratorDefinitionID, Version: "1", Name: CanvasOrchestratorDefinitionID,
			Description: "Handles one canvas conversation turn and delegates only when specialist work is required.", Tier: appharness.AgentTierChat,
			Model: appharness.ModelPolicy{Hint: "chat"}, Prompt: appharness.PromptSpec{ID: "canvas.orchestrator.v1"},
			Tools: append([]string(nil), readTools...), ControlTools: []string{"ask_user"},
			Budget:  budget(8, 10, 1, 3*time.Minute),
			Context: contextPolicy("recall", 64*1024, 8*1024),
			Memory:  appharness.MemoryPolicy{Recall: true, Remember: true},
		},
		{
			ID: PlannerDefinitionID, Version: "1", Name: PlannerDefinitionID,
			Description: "Plans one Warmmo writing collaboration turn.", Tier: appharness.AgentTierReasoning,
			Model: appharness.ModelPolicy{Hint: "reasoning"}, Prompt: appharness.PromptSpec{ID: "writing.planner.v1"},
			Tools:   append([]string(nil), readTools...),
			Budget:  budget(8, 12, 1, 3*time.Minute),
			Context: contextPolicy("recall", 64*1024, 8*1024),
			Memory:  appharness.MemoryPolicy{Recall: true, Remember: true},
		},
		{
			ID: CreatorDefinitionID, Version: "1", Name: CreatorDefinitionID,
			Description: "Creates one typed writing artifact from an approved plan.", Tier: appharness.AgentTierWorker,
			Model: appharness.ModelPolicy{Hint: "worker"}, Prompt: appharness.PromptSpec{ID: "writing.creator.v1"},
			Tools:   append([]string(nil), readTools...),
			Budget:  budget(8, 12, 1, 3*time.Minute),
			Context: contextPolicy("recall", 64*1024, 16*1024),
			Memory:  appharness.MemoryPolicy{Recall: true, Remember: true},
		},
		{
			ID: WriterDefinitionID, Version: "1", Name: WriterDefinitionID,
			Description: "Polishes a prose draft without changing its factual commitments.", Tier: appharness.AgentTierWorker,
			Model: appharness.ModelPolicy{Hint: "worker"}, Prompt: appharness.PromptSpec{ID: "writing.writer.v1"},
			Tools:   []string{"canvas.get_nodes"},
			Budget:  budget(4, 4, 1, 2*time.Minute),
			Context: contextPolicy("none", 32*1024, 16*1024),
		},
	}
	for _, registration := range directCreatorRegistrations {
		definitions = append(definitions, directCreatorDefinition(
			registration.AgentID, registration.Name, registration.SkillID,
			registration.ArtifactKind,
		))
	}
	definitions = append(definitions,
		nodeUpdateDefinition(), sectionOutlineDefinition(), chapterSectionDefinition(), chapterArchiveDefinition(),
	)
	return definitions
}

func directCreatorDefinition(id, name, skillID, artifactKind string) appharness.AgentDefinition {
	return appharness.AgentDefinition{
		ID: id, Version: "1", Name: name,
		Description: "Completes one delegated writing task using the fixed " + skillID + " capability.",
		Tier:        appharness.AgentTierWorker, Model: appharness.ModelPolicy{Hint: "worker"},
		Prompt: appharness.PromptSpec{ID: "writing.direct_creator.v1"}, Tools: []string{"canvas.get_nodes", "canvas.search_context", "story_spine.context", "workspace.search"},
		Budget:  budget(8, 12, 1, 3*time.Minute),
		Context: contextPolicy("recall", 64*1024, 16*1024), Memory: appharness.MemoryPolicy{Recall: true, Remember: true},
	}
}

func nodeUpdateDefinition() appharness.AgentDefinition {
	return appharness.AgentDefinition{
		ID: NodeUpdateDefinitionID, Version: "1", Name: "writing_node_update",
		Description: "Updates one existing canvas node from a typed replacement artifact.", Tier: appharness.AgentTierWorker,
		Model: appharness.ModelPolicy{Hint: "worker"}, Prompt: appharness.PromptSpec{ID: "writing.node_update.v1"},
		Tools: []string{"canvas.get_nodes", "workspace.search", "story_spine.context"}, ControlTools: []string{"ask_user"},
		Budget: budget(8, 12, 1, 3*time.Minute), Context: contextPolicy("recall", 64*1024, 8*1024),
		Memory: appharness.MemoryPolicy{Recall: true, Remember: true},
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
		Budget: budget(8, 12, 1, 4*time.Minute), Context: contextPolicy("recall", 64*1024, 16*1024),
		Memory: appharness.MemoryPolicy{Recall: true, Remember: true},
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
	return registry, nil
}

func budget(modelCalls, toolCalls, sideEffectCalls int, duration time.Duration) appharness.BudgetPolicy {
	return appharness.BudgetPolicy{
		MaxModelCalls: modelCalls, MaxToolCalls: toolCalls, MaxSideEffectCalls: sideEffectCalls,
		MaxDuration: duration, MaxToolResultBytes: 64 * 1024,
	}
}

func contextPolicy(memory string, toolResultBytes, reservedOutputTokens int) appharness.ContextPolicy {
	return appharness.ContextPolicy{
		History: "session", Memory: memory, MaxToolResultBytes: toolResultBytes,
		ReservedOutputTokens: reservedOutputTokens,
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

func proposalSchema(creatableKinds []canvas.NodeKind, maxNodes int) map[string]any {
	node := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"clientId": map[string]any{"type": "string", "minLength": 1},
			"kind":     map[string]any{"type": "string", "enum": creatableKinds},
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
			"kind":     map[string]any{"type": "string", "const": "generated_from"},
		},
		"required": []string{"sourceId", "targetId", "kind"}, "additionalProperties": false,
	}
	return map[string]any{
		"type":                 "object",
		"required":             []string{"baseRevisions", "nodes", "updates", "edges", "reasons", "questions"},
		"additionalProperties": false,
		"properties": map[string]any{
			"baseRevisions": map[string]any{"type": "object", "maxProperties": 0},
			"nodes":         map[string]any{"type": "array", "items": node, "minItems": 1, "maxItems": maxNodes},
			"updates":       map[string]any{"type": "array", "items": update, "maxItems": 0},
			"edges":         map[string]any{"type": "array", "items": edge},
			"reasons":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"questions":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
}

func nonEmptyStringSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1}
}

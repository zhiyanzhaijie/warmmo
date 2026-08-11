package collaboration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"warmmo/core/internal/adapter/agent/adk"
	agentcore "warmmo/core/internal/adapter/agent/core"
	writing "warmmo/core/internal/adapter/agent/writing"
	appharness "warmmo/core/internal/application/harness"
)

// NonCollaborativeChain runs direct canvas workflows as one durable ADK
// turn. The model loop, tool policy, checkpoints and artifacts are shared with
// the collaborative chain; only the final product projection remains target
// specific in AgentService.
type NonCollaborativeChain struct {
	definitions *appharness.DefinitionRegistry
	runner      TurnRunner
	artifacts   appharness.ArtifactStore
	checkpoints appharness.CheckpointStore
	skills      writing.SkillCatalog
}

func NewNonCollaborativeChain(definitions *appharness.DefinitionRegistry, runner TurnRunner, artifacts appharness.ArtifactStore, checkpoints appharness.CheckpointStore, skills writing.SkillCatalog) (*NonCollaborativeChain, error) {
	if definitions == nil || runner == nil || artifacts == nil || checkpoints == nil || skills == nil {
		return nil, errors.New("non-collaborative chain dependencies are required")
	}
	for _, id := range []string{NodeUpdateDefinitionID, SectionOutlineDefinitionID, ChapterSectionDefinitionID, ChapterArchiveDefinitionID} {
		if _, err := definitions.Resolve(id); err != nil {
			return nil, err
		}
	}
	return &NonCollaborativeChain{definitions: definitions, runner: runner, artifacts: artifacts, checkpoints: checkpoints, skills: skills}, nil
}

func (c *NonCollaborativeChain) Run(ctx context.Context, input writing.RunInput, emit writing.Emitter) (writing.RunResult, error) {
	definitionID, artifactKind, err := definitionForTarget(input.Target)
	if err != nil {
		return writing.RunResult{}, err
	}
	skill, err := c.loadSkill(ctx, input.Target, input.Prompt)
	if err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventContextPreparing, map[string]any{"nodeCount": len(input.ContextNodes) + 1, "mode": "on-demand"}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventContextReady, map[string]any{"nodeCount": len(input.ContextNodes) + 1, "mode": "on-demand"}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventSkillSearching, map[string]any{"target": input.Target}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventSkillMatched, map[string]any{"matches": []writing.SkillMatch{{ID: skill.ID, Name: skill.Name, Version: skill.Version, Description: skill.Description}}}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventSkillLoaded, map[string]any{"skillId": skill.ID, "version": skill.Version}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventGenerationStarted, map[string]any{"nodeId": input.TargetNodeID, "mode": generationMode(input), "target": input.Target}); err != nil {
		return writing.RunResult{}, err
	}
	registered, err := c.definitions.Resolve(definitionID)
	if err != nil {
		return writing.RunResult{}, err
	}
	output, err := outputForKind(registered.Definition.Output, artifactKind)
	if err != nil {
		return writing.RunResult{}, err
	}
	allowedTools := intersectTools(registered.Definition.Tools, skill.AllowedTools)
	turnID := uuid.NewString()
	prompt, err := encodePrompt(nonCollaborativePrompt(input, skill))
	if err != nil {
		return writing.RunResult{}, err
	}
	outcome, err := c.runner.Run(ctx, adk.LLMTurnRequest{
		RunID: input.RunID, TurnID: turnID, AgentID: registered.Definition.ID,
		AgentName: registered.Definition.Name, Description: registered.Definition.Description,
		Instruction: nonCollaborativeInstruction(skill, artifactKind), DefinitionVersion: registered.Definition.Version,
		DefinitionHash: registered.Hash, ProviderID: input.ProviderID, ModelID: input.ModelID,
		UserID: "work:" + input.WorkID, SessionID: uuid.NewString(), Prompt: prompt,
		AllowedTools: allowedTools, ControlTools: registered.Definition.ControlTools,
		ToolInvocation: agentcore.ToolInvocation{RunID: input.RunID, TurnID: turnID, WorkID: input.WorkID, SkillID: skill.ID, SkillVersion: skill.Version},
		Budget:         registered.Definition.Budget, Context: registered.Definition.Context, Memory: registered.Definition.Memory,
		Output: output,
	}, func(event adk.LLMTurnEvent) error { return projectEvent(event, writing.RoleCreator, emit) })
	if err != nil {
		return writing.RunResult{}, err
	}
	return c.outcomeResult(ctx, input, skill, artifactKind, outcome, emit)
}

func (c *NonCollaborativeChain) Resume(ctx context.Context, input writing.RunInput, answer string, emit writing.Emitter) (writing.RunResult, error) {
	definitionID, artifactKind, err := definitionForTarget(input.Target)
	if err != nil {
		return writing.RunResult{}, err
	}
	pending, err := c.checkpoints.FindPendingCheckpoint(ctx, input.RunID)
	if err != nil {
		return writing.RunResult{}, err
	}
	if pending.AgentID != definitionID || pending.Snapshot == nil {
		return writing.RunResult{}, fmt.Errorf("pending checkpoint does not belong to agent %q", definitionID)
	}
	skill := skillFromSnapshot(*pending.Snapshot)
	if skill.ID == "" || skill.Version == "" {
		return writing.RunResult{}, errors.New("pending checkpoint has no frozen skill identity")
	}
	outcome, err := c.runner.Resume(ctx, input.RunID, answer, func(event adk.LLMTurnEvent) error { return projectEvent(event, writing.RoleCreator, emit) })
	if err != nil {
		return writing.RunResult{}, err
	}
	registered, err := c.definitions.Resolve(definitionID)
	if err != nil {
		return writing.RunResult{}, err
	}
	if outcome.Status == appharness.TurnAwaitingUser {
		return writing.RunResult{}, writing.ErrApprovalRequired
	}
	if outcome.Artifact == nil || outcome.Artifact.Kind != artifactKind || outcome.Artifact.ID == "" {
		return writing.RunResult{}, fmt.Errorf("agent %q resumed without artifact", registered.Definition.ID)
	}
	return c.outcomeResult(ctx, input, skill, artifactKind, outcome, emit)
}

func (c *NonCollaborativeChain) Recover(ctx context.Context, input writing.RunInput, emit writing.Emitter) (writing.RunResult, error) {
	definitionID, artifactKind, err := definitionForTarget(input.Target)
	if err != nil {
		return writing.RunResult{}, err
	}
	pending, err := c.checkpoints.FindPendingCheckpoint(ctx, input.RunID)
	if err == nil {
		if pending.Status != appharness.TurnAwaitingUser || pending.Pending == nil {
			return writing.RunResult{}, fmt.Errorf("unsupported recovery checkpoint status %q", pending.Status)
		}
		if pending.AgentID != definitionID {
			return writing.RunResult{}, fmt.Errorf("pending checkpoint belongs to agent %q, expected %q", pending.AgentID, definitionID)
		}
		var question struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
		}
		if decodeErr := json.Unmarshal(pending.Pending.Payload, &question); decodeErr != nil {
			return writing.RunResult{}, decodeErr
		}
		if strings.TrimSpace(question.Question) == "" {
			return writing.RunResult{}, errors.New("pending recovery question is empty")
		}
		if emitErr := emit(writing.EventApprovalRequired, map[string]any{"question": question.Question, "options": question.Options, "role": writing.RoleCreator, "toolName": pending.Pending.ToolName, "toolCallId": pending.Pending.ToolCallID}); emitErr != nil {
			return writing.RunResult{}, emitErr
		}
		return writing.RunResult{}, writing.ErrApprovalRequired
	} else if !errors.Is(err, appharness.ErrCheckpointNotFound) {
		return writing.RunResult{}, err
	}
	artifact, err := c.artifacts.FindArtifact(ctx, input.RunID, artifactKind)
	if err != nil {
		return writing.RunResult{}, fmt.Errorf("interrupted turn has no durable artifact: %w", err)
	}
	if artifact.AgentID != definitionID {
		return writing.RunResult{}, fmt.Errorf("artifact belongs to agent %q, expected %q", artifact.AgentID, definitionID)
	}
	skill := writing.Skill{}
	checkpoint, checkpointErr := c.checkpoints.GetCheckpoint(ctx, artifact.TurnID)
	if checkpointErr != nil && !errors.Is(checkpointErr, appharness.ErrCheckpointNotFound) {
		return writing.RunResult{}, checkpointErr
	}
	if checkpointErr == nil && checkpoint.Snapshot != nil {
		skill = skillFromSnapshot(*checkpoint.Snapshot)
	}
	if skill.ID == "" || skill.Version == "" {
		skill, err = c.loadSkill(ctx, input.Target, input.Prompt)
		if err != nil {
			return writing.RunResult{}, err
		}
	}
	return artifactResult(input, skill, artifactKind, artifact, emit)
}

func (c *NonCollaborativeChain) outcomeResult(ctx context.Context, input writing.RunInput, skill writing.Skill, artifactKind string, outcome adk.LLMTurnOutcome, emit writing.Emitter) (writing.RunResult, error) {
	if outcome.Status == appharness.TurnAwaitingUser {
		return writing.RunResult{}, writing.ErrApprovalRequired
	}
	if outcome.Status != appharness.TurnCompleted || outcome.Artifact == nil || outcome.Artifact.Kind != artifactKind {
		return writing.RunResult{}, fmt.Errorf("turn did not submit required artifact: status=%s reason=%s", outcome.Status, outcome.StopReason)
	}
	artifact, err := c.artifacts.GetArtifact(ctx, outcome.Artifact.ID)
	if err != nil {
		return writing.RunResult{}, err
	}
	return artifactResult(input, skill, artifactKind, artifact, emit)
}

func artifactResult(input writing.RunInput, skill writing.Skill, kind string, artifact appharness.Artifact, emit writing.Emitter) (writing.RunResult, error) {
	content := strings.TrimSpace(string(artifact.Payload))
	result := writing.RunResult{
		Content: content, Role: writing.RoleCreator, SkillID: skill.ID, SkillVersion: skill.Version,
		ExpectedRevision: input.TargetNodeRevision, ArtifactID: artifact.Ref.ID, ArtifactKind: artifact.Ref.Kind,
	}
	if kind == NodeUpdateArtifact {
		var update struct {
			Title   string `json:"title"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(artifact.Payload, &update); err != nil || strings.TrimSpace(update.Title) == "" || strings.TrimSpace(update.Content) == "" {
			return writing.RunResult{}, errors.New("node update artifact is invalid")
		}
		result.Title, result.Content = strings.TrimSpace(update.Title), strings.TrimSpace(update.Content)
	}
	if err := emit(writing.EventMessageDelta, map[string]string{"delta": result.Content}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventSkillCompleted, map[string]any{"skillId": skill.ID, "version": skill.Version}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventValidationCompleted, map[string]any{"valid": true, "artifact": artifact.Ref}); err != nil {
		return writing.RunResult{}, err
	}
	return result, nil
}

func (c *NonCollaborativeChain) loadSkill(ctx context.Context, target, prompt string) (writing.Skill, error) {
	matches, err := c.skills.Search(ctx, target, prompt)
	if err != nil {
		return writing.Skill{}, err
	}
	if len(matches) != 1 {
		return writing.Skill{}, fmt.Errorf("target %q must resolve to exactly one skill, got %d", target, len(matches))
	}
	return c.skills.Load(ctx, matches[0].ID)
}

func definitionForTarget(target string) (string, string, error) {
	switch {
	case writing.IsNodeUpdateTarget(target):
		return NodeUpdateDefinitionID, NodeUpdateArtifact, nil
	case target == writing.TargetSectionOutlineBatch:
		return SectionOutlineDefinitionID, SectionOutlineBatchArtifact, nil
	case target == writing.TargetChapterSection:
		return ChapterSectionDefinitionID, ChapterSectionArtifact, nil
	case target == writing.TargetChapterArchive:
		return ChapterArchiveDefinitionID, ChapterArchiveArtifact, nil
	default:
		return "", "", fmt.Errorf("non-collaborative target %q is not supported", target)
	}
}

func nonCollaborativePrompt(input writing.RunInput, skill writing.Skill) map[string]any {
	payload := map[string]any{
		"request": input.Prompt, "target": input.Target,
		"targetNode":            map[string]any{"id": input.TargetNodeID, "type": input.TargetNodeType, "revision": input.TargetNodeRevision},
		"availableContextNodes": input.ContextNodes, "contextNodeIds": input.ContextNodeIDs,
		"userResponses": input.UserResponses, "skill": map[string]any{"id": skill.ID, "version": skill.Version},
		"contextAccessPolicy": []string{
			"Node titles and content are not preloaded; targetNode and availableContextNodes contain metadata only.",
			"Select the smallest complete set of relevant node IDs and read them with canvas.get_nodes in batches of at most 64.",
			"Do not read nodes one at a time unless a completed read reveals a concrete need for more context.",
		},
	}
	if writing.IsNodeUpdateTarget(input.Target) {
		mode := nodeMutationMode(input.Prompt, input.TargetNodeType)
		payload["operation"] = "update_existing_node"
		payload["mutationMode"] = mode
		if mode == "replace" {
			payload["updatePolicy"] = []string{"Replace the target's previous semantic content with the requested new concept.", "Do not reuse the previous identity merely because it occupies the target node.", "Return a complete title and content replacement."}
		} else {
			payload["updatePolicy"] = []string{"Read the target node as the authoritative baseline.", "Preserve established details not explicitly changed by the request.", "Return the complete merged title and content."}
		}
		return payload
	}
	payload["operation"] = "derive_child_nodes"
	if input.Target == writing.TargetChapterArchive {
		payload["operation"] = "archive_chapter_and_propose_entity_versions"
	}
	return payload
}

func nonCollaborativeInstruction(skill writing.Skill, artifactKind string) string {
	return strings.TrimSpace(skill.Instructions) + "\n\nYou are a Warmmo canvas workflow agent. Read the minimum authoritative context with tools and preserve supplied facts. Any instruction above to return JSON describes the artifact payload, not a direct model response. You must call submit_artifact exactly once with kind " + artifactKind + ". Do not return the artifact as final text."
}

func skillFromSnapshot(snapshot appharness.TurnSnapshot) writing.Skill {
	return writing.Skill{ID: snapshot.SkillID, Version: snapshot.SkillVersion}
}

func generationMode(input writing.RunInput) string {
	if writing.IsNodeUpdateTarget(input.Target) {
		return nodeMutationMode(input.Prompt, input.TargetNodeType)
	}
	if input.Target == writing.TargetChapterArchive {
		return "archive"
	}
	return "derive"
}

func nodeMutationMode(prompt, nodeKind string) string {
	normalized := strings.ToLower(strings.TrimSpace(prompt))
	for _, phrase := range []string{"替换这个节点", "替换当前节点", "重做这个节点", "重写这个节点", "清空重写", "完全重写", "replace this node", "rewrite this node", "start this node over"} {
		if strings.Contains(normalized, phrase) {
			return "replace"
		}
	}
	phrasesByKind := map[string][]string{
		"character":       {"新角色", "新人物", "另一个角色", "另一个人物", "创建角色", "创建一个角色", "new character", "another character", "create a character"},
		"world":           {"新世界观", "另一个世界观", "创建世界观", "new world", "create a world"},
		"location":        {"新地点", "新场景", "另一个地点", "创建地点", "new location", "create a location"},
		"event":           {"新事件", "另一个事件", "创建事件", "new event", "create an event"},
		"mechanism":       {"新机制", "另一个机制", "创建机制", "new mechanism", "create a mechanism"},
		"chapter-outline": {"新章节", "新的章节", "下一章", "下一个章节", "续写新章", "续写下一章", "新大纲", "新的章节概览", "创建章节", "创建大纲", "new chapter", "next chapter", "new outline"},
	}
	for _, phrase := range phrasesByKind[nodeKind] {
		if strings.Contains(normalized, phrase) {
			return "replace"
		}
	}
	return "merge"
}

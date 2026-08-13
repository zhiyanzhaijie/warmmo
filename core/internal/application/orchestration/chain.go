package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	appharness "warmmo/core/internal/application/harness"
	"warmmo/core/internal/application/writing"
	"warmmo/core/internal/domain/canvas"
)

type WritingCollaborationChain struct {
	definitions *appharness.DefinitionRegistry
	runner      appharness.AgentRuntime
	artifacts   appharness.ArtifactStore
	checkpoints appharness.CheckpointStore
	skills      writing.SkillCatalog
}

func NewWritingCollaborationChain(
	definitions *appharness.DefinitionRegistry,
	runner appharness.AgentRuntime,
	artifacts appharness.ArtifactStore,
	checkpoints appharness.CheckpointStore,
	skills writing.SkillCatalog,
) (*WritingCollaborationChain, error) {
	if definitions == nil || runner == nil || artifacts == nil || checkpoints == nil || skills == nil {
		return nil, errors.New("writing collaboration chain dependencies are required")
	}
	for _, id := range []string{
		PlannerDefinitionID, CreatorDefinitionID, WriterDefinitionID,
		EntityCreatorDefinitionID, ChapterCreatorDefinitionID, ProseCreatorDefinitionID, BrainstormDefinitionID,
	} {
		if _, err := definitions.Resolve(id); err != nil {
			return nil, err
		}
	}
	return &WritingCollaborationChain{definitions: definitions, runner: runner, artifacts: artifacts, checkpoints: checkpoints, skills: skills}, nil
}

func (c *WritingCollaborationChain) RecoverArtifact(
	ctx context.Context,
	ref appharness.ArtifactRef,
	emit writing.Emitter,
) (writing.RunResult, error) {
	artifact, err := c.artifacts.GetArtifact(ctx, ref.ID)
	if err != nil {
		return writing.RunResult{}, err
	}
	if artifact.Ref.Kind != ref.Kind || artifact.Ref.SchemaVersion != ref.SchemaVersion {
		return writing.RunResult{}, errors.New("delegated artifact does not match the root checkpoint handoff")
	}
	checkpoint, err := c.checkpoints.GetCheckpoint(ctx, artifact.TurnID)
	if err != nil {
		return writing.RunResult{}, fmt.Errorf("load delegated artifact turn: %w", err)
	}
	if checkpoint.Snapshot == nil || strings.TrimSpace(checkpoint.Snapshot.SkillID) == "" {
		return writing.RunResult{}, errors.New("delegated artifact turn has no skill snapshot")
	}
	skill, err := c.skills.Load(ctx, checkpoint.Snapshot.SkillID)
	if err != nil {
		return writing.RunResult{}, err
	}
	if _, _, directErr := directCreatorSpec(artifact.AgentID); directErr == nil {
		skillID, _, specErr := directCreatorSpec(artifact.AgentID)
		if specErr != nil {
			return writing.RunResult{}, specErr
		}
		skill, skillErr := c.skills.Load(ctx, skillID)
		if skillErr != nil {
			return writing.RunResult{}, skillErr
		}
		return c.finishDirectCreator(artifact, skill, emit)
	}
	switch artifact.AgentID {
	case CreatorDefinitionID:
		return c.finishDirectCreator(artifact, skill, emit)
	case WriterDefinitionID:
		if err := emit(writing.EventRoleCompleted, map[string]any{"role": writing.RoleWriter, "artifact": artifact.Ref}); err != nil {
			return writing.RunResult{}, err
		}
		return resultFromArtifact(artifact, skill, writing.RoleWriter, emit)
	default:
		return writing.RunResult{}, fmt.Errorf("root handoff references unsupported agent %q", artifact.AgentID)
	}
}

func (c *WritingCollaborationChain) RunDelegated(
	ctx context.Context,
	input writing.RunInput,
	parentTurnID string,
	task string,
	emit writing.Emitter,
) (writing.RunResult, error) {
	if !writing.IsCollaborativeTarget(input.Target) {
		return writing.RunResult{}, fmt.Errorf("writing collaboration chain does not support target %q", input.Target)
	}
	plannerSkill, err := c.skills.Load(ctx, "creative-planner")
	if err != nil {
		return writing.RunResult{}, fmt.Errorf("load planner skill: %w", err)
	}
	if err := emit(writing.EventRoleStarted, map[string]any{"role": writing.RolePlanner, "skillId": plannerSkill.ID}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventPlanStarted, map[string]any{"role": writing.RolePlanner}); err != nil {
		return writing.RunResult{}, err
	}
	plannerPrompt, err := encodePrompt(map[string]any{
		"request": strings.TrimSpace(task), "originalRequest": input.Prompt,
		"runTarget": input.Target, "context": contextEnvelope(input),
	})
	if err != nil {
		return writing.RunResult{}, err
	}
	plannerArtifact, err := c.runTurn(ctx, input, PlannerDefinitionID, plannerSkill, CollaborationPlanArtifact,
		plannerInstruction(plannerSkill), plannerPrompt, writing.RolePlanner, false,
		parentAgentID(parentTurnID), parentTurnID, emit)
	if err != nil {
		return writing.RunResult{}, err
	}
	plan, err := writing.ParseCollaborationPlan(string(plannerArtifact.Payload), input.Target)
	if err != nil {
		return writing.RunResult{}, fmt.Errorf("validate planner artifact: %w", err)
	}
	return c.continueFromPlan(ctx, input, plannerArtifact, plan, emit)
}

func (c *WritingCollaborationChain) RunCreator(
	ctx context.Context,
	input writing.RunInput,
	parentTurnID string,
	task string,
	agentID string,
	emit writing.Emitter,
) (writing.RunResult, error) {
	skillID, artifactKind, err := directCreatorSpec(agentID)
	if err != nil {
		return writing.RunResult{}, err
	}
	creatorSkill, err := c.skills.Load(ctx, skillID)
	if err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventRoleStarted, map[string]any{"role": writing.RoleCreator, "skillId": creatorSkill.ID}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventGenerationStarted, map[string]any{"mode": "direct-delegation", "role": writing.RoleCreator}); err != nil {
		return writing.RunResult{}, err
	}
	prompt, err := encodePrompt(map[string]any{
		"request": task, "originalRequest": input.Prompt, "context": contextEnvelope(input),
		"expectedArtifactKind": artifactKind,
	})
	if err != nil {
		return writing.RunResult{}, err
	}
	artifact, err := c.runTurn(ctx, input, agentID, creatorSkill, artifactKind,
		directCreatorInstruction(creatorSkill, artifactKind), prompt, writing.RoleCreator, false,
		CanvasOrchestratorDefinitionID, parentTurnID, emit)
	if err != nil {
		return writing.RunResult{}, err
	}
	return c.finishDirectCreator(artifact, creatorSkill, emit)
}

func (c *WritingCollaborationChain) finishDirectCreator(
	artifact appharness.Artifact,
	skill writing.Skill,
	emit writing.Emitter,
) (writing.RunResult, error) {
	if artifact.Ref.Kind == ProposalArtifact {
		if err := writing.ValidateProposalSetForSkill(string(artifact.Payload), skill.ID); err != nil {
			return writing.RunResult{}, fmt.Errorf("validate creator artifact: %w", err)
		}
	}
	if err := emit(writing.EventRoleCompleted, map[string]any{"role": writing.RoleCreator, "artifact": artifact.Ref}); err != nil {
		return writing.RunResult{}, err
	}
	return resultFromArtifact(artifact, skill, writing.RoleCreator, emit)
}

func (c *WritingCollaborationChain) continueFromPlan(
	ctx context.Context,
	input writing.RunInput,
	plannerArtifact appharness.Artifact,
	plan writing.CollaborationPlan,
	emit writing.Emitter,
) (writing.RunResult, error) {
	if err := emit(writing.EventPlanCompleted, map[string]any{"plan": plan, "artifact": plannerArtifact.Ref, "role": writing.RolePlanner}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventRoleCompleted, map[string]any{"role": writing.RolePlanner, "artifact": plannerArtifact.Ref}); err != nil {
		return writing.RunResult{}, err
	}
	creatorSkill, err := c.loadCreatorSkill(ctx, plan)
	if err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventRoleHandoff, map[string]any{"from": writing.RolePlanner, "to": writing.RoleCreator, "artifact": plannerArtifact.Ref, "skillId": creatorSkill.ID}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventRoleStarted, map[string]any{"role": writing.RoleCreator, "skillId": creatorSkill.ID}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventGenerationStarted, map[string]any{"mode": "collaboration-chain", "role": writing.RoleCreator}); err != nil {
		return writing.RunResult{}, err
	}
	creatorKind := artifactKindForPlan(plan)
	creatorPrompt, err := encodePrompt(map[string]any{
		"request": input.Prompt, "context": contextEnvelope(input),
		"planArtifact":         map[string]any{"ref": plannerArtifact.Ref, "payload": json.RawMessage(plannerArtifact.Payload)},
		"expectedArtifactKind": creatorKind,
	})
	if err != nil {
		return writing.RunResult{}, err
	}
	creatorArtifact, err := c.runTurn(ctx, input, CreatorDefinitionID, creatorSkill, creatorKind,
		creatorInstruction(creatorSkill, creatorKind), creatorPrompt, writing.RoleCreator, false,
		PlannerDefinitionID, plannerArtifact.TurnID, emit)
	if err != nil {
		return writing.RunResult{}, err
	}
	return c.continueFromCreator(ctx, input, plannerArtifact, plan, creatorArtifact, creatorSkill, emit)
}

func (c *WritingCollaborationChain) continueFromCreator(
	ctx context.Context,
	input writing.RunInput,
	plannerArtifact appharness.Artifact,
	plan writing.CollaborationPlan,
	creatorArtifact appharness.Artifact,
	creatorSkill writing.Skill,
	emit writing.Emitter,
) (writing.RunResult, error) {
	if creatorArtifact.Ref.Kind == ProposalArtifact {
		if err := writing.ValidateProposalSetForSkill(string(creatorArtifact.Payload), creatorSkill.ID); err != nil {
			return writing.RunResult{}, fmt.Errorf("validate creator artifact: %w", err)
		}
	}
	if err := emit(writing.EventRoleCompleted, map[string]any{"role": writing.RoleCreator, "artifact": creatorArtifact.Ref}); err != nil {
		return writing.RunResult{}, err
	}
	if !plan.WriterRequired {
		return resultFromArtifact(creatorArtifact, creatorSkill, writing.RoleCreator, emit)
	}
	writerSkill, err := c.skills.Load(ctx, "prose-writer")
	if err != nil {
		return writing.RunResult{}, fmt.Errorf("load writer skill: %w", err)
	}
	if err := emit(writing.EventRoleHandoff, map[string]any{"from": writing.RoleCreator, "to": writing.RoleWriter, "artifact": creatorArtifact.Ref, "skillId": writerSkill.ID}); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventRoleStarted, map[string]any{"role": writing.RoleWriter, "skillId": writerSkill.ID}); err != nil {
		return writing.RunResult{}, err
	}
	writerPrompt, err := encodePrompt(map[string]any{
		"request": input.Prompt, "writerInstruction": plan.WriterInstruction,
		"planArtifactRef": plannerArtifact.Ref,
		"draftArtifact":   map[string]any{"ref": creatorArtifact.Ref, "payload": json.RawMessage(creatorArtifact.Payload)},
	})
	if err != nil {
		return writing.RunResult{}, err
	}
	writerArtifact, err := c.runTurn(ctx, input, WriterDefinitionID, writerSkill, PolishedProseArtifact,
		writerInstruction(writerSkill), writerPrompt, writing.RoleWriter, false,
		PlannerDefinitionID, plannerArtifact.TurnID, emit)
	if err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventRoleCompleted, map[string]any{"role": writing.RoleWriter, "artifact": writerArtifact.Ref}); err != nil {
		return writing.RunResult{}, err
	}
	return resultFromArtifact(writerArtifact, writerSkill, writing.RoleWriter, emit)
}

func (c *WritingCollaborationChain) loadPlan(ctx context.Context, input writing.RunInput) (appharness.Artifact, writing.CollaborationPlan, error) {
	artifact, err := c.artifacts.FindArtifact(ctx, input.RunID, CollaborationPlanArtifact)
	if err != nil {
		return appharness.Artifact{}, writing.CollaborationPlan{}, fmt.Errorf("load collaboration plan artifact: %w", err)
	}
	plan, err := writing.ParseCollaborationPlan(string(artifact.Payload), input.Target)
	if err != nil {
		return appharness.Artifact{}, writing.CollaborationPlan{}, err
	}
	return artifact, plan, nil
}

func (c *WritingCollaborationChain) runTurn(
	ctx context.Context,
	input writing.RunInput,
	definitionID string,
	skill writing.Skill,
	expectedKind string,
	instruction string,
	prompt string,
	role writing.AgentRole,
	publishConversation bool,
	_ string,
	_ string,
	emit writing.Emitter,
) (appharness.Artifact, error) {
	registered, err := c.definitions.Resolve(definitionID)
	if err != nil {
		return appharness.Artifact{}, err
	}
	responseSchema, err := schemaForSkillOutput(skill.ID, expectedKind)
	if err != nil {
		return appharness.Artifact{}, err
	}
	allowedTools := intersectTools(registered.Definition.Tools, skill.AllowedTools)
	turnID := uuid.NewString()
	outcome, err := c.runner.Run(ctx, appharness.RuntimeRequest{
		RunID: input.RunID, TurnID: turnID, AgentID: registered.Definition.ID,
		AgentName: registered.Definition.Name, Description: registered.Definition.Description,
		Instruction: instruction, DefinitionVersion: registered.Definition.Version, DefinitionHash: registered.Hash,
		ProviderID: input.ProviderID, ModelID: input.ModelID,
		UserID: "work:" + input.WorkID, SessionID: uuid.NewString(), ConversationSessionID: input.ConversationSessionID, Prompt: prompt,
		ConversationUserContent: input.Prompt, PublishConversation: publishConversation,
		AllowedTools: allowedTools,
		ToolInvocation: appharness.ToolInvocation{
			RunID: input.RunID, TurnID: turnID, WorkID: input.WorkID, SkillID: skill.ID, SkillVersion: skill.Version,
		},
		Budget: registered.Definition.Budget, Context: registered.Definition.Context,
		Memory: registered.Definition.Memory, ResponseSchema: responseSchema,
	}, func(event appharness.RuntimeEvent) error {
		return projectEvent(event, role, emit)
	})
	if err != nil {
		return appharness.Artifact{}, err
	}
	if outcome.Status == appharness.TurnAwaitingUser {
		return appharness.Artifact{}, writing.ErrApprovalRequired
	}
	if outcome.Status != appharness.TurnCompleted || len(outcome.Output) == 0 {
		return appharness.Artifact{}, fmt.Errorf("agent %q completed without required structured output: status=%s reason=%s", definitionID, outcome.Status, outcome.StopReason)
	}
	artifact, err := persistWritingOutput(ctx, c.artifacts, input.RunID, outcome.TurnID, definitionID, expectedKind, outcome.Output)
	if err != nil {
		return appharness.Artifact{}, fmt.Errorf("persist writing output: %w", err)
	}
	return artifact, nil
}

func persistWritingOutput(
	ctx context.Context,
	store appharness.ArtifactStore,
	runID string,
	turnID string,
	agentID string,
	kind string,
	output json.RawMessage,
) (appharness.Artifact, error) {
	if store == nil {
		return appharness.Artifact{}, errors.New("writing output store is not configured")
	}
	if strings.TrimSpace(turnID) == "" || len(output) == 0 {
		return appharness.Artifact{}, errors.New("writing output identity and payload are required")
	}
	return store.SaveArtifact(ctx, appharness.Artifact{
		Ref:   appharness.ArtifactRef{ID: turnID, Kind: kind, SchemaVersion: "1"},
		RunID: runID, TurnID: turnID, AgentID: agentID,
		Payload: append(json.RawMessage(nil), output...),
	})
}

func roleForAgentName(name string) writing.AgentRole {
	switch name {
	case "writing_planner":
		return writing.RolePlanner
	case "writing_writer":
		return writing.RoleWriter
	default:
		return writing.RoleCreator
	}
}

func (c *WritingCollaborationChain) loadCreatorSkill(ctx context.Context, plan writing.CollaborationPlan) (writing.Skill, error) {
	matches, err := c.skills.Search(ctx, plan.CreatorTarget, plan.Brief)
	if err != nil {
		return writing.Skill{}, fmt.Errorf("search creator skills: %w", err)
	}
	found := false
	for _, match := range matches {
		if match.ID == plan.CreatorSkillID {
			found = true
			break
		}
	}
	if !found {
		return writing.Skill{}, fmt.Errorf("creator skill %q does not support target %q", plan.CreatorSkillID, plan.CreatorTarget)
	}
	skill, err := c.skills.Load(ctx, plan.CreatorSkillID)
	if err != nil {
		return writing.Skill{}, fmt.Errorf("load creator skill: %w", err)
	}
	return skill, nil
}

func contextEnvelope(input writing.RunInput) map[string]any {
	return map[string]any{
		"workId": input.WorkID, "availableContextNodes": input.ContextNodes,
		"priorityContextNodeIds":  input.ContextNodeIDs,
		"collaborativeCandidates": input.CollaborativeCandidates,
		"userResponses":           input.UserResponses,
	}
}

func encodePrompt(payload any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode collaboration turn prompt: %w", err)
	}
	return string(encoded), nil
}

func plannerInstruction(skill writing.Skill) string {
	return `You are Warmmo's Planner. Build one concrete collaboration plan from the request and evidence.
Use read tools only when evidence is needed. Do not write final fiction or mutate canvas content.
	Return only the complete CollaborationPlan matching the required response schema. Do not add a kind or wrapper.
	The plan must route exploration to story-brainstorm/advice and prose to prose-creator.` +
		"\n\n# Active Skill\n" + skill.Instructions
}

func creatorInstruction(skill writing.Skill, artifactKind string) string {
	return `You are Warmmo's Creator. Consume the approved plan artifact and create exactly its requested deliverable.
Do not alter the plan or perform the Writer role. Read the minimum relevant context with the allowed tools.
	Return only the complete result matching the required response schema. Do not add a kind or wrapper.` + "\n\n# Active Skill\n" + skill.Instructions
}

func directCreatorInstruction(skill writing.Skill, artifactKind string) string {
	return `You are Warmmo's Creator. Complete all deliverables in the delegated task exactly as requested, in one typed artifact.
The artifact is the complete proposal for this request: include every requested node now. Do not defer deliverables until after candidate review.
Do not create a plan or perform another agent's role. Read only the minimum relevant canvas evidence.
	Return only the complete result matching the required response schema. Do not add a kind or wrapper.` + "\n\n# Active Skill\n" + skill.Instructions
}

func directCreatorSpec(agentID string) (skillID string, artifactKind string, err error) {
	for _, registration := range directCreatorRegistrations {
		if registration.AgentID == agentID {
			return registration.SkillID, registration.ArtifactKind, nil
		}
	}
	return "", "", fmt.Errorf("agent %q is not a direct creator", agentID)
}

func directCreatorAgentIDs() []string {
	result := make([]string, 0, len(directCreatorRegistrations))
	for _, registration := range directCreatorRegistrations {
		result = append(result, registration.AgentID)
	}
	return result
}

func parentAgentID(parentTurnID string) string {
	if strings.TrimSpace(parentTurnID) == "" {
		return ""
	}
	return CanvasOrchestratorDefinitionID
}

func writerInstruction(skill writing.Skill) string {
	return `You are Warmmo's Writer. Polish only the supplied draft while preserving facts, viewpoint, chronology, names and outcomes.
	Return only the complete polished prose matching the required response schema.` + "\n\n# Active Skill\n" + skill.Instructions
}

func schemaForKind(kind string) (map[string]any, error) {
	switch kind {
	case CollaborationPlanArtifact:
		return collaborationPlanSchema(), nil
	case ProposalArtifact:
		return nil, errors.New("proposal output requires a creator capability")
	case AdviceArtifact, DraftArtifact, PolishedProseArtifact:
		return nonEmptyStringSchema(), nil
	case NodeUpdateArtifact, ChapterSectionArtifact:
		return nodeUpdateSchema(), nil
	case SectionOutlineBatchArtifact:
		return sectionOutlineBatchSchema(), nil
	case ChapterArchiveArtifact:
		return chapterArchiveSchema(), nil
	default:
		return nil, fmt.Errorf("unknown writing output kind %q", kind)
	}
}

func schemaForSkillOutput(skillID, kind string) (map[string]any, error) {
	if kind != ProposalArtifact {
		return schemaForKind(kind)
	}
	switch skillID {
	case "entity-creator":
		return proposalSchema(canvas.EntityNodeKinds(), writing.MaxProposalNodes), nil
	case "chapter-creator":
		return proposalSchema([]canvas.NodeKind{canvas.NodeKindChapterOutline}, 1), nil
	default:
		return nil, fmt.Errorf("skill %q cannot produce proposal output", skillID)
	}
}

func intersectTools(definitionTools, skillTools []string) []string {
	allowed := make(map[string]struct{}, len(skillTools))
	for _, name := range skillTools {
		allowed[name] = struct{}{}
	}
	result := make([]string, 0, len(definitionTools))
	for _, name := range definitionTools {
		if _, ok := allowed[name]; ok {
			result = append(result, name)
		}
	}
	return result
}

func artifactKindForPlan(plan writing.CollaborationPlan) string {
	switch plan.OutputKind {
	case "proposal":
		return ProposalArtifact
	case "advice":
		return AdviceArtifact
	default:
		return DraftArtifact
	}
}

func resultFromArtifact(artifact appharness.Artifact, skill writing.Skill, role writing.AgentRole, emit writing.Emitter) (writing.RunResult, error) {
	content := strings.TrimSpace(string(artifact.Payload))
	if artifact.Ref.Kind != ProposalArtifact {
		if err := json.Unmarshal(artifact.Payload, &content); err != nil {
			return writing.RunResult{}, fmt.Errorf("decode text artifact: %w", err)
		}
		content = strings.TrimSpace(content)
		if content == "" {
			return writing.RunResult{}, errors.New("text artifact is empty")
		}
		if err := emit(writing.EventMessageDelta, map[string]any{"delta": content, "replace": true}); err != nil {
			return writing.RunResult{}, err
		}
	}
	return writing.RunResult{
		Content: content, Role: role, SkillID: skill.ID, SkillVersion: skill.Version,
		ArtifactID: artifact.Ref.ID, ArtifactKind: artifact.Ref.Kind,
	}, nil
}

func projectEvent(event appharness.RuntimeEvent, role writing.AgentRole, emit writing.Emitter) error {
	data := map[string]any{
		"role": role, "agent": event.AgentName, "eventId": event.EventID,
		"toolName": event.ToolName, "toolCallId": event.ToolCallID,
		"summary": event.Summary, "errorCode": event.ErrorCode, "retryable": event.Retryable,
		"resultBytes": event.ResultBytes, "truncated": event.Truncated,
	}
	switch event.Type {
	case appharness.RuntimeEventMessageDelta:
		return nil
	case appharness.RuntimeEventReasoningStarted:
		return emit(writing.EventReasoningStarted, map[string]any{"role": role})
	case appharness.RuntimeEventReasoningDelta:
		return emit(writing.EventReasoningDelta, map[string]any{"delta": event.Text, "role": role})
	case appharness.RuntimeEventReasoningCompleted:
		return emit(writing.EventReasoningCompleted, map[string]any{"role": role})
	case appharness.RuntimeEventToolRequested:
		return emit(writing.EventToolRequested, data)
	case appharness.RuntimeEventToolStarted:
		return emit(writing.EventToolStarted, data)
	case appharness.RuntimeEventToolCompleted:
		return emit(writing.EventToolCompleted, data)
	case appharness.RuntimeEventToolFailed:
		return emit(writing.EventToolFailed, data)
	case appharness.RuntimeEventPaused:
		var pending struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
		}
		_ = json.Unmarshal(event.Payload, &pending)
		if strings.TrimSpace(pending.Question) == "" {
			pending.Question = event.Summary
		}
		return emit(writing.EventApprovalRequired, map[string]any{
			"question": pending.Question, "options": pending.Options, "role": role,
			"toolName": event.ToolName, "toolCallId": event.ToolCallID,
		})
	default:
		return nil
	}
}

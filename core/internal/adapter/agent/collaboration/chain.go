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

type WritingCollaborationChain struct {
	definitions *appharness.DefinitionRegistry
	runner      TurnRunner
	artifacts   appharness.ArtifactStore
	checkpoints appharness.CheckpointStore
	skills      writing.SkillCatalog
}

type TurnRunner interface {
	Run(context.Context, adk.LLMTurnRequest, adk.LLMTurnEmitter) (adk.LLMTurnOutcome, error)
	Resume(context.Context, string, string, adk.LLMTurnEmitter) (adk.LLMTurnOutcome, error)
}

func NewWritingCollaborationChain(
	definitions *appharness.DefinitionRegistry,
	runner TurnRunner,
	artifacts appharness.ArtifactStore,
	checkpoints appharness.CheckpointStore,
	skills writing.SkillCatalog,
) (*WritingCollaborationChain, error) {
	if definitions == nil || runner == nil || artifacts == nil || checkpoints == nil || skills == nil {
		return nil, errors.New("writing collaboration chain dependencies are required")
	}
	for _, id := range []string{PlannerDefinitionID, CreatorDefinitionID, WriterDefinitionID} {
		if _, err := definitions.Resolve(id); err != nil {
			return nil, err
		}
	}
	return &WritingCollaborationChain{definitions: definitions, runner: runner, artifacts: artifacts, checkpoints: checkpoints, skills: skills}, nil
}

func (c *WritingCollaborationChain) Recover(
	ctx context.Context,
	input writing.RunInput,
	emit writing.Emitter,
) (writing.RunResult, error) {
	// A typed artifact or pending checkpoint is the only safe handoff. An
	// interrupted model call without either is deliberately not replayed.
	pending, err := c.checkpoints.FindPendingCheckpoint(ctx, input.RunID)
	if err == nil {
		if pending.Status != appharness.TurnAwaitingUser || pending.Pending == nil {
			return writing.RunResult{}, fmt.Errorf("run %s has an unsupported pending checkpoint status %q", input.RunID, pending.Status)
		}
		if pending.Snapshot == nil {
			return writing.RunResult{}, errors.New("pending recovery checkpoint has no turn snapshot")
		}
		var question struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
		}
		if decodeErr := json.Unmarshal(pending.Pending.Payload, &question); decodeErr != nil {
			return writing.RunResult{}, fmt.Errorf("decode pending recovery question: %w", decodeErr)
		}
		if strings.TrimSpace(question.Question) == "" {
			return writing.RunResult{}, errors.New("pending recovery question is empty")
		}
		if err := emit(writing.EventApprovalRequired, map[string]any{
			"question": question.Question, "options": question.Options,
			"role":     roleForAgentName(pending.Snapshot.AgentName),
			"toolName": pending.Pending.ToolName, "toolCallId": pending.Pending.ToolCallID,
		}); err != nil {
			return writing.RunResult{}, err
		}
		return writing.RunResult{}, writing.ErrApprovalRequired
	} else if !errors.Is(err, appharness.ErrCheckpointNotFound) {
		return writing.RunResult{}, fmt.Errorf("inspect recovery checkpoint: %w", err)
	}

	plannerArtifact, err := c.findArtifact(ctx, input.RunID, PlannerDefinitionID, CollaborationPlanArtifact)
	if err != nil {
		return writing.RunResult{}, fmt.Errorf("recover planner handoff: %w", err)
	}
	plan, err := writing.ParseCollaborationPlan(string(plannerArtifact.Payload), input.Target)
	if err != nil {
		return writing.RunResult{}, fmt.Errorf("validate recovered planner artifact: %w", err)
	}
	creatorKind := artifactKindForPlan(plan)
	creatorArtifact, creatorFound, err := c.findLatestCreatorArtifact(ctx, input.RunID, creatorKind)
	if err != nil {
		return writing.RunResult{}, err
	}
	if !creatorFound || !creatorArtifact.CreatedAt.After(plannerArtifact.CreatedAt) {
		return c.continueFromPlan(ctx, input, plannerArtifact, plan, emit)
	}
	creatorSkill, err := c.loadCreatorSkill(ctx, plan)
	if err != nil {
		return writing.RunResult{}, err
	}
	if !plan.WriterRequired {
		return c.continueFromCreator(ctx, input, plannerArtifact, plan, creatorArtifact, creatorSkill, emit)
	}
	writerArtifact, writerFound, err := c.findOptionalArtifact(ctx, input.RunID, WriterDefinitionID, PolishedProseArtifact)
	if err != nil {
		return writing.RunResult{}, err
	}
	if writerFound && writerArtifact.CreatedAt.After(creatorArtifact.CreatedAt) {
		writerSkill, skillErr := c.skills.Load(ctx, "prose-writer")
		if skillErr != nil {
			return writing.RunResult{}, skillErr
		}
		return resultFromArtifact(writerArtifact, writerSkill, writing.RoleWriter, emit)
	}
	return c.continueFromCreator(ctx, input, plannerArtifact, plan, creatorArtifact, creatorSkill, emit)
}

func (c *WritingCollaborationChain) findArtifact(ctx context.Context, runID, agentID, kind string) (appharness.Artifact, error) {
	artifact, err := c.artifacts.FindArtifact(ctx, runID, kind)
	if err != nil {
		return appharness.Artifact{}, err
	}
	if artifact.AgentID != agentID {
		return appharness.Artifact{}, fmt.Errorf("artifact %s belongs to agent %q, expected %q", artifact.Ref.ID, artifact.AgentID, agentID)
	}
	return artifact, nil
}

func (c *WritingCollaborationChain) findLatestCreatorArtifact(ctx context.Context, runID, kind string) (appharness.Artifact, bool, error) {
	return c.findOptionalArtifact(ctx, runID, CreatorDefinitionID, kind)
}

func (c *WritingCollaborationChain) findOptionalArtifact(ctx context.Context, runID, agentID, kind string) (appharness.Artifact, bool, error) {
	artifact, err := c.artifacts.FindArtifact(ctx, runID, kind)
	if errors.Is(err, appharness.ErrArtifactNotFound) {
		return appharness.Artifact{}, false, nil
	}
	if err != nil {
		return appharness.Artifact{}, false, err
	}
	if artifact.AgentID != agentID {
		return appharness.Artifact{}, false, fmt.Errorf("artifact %s belongs to agent %q, expected %q", artifact.Ref.ID, artifact.AgentID, agentID)
	}
	return artifact, true, nil
}

func (c *WritingCollaborationChain) Run(
	ctx context.Context,
	input writing.RunInput,
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
		"request": input.Prompt, "runTarget": input.Target, "context": contextEnvelope(input),
	})
	if err != nil {
		return writing.RunResult{}, err
	}
	plannerArtifact, err := c.runTurn(ctx, input, PlannerDefinitionID, plannerSkill, CollaborationPlanArtifact,
		plannerInstruction(plannerSkill), plannerPrompt, writing.RolePlanner, "", "", emit)
	if err != nil {
		return writing.RunResult{}, err
	}
	plan, err := writing.ParseCollaborationPlan(string(plannerArtifact.Payload), input.Target)
	if err != nil {
		return writing.RunResult{}, fmt.Errorf("validate planner artifact: %w", err)
	}
	return c.continueFromPlan(ctx, input, plannerArtifact, plan, emit)
}

func (c *WritingCollaborationChain) Resume(
	ctx context.Context,
	input writing.RunInput,
	answer string,
	emit writing.Emitter,
) (writing.RunResult, error) {
	outcome, err := c.runner.Resume(ctx, input.RunID, answer, func(event adk.LLMTurnEvent) error {
		return projectEvent(event, roleForAgentName(event.AgentName), emit)
	})
	if err != nil {
		return writing.RunResult{}, err
	}
	if outcome.Status == appharness.TurnAwaitingUser {
		return writing.RunResult{}, writing.ErrApprovalRequired
	}
	if outcome.Status != appharness.TurnCompleted || outcome.Artifact == nil {
		return writing.RunResult{}, fmt.Errorf("resumed collaboration turn did not submit an artifact: status=%s reason=%s", outcome.Status, outcome.StopReason)
	}
	artifact, err := c.artifacts.GetArtifact(ctx, outcome.Artifact.ID)
	if err != nil {
		return writing.RunResult{}, err
	}
	switch artifact.AgentID {
	case PlannerDefinitionID:
		plan, err := writing.ParseCollaborationPlan(string(artifact.Payload), input.Target)
		if err != nil {
			return writing.RunResult{}, fmt.Errorf("validate resumed planner artifact: %w", err)
		}
		return c.continueFromPlan(ctx, input, artifact, plan, emit)
	case CreatorDefinitionID:
		planArtifact, plan, err := c.loadPlan(ctx, input)
		if err != nil {
			return writing.RunResult{}, err
		}
		creatorSkill, err := c.loadCreatorSkill(ctx, plan)
		if err != nil {
			return writing.RunResult{}, err
		}
		return c.continueFromCreator(ctx, input, planArtifact, plan, artifact, creatorSkill, emit)
	case WriterDefinitionID:
		writerSkill, err := c.skills.Load(ctx, "prose-writer")
		if err != nil {
			return writing.RunResult{}, err
		}
		if err := emit(writing.EventRoleCompleted, map[string]any{"role": writing.RoleWriter, "artifact": artifact.Ref}); err != nil {
			return writing.RunResult{}, err
		}
		return resultFromArtifact(artifact, writerSkill, writing.RoleWriter, emit)
	default:
		return writing.RunResult{}, fmt.Errorf("resumed artifact belongs to unsupported agent %q", artifact.AgentID)
	}
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
		creatorInstruction(creatorSkill, creatorKind), creatorPrompt, writing.RoleCreator,
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
		if err := writing.ValidateProposalSet(string(creatorArtifact.Payload)); err != nil {
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
		writerInstruction(writerSkill), writerPrompt, writing.RoleWriter,
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
	parentAgentID string,
	parentTurnID string,
	emit writing.Emitter,
) (appharness.Artifact, error) {
	registered, err := c.definitions.Resolve(definitionID)
	if err != nil {
		return appharness.Artifact{}, err
	}
	output, err := outputForKind(registered.Definition.Output, expectedKind)
	if err != nil {
		return appharness.Artifact{}, err
	}
	allowedTools := intersectTools(registered.Definition.Tools, skill.AllowedTools)
	turnID := uuid.NewString()
	outcome, err := c.runner.Run(ctx, adk.LLMTurnRequest{
		RunID: input.RunID, TurnID: turnID, AgentID: registered.Definition.ID,
		ParentTurnID: parentTurnID, ParentAgentID: parentAgentID,
		AgentName: registered.Definition.Name, Description: registered.Definition.Description,
		Instruction: instruction, DefinitionVersion: registered.Definition.Version, DefinitionHash: registered.Hash,
		ProviderID: input.ProviderID, ModelID: input.ModelID,
		UserID: "work:" + input.WorkID, SessionID: uuid.NewString(), Prompt: prompt,
		AllowedTools: allowedTools,
		ControlTools: append([]string(nil), registered.Definition.ControlTools...),
		ToolInvocation: agentcore.ToolInvocation{
			RunID: input.RunID, TurnID: turnID, WorkID: input.WorkID, SkillID: skill.ID, SkillVersion: skill.Version,
		},
		Budget: registered.Definition.Budget, Context: registered.Definition.Context,
		Memory: registered.Definition.Memory, Output: output,
	}, func(event adk.LLMTurnEvent) error {
		return projectEvent(event, role, emit)
	})
	if err != nil {
		return appharness.Artifact{}, err
	}
	if outcome.Status == appharness.TurnAwaitingUser {
		return appharness.Artifact{}, writing.ErrApprovalRequired
	}
	if outcome.Status != appharness.TurnCompleted || outcome.StopReason != appharness.StopArtifactSubmitted || outcome.Artifact == nil {
		return appharness.Artifact{}, fmt.Errorf("agent %q completed without required artifact: status=%s reason=%s", definitionID, outcome.Status, outcome.StopReason)
	}
	if outcome.Artifact.Kind != expectedKind {
		return appharness.Artifact{}, fmt.Errorf("agent %q submitted artifact %q, expected %q", definitionID, outcome.Artifact.Kind, expectedKind)
	}
	artifact, err := c.artifacts.GetArtifact(ctx, outcome.Artifact.ID)
	if err != nil {
		return appharness.Artifact{}, fmt.Errorf("load submitted artifact: %w", err)
	}
	return artifact, nil
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
When the plan is complete, call submit_artifact exactly once with kind collaboration_plan_v1 and the plan object.
Do not return the plan as final text. The plan must route exploration to story-brainstorm/advice and prose to prose-creator.` +
		"\n\n# Active Skill\n" + skill.Instructions
}

func creatorInstruction(skill writing.Skill, artifactKind string) string {
	return `You are Warmmo's Creator. Consume the approved plan artifact and create exactly its requested deliverable.
Do not alter the plan or perform the Writer role. Read the minimum relevant context with the allowed tools.
When complete, call submit_artifact exactly once using the required kind ` + artifactKind + `.
Do not return the artifact as final text.` + "\n\n# Active Skill\n" + skill.Instructions
}

func writerInstruction(skill writing.Skill) string {
	return `You are Warmmo's Writer. Polish only the supplied draft while preserving facts, viewpoint, chronology, names and outcomes.
When complete, call submit_artifact exactly once with kind polished_prose_v1 and the polished prose string.
Do not return the prose as final text.` + "\n\n# Active Skill\n" + skill.Instructions
}

func outputForKind(contract appharness.OutputContract, kind string) (appharness.OutputContract, error) {
	artifact, ok := contract.Artifact(kind)
	if !ok {
		return appharness.OutputContract{}, fmt.Errorf("output contract does not allow artifact %q", kind)
	}
	return appharness.OutputContract{Kind: appharness.OutputKindArtifact, Artifacts: []appharness.ArtifactSchema{artifact}}, nil
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
		if err := emit(writing.EventMessageDelta, map[string]string{"delta": content}); err != nil {
			return writing.RunResult{}, err
		}
	}
	return writing.RunResult{
		Content: content, Role: role, SkillID: skill.ID, SkillVersion: skill.Version,
		ArtifactID: artifact.Ref.ID, ArtifactKind: artifact.Ref.Kind,
	}, nil
}

func projectEvent(event adk.LLMTurnEvent, role writing.AgentRole, emit writing.Emitter) error {
	data := map[string]any{
		"role": role, "agent": event.AgentName, "eventId": event.EventID,
		"toolName": event.ToolName, "toolCallId": event.ToolCallID,
		"summary": event.Summary, "errorCode": event.ErrorCode, "retryable": event.Retryable,
		"resultBytes": event.ResultBytes, "truncated": event.Truncated,
	}
	switch event.Type {
	case adk.LLMEventMessageDelta:
		return nil
	case adk.LLMEventToolRequested:
		return emit(writing.EventToolRequested, data)
	case adk.LLMEventToolStarted:
		return emit(writing.EventToolStarted, data)
	case adk.LLMEventToolCompleted:
		return emit(writing.EventToolCompleted, data)
	case adk.LLMEventToolFailed:
		return emit(writing.EventToolFailed, data)
	case adk.LLMEventTurnPaused:
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

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
)

type CanvasOrchestrator struct {
	definitions *appharness.DefinitionRegistry
	runner      appharness.AgentRuntime
	chain       *WritingCollaborationChain
	checkpoints appharness.CheckpointStore
}

func NewCanvasOrchestrator(
	definitions *appharness.DefinitionRegistry,
	runner appharness.AgentRuntime,
	chain *WritingCollaborationChain,
	checkpoints appharness.CheckpointStore,
) (*CanvasOrchestrator, error) {
	if definitions == nil || runner == nil || chain == nil || checkpoints == nil {
		return nil, errors.New("canvas orchestrator dependencies are required")
	}
	if _, err := definitions.Resolve(CanvasOrchestratorDefinitionID); err != nil {
		return nil, err
	}
	return &CanvasOrchestrator{definitions: definitions, runner: runner, chain: chain, checkpoints: checkpoints}, nil
}

func (o *CanvasOrchestrator) Run(ctx context.Context, input writing.RunInput, emit writing.Emitter) (writing.RunResult, error) {
	request, err := o.rootRequest(input, uuid.NewString())
	if err != nil {
		return writing.RunResult{}, err
	}
	projector := newOrchestratorProjector(emit)
	outcome, err := o.runner.Run(ctx, request, projector.Project)
	if err != nil {
		return writing.RunResult{}, err
	}
	return o.result(ctx, outcome, projector, emit)
}

func (o *CanvasOrchestrator) Resume(ctx context.Context, input writing.RunInput, answer string, emit writing.Emitter) (writing.RunResult, error) {
	root, err := o.latestRootCheckpoint(ctx, input.RunID)
	if err != nil {
		return writing.RunResult{}, fmt.Errorf("load canvas orchestrator checkpoint: %w", err)
	}
	if root.Status != appharness.TurnAwaitingUser {
		return writing.RunResult{}, fmt.Errorf("canvas orchestrator is not awaiting user input: %s", root.Status)
	}
	projector := newOrchestratorProjector(emit)
	outcome, err := o.runner.Resume(ctx, input.RunID, answer, projector.Project)
	if err != nil {
		return writing.RunResult{}, err
	}
	return o.result(ctx, outcome, projector, emit)
}

func (o *CanvasOrchestrator) Recover(ctx context.Context, input writing.RunInput, emit writing.Emitter) (writing.RunResult, error) {
	root, err := o.latestRootCheckpoint(ctx, input.RunID)
	if err != nil {
		return writing.RunResult{}, fmt.Errorf("load canvas orchestrator recovery checkpoint: %w", err)
	}
	if matches, matchErr := rootMatchesInput(root, input); matchErr != nil {
		return writing.RunResult{}, matchErr
	} else if !matches {
		return writing.RunResult{}, errors.New("latest orchestrator checkpoint belongs to an earlier collaboration batch")
	}
	switch root.Status {
	case appharness.TurnCompleted:
		outcome, restoreErr := o.runner.Restore(ctx, root)
		if restoreErr != nil {
			return writing.RunResult{}, restoreErr
		}
		if ref, ok, resultErr := specialistArtifact(outcome.ToolResults); resultErr != nil {
			return writing.RunResult{}, resultErr
		} else if ok {
			return o.chain.RecoverArtifact(ctx, ref, emit)
		}
		if root.Final != nil && strings.TrimSpace(root.Final.Content) != "" {
			if err := emit(writing.EventMessageDelta, map[string]any{"delta": root.Final.Content, "replace": true}); err != nil {
				return writing.RunResult{}, err
			}
		}
		return writing.RunResult{Role: writing.RoleOrchestrator}, nil
	case appharness.TurnAwaitingUser:
		return recoverApproval(root, writing.RoleOrchestrator, emit)
	default:
		return writing.RunResult{}, fmt.Errorf("canvas orchestrator turn cannot be recovered from status %q", root.Status)
	}
}

func (o *CanvasOrchestrator) result(
	ctx context.Context,
	outcome appharness.TurnOutcome,
	projector *orchestratorProjector,
	emit writing.Emitter,
) (writing.RunResult, error) {
	if outcome.Status == appharness.TurnAwaitingUser {
		return writing.RunResult{}, writing.ErrApprovalRequired
	}
	if outcome.Status != appharness.TurnCompleted {
		return writing.RunResult{}, fmt.Errorf("canvas orchestrator stopped unexpectedly: status=%s reason=%s budget_dimension=%s usage(model=%d tool=%d side_effect=%d)", outcome.Status, outcome.StopReason, outcome.BudgetDimension, outcome.Budget.ModelCalls, outcome.Budget.ToolCalls, outcome.Budget.SideEffectCalls)
	}
	if ref, ok, err := specialistArtifact(outcome.ToolResults); err != nil {
		return writing.RunResult{}, err
	} else if ok {
		return o.chain.RecoverArtifact(ctx, ref, emit)
	}
	if outcome.Final == nil {
		return writing.RunResult{}, errors.New("canvas orchestrator completed without a final response")
	}
	if err := projector.ReplaceFinal(outcome.Final.Content); err != nil {
		return writing.RunResult{}, err
	}
	return writing.RunResult{Role: writing.RoleOrchestrator}, nil
}

func specialistArtifact(results []appharness.ToolResult) (appharness.ArtifactRef, bool, error) {
	for i := len(results) - 1; i >= 0; i-- {
		if !isSpecialistTool(results[i].Name) {
			continue
		}
		var result struct {
			ArtifactID            string `json:"artifactId"`
			ArtifactKind          string `json:"artifactKind"`
			ArtifactSchemaVersion string `json:"artifactSchemaVersion"`
		}
		if err := json.Unmarshal(results[i].Output, &result); err != nil {
			return appharness.ArtifactRef{}, false, fmt.Errorf("decode specialist result %q: %w", results[i].Name, err)
		}
		if strings.TrimSpace(result.ArtifactID) == "" || strings.TrimSpace(result.ArtifactKind) == "" || strings.TrimSpace(result.ArtifactSchemaVersion) == "" {
			var failed struct {
				Error any `json:"error"`
			}
			if err := json.Unmarshal(results[i].Output, &failed); err == nil && failed.Error != nil {
				continue
			}
			return appharness.ArtifactRef{}, false, fmt.Errorf("specialist result %q has no writing output reference", results[i].Name)
		}
		return appharness.ArtifactRef{
			ID: result.ArtifactID, Kind: result.ArtifactKind, SchemaVersion: result.ArtifactSchemaVersion,
		}, true, nil
	}
	return appharness.ArtifactRef{}, false, nil
}

func isSpecialistTool(name string) bool {
	if name == PlannerDefinitionID {
		return true
	}
	for _, id := range directCreatorAgentIDs() {
		if name == id {
			return true
		}
	}
	return false
}

func (o *CanvasOrchestrator) rootRequest(input writing.RunInput, turnID string) (appharness.RuntimeRequest, error) {
	registered, err := o.definitions.Resolve(CanvasOrchestratorDefinitionID)
	if err != nil {
		return appharness.RuntimeRequest{}, err
	}
	prompt, err := encodePrompt(map[string]any{"request": input.Prompt, "mode": input.Target, "context": contextEnvelope(input)})
	if err != nil {
		return appharness.RuntimeRequest{}, err
	}
	extension, err := json.Marshal(SpecialistToolContext{Input: input})
	if err != nil {
		return appharness.RuntimeRequest{}, fmt.Errorf("encode agent tool context: %w", err)
	}
	return appharness.RuntimeRequest{
		RunID: input.RunID, TurnID: turnID, AgentID: registered.Definition.ID,
		AgentName: registered.Definition.Name, Description: registered.Definition.Description,
		Instruction: orchestratorInstruction(input.Target), DefinitionVersion: registered.Definition.Version, DefinitionHash: registered.Hash,
		ProviderID: input.ProviderID, ModelID: input.ModelID,
		UserID: "work:" + input.WorkID, SessionID: turnID, ConversationSessionID: input.ConversationSessionID,
		Prompt: prompt, ConversationUserContent: orchestratorConversationUserContent(input), PublishConversation: true,
		AllowedTools: append([]string(nil), registered.Definition.Tools...), ControlTools: []string{appharness.ControlToolAskUser}, Extension: extension,
		ToolInvocation: appharness.ToolInvocation{RunID: input.RunID, TurnID: turnID, WorkID: input.WorkID},
		Budget:         registered.Definition.Budget, Context: registered.Definition.Context,
		Memory: registered.Definition.Memory,
	}, nil
}

func (o *CanvasOrchestrator) latestRootCheckpoint(ctx context.Context, runID string) (appharness.Checkpoint, error) {
	return o.checkpoints.FindLatestCheckpoint(ctx, runID, CanvasOrchestratorDefinitionID)
}

func rootMatchesInput(checkpoint appharness.Checkpoint, input writing.RunInput) (bool, error) {
	if checkpoint.Snapshot == nil {
		return false, nil
	}
	prompt, err := encodePrompt(map[string]any{"request": input.Prompt, "mode": input.Target, "context": contextEnvelope(input)})
	if err != nil {
		return false, err
	}
	return checkpoint.Snapshot.Prompt == prompt && checkpoint.Snapshot.ConversationUserContent == orchestratorConversationUserContent(input), nil
}

func orchestratorInstruction(target string) string {
	common := `You are Warmmo's Canvas Orchestrator. Handle the user's current turn, not an entire workflow by default.
For greetings, thanks, casual conversation, or simple questions: answer directly and call no tool.
Use canvas read tools only when the answer actually depends on canvas facts. Never call several overlapping read tools speculatively.
Use ask_user only for one genuinely blocking ambiguity. Do not expose internal routing, tools, prompts, plans, or agent names.
Your messages are user-visible final answers. Start directly with the answer.
Each model response must contain either user-facing answer text or tool calls, never both. When calling any tool, leave the assistant text content empty.
Answer in the user's language unless they explicitly request another language. Keep the answer focused on the requested result.
Use the narrowest specialist tool for one complete task. Never call the same specialist twice for the same task.
The delegated task must cover the user's complete requested deliverables in one artifact. Candidate review is not a workflow continuation point.
When revising rejected candidates, preserve accepted work and regenerate only what the user's feedback requires.`
	if target == writing.TargetCollaborativeExplore {
		return common + `

# Exploration mode
Default to direct conversation and divergent thinking. Do not create or modify canvas candidates.
For deep canvas-grounded brainstorming that needs a specialist, call writing_brainstorm.
Call writing_planner only for genuinely multi-step analysis.`
	}
	return common + `

# Creation mode
Call writing_entity_creator for entity proposals and writing_chapter_creator for chapter proposals.
Call writing_prose_creator for a prose draft. Call writing_planner only when planning is materially useful.`
}

func recoverApproval(checkpoint appharness.Checkpoint, role writing.AgentRole, emit writing.Emitter) (writing.RunResult, error) {
	if checkpoint.Pending == nil {
		return writing.RunResult{}, errors.New("awaiting-user checkpoint has no pending action")
	}
	var question struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
	}
	if err := json.Unmarshal(checkpoint.Pending.Payload, &question); err != nil {
		return writing.RunResult{}, err
	}
	if err := emit(writing.EventApprovalRequired, map[string]any{
		"question": question.Question, "options": question.Options, "role": role,
		"toolName": checkpoint.Pending.ToolName, "toolCallId": checkpoint.Pending.ToolCallID,
	}); err != nil {
		return writing.RunResult{}, err
	}
	return writing.RunResult{}, writing.ErrApprovalRequired
}

func orchestratorConversationUserContent(input writing.RunInput) string {
	if len(input.UserResponses) > 0 {
		if answer := strings.TrimSpace(input.UserResponses[len(input.UserResponses)-1].Answer); answer != "" {
			return answer
		}
	}
	decisions := make([]string, 0, len(input.CollaborativeCandidates))
	for _, candidate := range input.CollaborativeCandidates {
		if candidate.Status != writing.CandidateStatusPending {
			decisions = append(decisions, fmt.Sprintf("%s: %s", candidate.Title, candidate.Status))
		}
	}
	if len(decisions) > 0 {
		return "Canvas candidate decisions: " + strings.Join(decisions, "; ")
	}
	return input.Prompt
}

type orchestratorProjector struct {
	emit        writing.Emitter
	textEmitted bool
}

func newOrchestratorProjector(emit writing.Emitter) *orchestratorProjector {
	return &orchestratorProjector{emit: emit}
}

func (p *orchestratorProjector) Project(event appharness.RuntimeEvent) error {
	switch event.Type {
	case appharness.RuntimeEventReasoningStarted:
		return p.emit(writing.EventReasoningStarted, map[string]any{"role": writing.RoleOrchestrator})
	case appharness.RuntimeEventReasoningDelta:
		return p.emit(writing.EventReasoningDelta, map[string]any{"delta": event.Text, "role": writing.RoleOrchestrator})
	case appharness.RuntimeEventReasoningCompleted:
		return p.emit(writing.EventReasoningCompleted, map[string]any{"role": writing.RoleOrchestrator})
	case appharness.RuntimeEventMessageDelta:
		if err := p.emit(writing.EventMessageDelta, map[string]any{"delta": event.Text}); err != nil {
			return err
		}
		p.textEmitted = true
	case appharness.RuntimeEventPaused:
		if event.ToolName == appharness.ControlToolAskUser {
			var question struct {
				Question string   `json:"question"`
				Options  []string `json:"options"`
			}
			_ = json.Unmarshal(event.Payload, &question)
			return p.emit(writing.EventApprovalRequired, map[string]any{"question": question.Question, "options": question.Options, "role": writing.RoleOrchestrator, "toolName": event.ToolName, "toolCallId": event.ToolCallID})
		}
	case appharness.RuntimeEventToolRequested, appharness.RuntimeEventToolStarted, appharness.RuntimeEventToolCompleted, appharness.RuntimeEventToolFailed:
		if event.ToolName != appharness.ControlToolAskUser {
			return projectEvent(event, writing.RoleOrchestrator, p.emit)
		}
	}
	return nil
}

func (p *orchestratorProjector) ReplaceFinal(content string) error {
	if p.textEmitted || strings.TrimSpace(content) == "" {
		return nil
	}
	return p.emit(writing.EventMessageDelta, map[string]any{"delta": content, "replace": true})
}

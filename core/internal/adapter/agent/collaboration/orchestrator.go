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

type CanvasOrchestrator struct {
	definitions *appharness.DefinitionRegistry
	runner      TurnRunner
	chain       *WritingCollaborationChain
	checkpoints appharness.CheckpointStore
}

type delegateRequest struct {
	AgentID    string
	Task       string
	Reason     string
	Input      map[string]any
	SkillID    string
	OutputKind string
}

func NewCanvasOrchestrator(
	definitions *appharness.DefinitionRegistry,
	runner TurnRunner,
	chain *WritingCollaborationChain,
	checkpoints appharness.CheckpointStore,
) (*CanvasOrchestrator, error) {
	if definitions == nil || runner == nil || chain == nil || checkpoints == nil {
		return nil, errors.New("canvas orchestrator dependencies are required")
	}
	if _, err := definitions.Resolve(CanvasOrchestratorDefinitionID); err != nil {
		return nil, err
	}
	return &CanvasOrchestrator{
		definitions: definitions, runner: runner, chain: chain, checkpoints: checkpoints,
	}, nil
}

func (o *CanvasOrchestrator) Run(
	ctx context.Context,
	input writing.RunInput,
	emit writing.Emitter,
) (writing.RunResult, error) {
	request, err := o.rootRequest(input, uuid.NewString())
	if err != nil {
		return writing.RunResult{}, err
	}
	projector := newOrchestratorProjector(emit, true, true)
	outcome, err := o.runner.Run(ctx, request, projector.Project)
	if err != nil {
		return writing.RunResult{}, err
	}
	return o.handleRootOutcome(ctx, input, outcome, projector, emit)
}

func (o *CanvasOrchestrator) Resume(
	ctx context.Context,
	input writing.RunInput,
	answer string,
	emit writing.Emitter,
) (writing.RunResult, error) {
	root, err := o.latestRootCheckpoint(ctx, input.RunID)
	if err != nil {
		return writing.RunResult{}, fmt.Errorf("load canvas orchestrator checkpoint: %w", err)
	}
	if root.Status == appharness.TurnAwaitingUser {
		projector := newOrchestratorProjector(emit, true, true)
		outcome, resumeErr := o.runner.Resume(ctx, input.RunID, answer, projector.Project)
		if resumeErr != nil {
			return writing.RunResult{}, resumeErr
		}
		return o.handleRootOutcome(ctx, input, outcome, projector, emit)
	}
	if root.Status != appharness.TurnAwaitingChild || root.Pending == nil {
		return writing.RunResult{}, fmt.Errorf("canvas orchestrator is not awaiting input: %s", root.Status)
	}
	delegation, err := parseDelegateRequest(root.Pending.Payload, input.Target)
	if err != nil {
		return writing.RunResult{}, err
	}
	var child writing.RunResult
	if delegation.AgentID == CreatorDefinitionID {
		child, err = o.chain.ResumeCreator(ctx, input, answer, delegation.Task, delegation.SkillID, delegation.OutputKind, emit)
	} else {
		child, err = o.chain.Resume(ctx, input, answer, emit)
	}
	if err != nil {
		return writing.RunResult{}, err
	}
	return o.continueAfterChild(ctx, input, root, delegation, child, emit)
}

func (o *CanvasOrchestrator) Recover(
	ctx context.Context,
	input writing.RunInput,
	emit writing.Emitter,
) (writing.RunResult, error) {
	root, err := o.latestRootCheckpoint(ctx, input.RunID)
	if err != nil {
		return writing.RunResult{}, fmt.Errorf("load canvas orchestrator recovery checkpoint: %w", err)
	}
	if matches, err := rootMatchesInput(root, input); err != nil {
		return writing.RunResult{}, err
	} else if !matches {
		return writing.RunResult{}, errors.New("latest orchestrator checkpoint belongs to an earlier collaboration batch")
	}
	switch root.Status {
	case appharness.TurnCompleted:
		if root.Artifact != nil {
			return o.chain.RecoverArtifact(ctx, *root.Artifact, emit)
		}
		if root.Final != nil && strings.TrimSpace(root.Final.Content) != "" {
			if err := emit(writing.EventMessageDelta, map[string]any{"delta": root.Final.Content, "replace": true}); err != nil {
				return writing.RunResult{}, err
			}
		}
		return writing.RunResult{Role: writing.RoleOrchestrator}, nil
	case appharness.TurnAwaitingUser:
		return recoverApproval(root, writing.RoleOrchestrator, emit)
	case appharness.TurnAwaitingChild:
		if root.Pending == nil {
			return writing.RunResult{}, errors.New("orchestrator child checkpoint has no pending action")
		}
		delegation, err := parseDelegateRequest(root.Pending.Payload, input.Target)
		if err != nil {
			return writing.RunResult{}, err
		}
		var child writing.RunResult
		var recoverErr error
		if delegation.AgentID == CreatorDefinitionID {
			child, recoverErr = o.chain.RecoverCreator(ctx, input, delegation.Task, delegation.SkillID, delegation.OutputKind, emit)
		} else {
			child, recoverErr = o.chain.Recover(ctx, input, emit)
		}
		if recoverErr != nil {
			return writing.RunResult{}, recoverErr
		}
		return o.continueAfterChild(ctx, input, root, delegation, child, emit)
	default:
		return writing.RunResult{}, fmt.Errorf("canvas orchestrator turn cannot be recovered from status %q", root.Status)
	}
}

func (o *CanvasOrchestrator) handleRootOutcome(
	ctx context.Context,
	input writing.RunInput,
	outcome adk.LLMTurnOutcome,
	projector *orchestratorProjector,
	emit writing.Emitter,
) (writing.RunResult, error) {
	switch outcome.Status {
	case appharness.TurnCompleted:
		if outcome.StopReason != appharness.StopFinalResponse || outcome.Final == nil {
			return writing.RunResult{}, fmt.Errorf("canvas orchestrator completed without a final response: %s", outcome.StopReason)
		}
		if err := projector.ReplaceFinal(outcome.Final.Content); err != nil {
			return writing.RunResult{}, err
		}
		return writing.RunResult{Role: writing.RoleOrchestrator}, nil
	case appharness.TurnAwaitingUser:
		return writing.RunResult{}, writing.ErrApprovalRequired
	case appharness.TurnAwaitingChild:
		if outcome.Pending == nil {
			return writing.RunResult{}, errors.New("canvas orchestrator delegated without a pending action")
		}
		delegation, err := parseDelegateRequest(outcome.Pending.Payload, input.Target)
		if err != nil {
			return writing.RunResult{}, err
		}
		return o.runDelegate(ctx, input, outcome.Pending, delegation, emit)
	default:
		return writing.RunResult{}, fmt.Errorf("canvas orchestrator stopped unexpectedly: status=%s reason=%s", outcome.Status, outcome.StopReason)
	}
}

func (o *CanvasOrchestrator) runDelegate(
	ctx context.Context,
	input writing.RunInput,
	pending *appharness.PendingAction,
	delegation delegateRequest,
	emit writing.Emitter,
) (writing.RunResult, error) {
	root, err := o.latestRootCheckpoint(ctx, input.RunID)
	if err != nil {
		return writing.RunResult{}, err
	}
	if root.Status != appharness.TurnAwaitingChild || root.Pending == nil || root.Pending.ToolCallID != pending.ToolCallID {
		return writing.RunResult{}, errors.New("orchestrator delegation checkpoint is not current")
	}
	if err := o.definitions.ValidateChildInput(CanvasOrchestratorDefinitionID, delegation.AgentID, delegation.Input); err != nil {
		return o.rejectDelegate(ctx, input, root, err, emit)
	}
	if delegation.AgentID == CreatorDefinitionID {
		if _, err := directCreatorPlan(input.Target, delegation.Task, delegation.SkillID, delegation.OutputKind); err != nil {
			return o.rejectDelegate(ctx, input, root, err, emit)
		}
	}
	if err := emit(writing.EventRoleHandoff, map[string]any{
		"from": writing.RoleOrchestrator, "to": roleForDefinition(delegation.AgentID),
		"agentId": delegation.AgentID, "reason": delegation.Reason,
	}); err != nil {
		return writing.RunResult{}, err
	}
	var child writing.RunResult
	err = nil
	if delegation.AgentID == CreatorDefinitionID {
		child, err = o.chain.RunCreator(ctx, input, root.TurnID, delegation.Task, delegation.SkillID, delegation.OutputKind, delegation.Input, emit)
	} else {
		child, err = o.chain.RunDelegated(ctx, input, root.TurnID, delegation.Task, delegation.Input, emit)
	}
	if err != nil {
		return writing.RunResult{}, err
	}
	root, err = o.checkpoints.GetCheckpoint(ctx, root.TurnID)
	if err != nil {
		return writing.RunResult{}, err
	}
	if root.Pending == nil || root.Pending.ToolCallID != pending.ToolCallID {
		return writing.RunResult{}, errors.New("orchestrator delegation checkpoint changed before child completion")
	}
	return o.continueAfterChild(ctx, input, root, delegation, child, emit)
}

func (o *CanvasOrchestrator) rejectDelegate(
	ctx context.Context,
	input writing.RunInput,
	root appharness.Checkpoint,
	reason error,
	emit writing.Emitter,
) (writing.RunResult, error) {
	projector := newOrchestratorProjector(emit, true, true)
	outcome, err := o.runner.Continue(ctx, root.TurnID, map[string]any{
		"status": "rejected",
		"error":  map[string]any{"code": "invalid_child_input", "message": reason.Error()},
	}, projector.Project)
	if err != nil {
		return writing.RunResult{}, err
	}
	return o.handleRootOutcome(ctx, input, outcome, projector, emit)
}

func (o *CanvasOrchestrator) continueAfterChild(
	ctx context.Context,
	input writing.RunInput,
	root appharness.Checkpoint,
	delegation delegateRequest,
	child writing.RunResult,
	emit writing.Emitter,
) (writing.RunResult, error) {
	response := map[string]any{
		"status": "completed", "agentId": delegation.AgentID,
		"artifactId": child.ArtifactID, "artifactKind": child.ArtifactKind, "artifactSchemaVersion": "1",
		"receipt": childReceipt(child),
	}
	projector := newOrchestratorProjector(emit, false, false)
	outcome, err := o.runner.Continue(ctx, root.TurnID, response, projector.Project)
	if err != nil {
		return writing.RunResult{}, err
	}
	if outcome.Status == appharness.TurnAwaitingChild {
		return o.handleRootOutcome(ctx, input, outcome, projector, emit)
	}
	if outcome.Status == appharness.TurnAwaitingUser {
		return writing.RunResult{}, writing.ErrApprovalRequired
	}
	if outcome.Status != appharness.TurnCompleted {
		return writing.RunResult{}, fmt.Errorf("orchestrator did not complete after child: status=%s reason=%s", outcome.Status, outcome.StopReason)
	}
	return child, nil
}

func childReceipt(child writing.RunResult) map[string]any {
	receipt := map[string]any{"deliverableCount": 1}
	if child.ArtifactKind != ProposalArtifact {
		return receipt
	}
	var proposal writing.ProposalSet
	if json.Unmarshal([]byte(child.Content), &proposal) != nil {
		return receipt
	}
	titles := make([]string, 0, len(proposal.Nodes)+len(proposal.Updates))
	for _, node := range proposal.Nodes {
		if title := strings.TrimSpace(node.Title); title != "" {
			titles = append(titles, title)
		}
	}
	for _, update := range proposal.Updates {
		if title := strings.TrimSpace(update.Title); title != "" {
			titles = append(titles, title)
		}
	}
	receipt["deliverableCount"] = len(proposal.Nodes) + len(proposal.Updates)
	receipt["nodeCount"] = len(proposal.Nodes)
	receipt["updateCount"] = len(proposal.Updates)
	receipt["titles"] = titles
	return receipt
}

func (o *CanvasOrchestrator) rootRequest(input writing.RunInput, turnID string) (adk.LLMTurnRequest, error) {
	registered, err := o.definitions.Resolve(CanvasOrchestratorDefinitionID)
	if err != nil {
		return adk.LLMTurnRequest{}, err
	}
	prompt, err := encodePrompt(map[string]any{
		"request": input.Prompt, "mode": input.Target, "context": contextEnvelope(input),
	})
	if err != nil {
		return adk.LLMTurnRequest{}, err
	}
	allowedTools := append([]string(nil), registered.Definition.Tools...)
	controlTools := append([]string(nil), registered.Definition.ControlTools...)
	allowedChildren := append([]appharness.ChildContract(nil), registered.Definition.AllowedChildren...)
	instruction := orchestratorInstruction(input.Target)
	if directOnlyTurn(input.Prompt) {
		allowedTools = nil
		controlTools = nil
		allowedChildren = nil
		instruction += `

# Direct-only turn
The current input is a short social message or an underspecified correction. No tool or child agent is available in this turn. Reply immediately. If the user has not identified what is wrong, ask one concise clarifying question without guessing.`
	}
	return adk.LLMTurnRequest{
		RunID: input.RunID, TurnID: turnID, AgentID: registered.Definition.ID,
		AgentName: registered.Definition.Name, Description: registered.Definition.Description,
		Instruction: instruction, DefinitionVersion: registered.Definition.Version, DefinitionHash: registered.Hash,
		ProviderID: input.ProviderID, ModelID: input.ModelID,
		UserID: "work:" + input.WorkID, SessionID: turnID, ConversationSessionID: input.ConversationSessionID,
		Prompt: prompt, ConversationUserContent: orchestratorConversationUserContent(input), PublishConversation: true,
		AllowedTools:    allowedTools,
		ControlTools:    controlTools,
		AllowedChildren: allowedChildren,
		ToolInvocation:  agentcore.ToolInvocation{RunID: input.RunID, TurnID: turnID, WorkID: input.WorkID},
		Budget:          registered.Definition.Budget, Context: registered.Definition.Context,
		Memory: registered.Definition.Memory, Output: registered.Definition.Output,
	}, nil
}

func directOnlyTurn(prompt string) bool {
	normalized := strings.TrimSpace(prompt)
	normalized = strings.Trim(normalized, "。！!？?，,～~ ")
	normalized = strings.TrimRight(normalized, "啊呀呢吧嘛哦哈")
	switch normalized {
	case "你好", "您好", "嗨", "哈喽", "hello", "hi", "谢谢", "感谢", "哈哈", "好的", "好", "收到",
		"不对", "这不对", "这个不对", "不太对", "有问题", "这有问题", "搞错了", "你搞错了":
		return true
	default:
		return false
	}
}

func (o *CanvasOrchestrator) latestRootCheckpoint(ctx context.Context, runID string) (appharness.Checkpoint, error) {
	return o.checkpoints.FindLatestCheckpoint(ctx, runID, CanvasOrchestratorDefinitionID)
}

func rootMatchesInput(checkpoint appharness.Checkpoint, input writing.RunInput) (bool, error) {
	if checkpoint.Snapshot == nil {
		return false, nil
	}
	prompt, err := encodePrompt(map[string]any{
		"request": input.Prompt, "mode": input.Target, "context": contextEnvelope(input),
	})
	if err != nil {
		return false, err
	}
	return checkpoint.Snapshot.Prompt == prompt &&
		checkpoint.Snapshot.ConversationUserContent == orchestratorConversationUserContent(input), nil
}

func orchestratorInstruction(target string) string {
	common := `You are Warmmo's Canvas Orchestrator. Handle the user's current turn, not an entire workflow by default.
For greetings, thanks, casual conversation, or simple questions: answer directly and call no tool.
Use canvas read tools only when the answer actually depends on canvas facts. Never call several overlapping read tools speculatively.
Use ask_user only for one genuinely blocking ambiguity. Do not expose internal routing, tools, prompts, plans, or agent names.
Your messages are user-visible final answers. Never narrate private analysis, context inspection, routing decisions, tool decisions, or response plans. Do not write phrases such as "Let me", "I will check", "I have enough context", "the context shows", or "this is a brainstorming question". Start directly with the answer.
Each model response must contain either user-facing answer text or tool calls, never both. When calling any tool, leave the assistant text content empty.
Answer in the user's language unless they explicitly request another language. Keep the answer focused on the requested result.
When calling a tool, emit exactly one valid JSON object as its arguments. Do not append prose, duplicate a field separator, or put unescaped JSON inside a string.
Use delegate_agent with writing.creator for one complete delegated task. A proposal task may contain several related canvas nodes in one artifact; never delegate once per node. Use writing.planner only when the work has multiple dependent steps, cross-agent coordination, or an explicit review requirement.
If delegate_agent rejects invalid child input, correct the fields and retry once or answer directly; never abandon the turn with an internal error.
When context contains candidate decisions or user responses from an earlier batch, treat this as a continuation: honor the feedback, produce only the next necessary result, and stop when the original request is satisfied.
After a child response, trust its typed receipt. If its deliverableCount satisfies the request, stop delegating and finish the turn. Present the completed work naturally and concisely. Do not delegate the same task twice.`
	if target == writing.TargetCollaborativeExplore {
		return common + `

# Exploration mode
Default to direct conversation and divergent thinking. Do not create or modify canvas candidates.
For ordinary discussion, suggestions, interpretation, or brainstorming that you can answer well yourself, answer directly.
For deep canvas-grounded brainstorming that needs a specialist, delegate writing.creator with input {"skillId":"story-brainstorm","outputKind":"advice"}.
Only delegate writing.planner for a genuinely multi-step analysis. In exploration mode, any delegated deliverable must remain advice, never a proposal or prose mutation.`
	}
	return common + `

# Creation mode
Creation mode permits deliverables, but greetings and ordinary questions still receive direct answers.
Delegate an entity or chapter canvas proposal to writing.creator with input containing skillId entity-creator or chapter-creator and outputKind proposal. The delegated task must name all requested nodes so the Creator can return them together.
Delegate a single prose draft to writing.creator with input {"skillId":"prose-creator","outputKind":"prose"}.
Delegate writing.planner only when planning is materially useful. Do not use it as a mandatory gateway.`
}

func parseDelegateRequest(payload json.RawMessage, target string) (delegateRequest, error) {
	var wire struct {
		AgentID string         `json:"agentId"`
		Task    string         `json:"task"`
		Reason  string         `json:"reason"`
		Input   map[string]any `json:"input"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return delegateRequest{}, fmt.Errorf("decode delegate request: %w", err)
	}
	if wire.Input == nil {
		wire.Input = make(map[string]any)
	}
	request := delegateRequest{AgentID: wire.AgentID, Task: wire.Task, Reason: wire.Reason, Input: wire.Input}
	request.AgentID = strings.TrimSpace(request.AgentID)
	request.Task = strings.TrimSpace(request.Task)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.AgentID != CreatorDefinitionID && request.AgentID != PlannerDefinitionID {
		return delegateRequest{}, fmt.Errorf("unsupported delegated agent %q", request.AgentID)
	}
	if request.Task == "" || request.Reason == "" {
		return delegateRequest{}, errors.New("delegate request requires task and reason")
	}
	if request.AgentID == CreatorDefinitionID {
		request.SkillID = delegateString(request.Input, "skillId")
		request.OutputKind = delegateString(request.Input, "outputKind")
		if target == writing.TargetCollaborativeExplore {
			request.SkillID = "story-brainstorm"
			request.OutputKind = "advice"
		}
	}
	return request, nil
}

func delegateString(input map[string]any, name string) string {
	if value, ok := input[name].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
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

func roleForDefinition(agentID string) writing.AgentRole {
	if agentID == PlannerDefinitionID {
		return writing.RolePlanner
	}
	return writing.RoleCreator
}

func orchestratorConversationUserContent(input writing.RunInput) string {
	if len(input.UserResponses) > 0 {
		latest := input.UserResponses[len(input.UserResponses)-1]
		if answer := strings.TrimSpace(latest.Answer); answer != "" {
			return answer
		}
	}
	decisions := make([]string, 0, len(input.CollaborativeCandidates))
	for _, candidate := range input.CollaborativeCandidates {
		if candidate.Status == writing.CandidateStatusPending {
			continue
		}
		decisions = append(decisions, fmt.Sprintf("%s: %s", candidate.Title, candidate.Status))
	}
	if len(decisions) > 0 {
		return "Canvas candidate decisions: " + strings.Join(decisions, "; ")
	}
	return input.Prompt
}

type orchestratorProjector struct {
	emit         writing.Emitter
	streamText   bool
	publishFinal bool
}

func newOrchestratorProjector(emit writing.Emitter, streamText, publishFinal bool) *orchestratorProjector {
	return &orchestratorProjector{emit: emit, streamText: streamText, publishFinal: publishFinal}
}

func (p *orchestratorProjector) Project(event adk.LLMTurnEvent) error {
	if event.Type == adk.LLMEventReasoningStarted {
		return p.emit(writing.EventReasoningStarted, map[string]any{"role": writing.RoleOrchestrator})
	}
	if event.Type == adk.LLMEventReasoningDelta {
		return p.emit(writing.EventReasoningDelta, map[string]any{"delta": event.Text, "role": writing.RoleOrchestrator})
	}
	if event.Type == adk.LLMEventReasoningCompleted {
		return p.emit(writing.EventReasoningCompleted, map[string]any{"role": writing.RoleOrchestrator})
	}
	if event.Type == adk.LLMEventMessageDelta && p.streamText {
		return p.emit(writing.EventMessageDelta, map[string]any{"delta": event.Text})
	}
	if event.Type == adk.LLMEventTurnPaused && event.ToolName == adk.AskUserToolName {
		var question struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
		}
		_ = json.Unmarshal(event.Payload, &question)
		return p.emit(writing.EventApprovalRequired, map[string]any{
			"question": question.Question, "options": question.Options, "role": writing.RoleOrchestrator,
			"toolName": event.ToolName, "toolCallId": event.ToolCallID,
		})
	}
	if event.Type == adk.LLMEventToolRequested || event.Type == adk.LLMEventToolStarted ||
		event.Type == adk.LLMEventToolCompleted || event.Type == adk.LLMEventToolFailed {
		if event.ToolName == adk.DelegateAgentToolName || event.ToolName == adk.AskUserToolName {
			return nil
		}
		return projectEvent(event, writing.RoleOrchestrator, p.emit)
	}
	return nil
}

func (p *orchestratorProjector) ReplaceFinal(content string) error {
	if !p.publishFinal || strings.TrimSpace(content) == "" {
		return nil
	}
	if p.streamText {
		return nil
	}
	return p.emit(writing.EventMessageDelta, map[string]any{"delta": content, "replace": true})
}

package writing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	agentcore "warmmo/core/internal/adapter/agent/core"
)

const (
	maxProposalAttempts    = 3
	maxProposalRepairRunes = 16 * 1024
)

const collaborationDecisionContract = `
# Collaboration Protocol

The run has three logical roles:
- planner: clarify intent, retrieve evidence, and return one structured plan with complete_plan.
- creator: use the approved plan and its skill to produce the requested artifact with produce_candidate.
- writer: polish prose only when the plan explicitly requests writerRequired=true.

The creator generates at most one new node per generation. After a candidate is
reviewed, the run may resume with the user's decision.

Before planning or producing another artifact, evaluate completion using these
general criteria:
1. Derive the complete set of requested deliverables and constraints from the original request and authoritative user responses.
2. Use collaborativeCandidates as the authoritative delivery ledger. Count only entries whose status is accepted as completed deliverables. Inspect their acceptedNodeId with canvas.get_nodes when their content is needed for the comparison.
3. Treat rejected candidates as unmet deliverables and use the user's rejection reason as a correction constraint.
4. Choose finish when every requested deliverable is represented by an accepted result and no explicit constraint or blocking question remains unresolved.
5. Continue only for a concrete unmet deliverable. Never recreate, extend, or replace an accepted result unless the user explicitly requests it.
6. Never infer delivered counts from prior assistant prose, plans, candidate JSON, or candidate.decision event text. Never claim that an artifact was delivered unless it appears as accepted in collaborativeCandidates.

Both planner and creator may choose finish when these completion criteria are met.
The finish content is the user-facing closing message. It must briefly report only actually accepted deliverables and may non-blockingly invite another creative goal.
If a creator is handed a plan whose requested artifact is already represented by an accepted candidate, it must choose finish. It must never emit an empty ProposalSet merely to report that no change is needed.

Convergence rules for planner reasoning:
- continue_brainstorm is an internal working action, not the final answer. Use it only when a concrete missing question, contradiction, or newly retrieved evidence changes the reasoning.
- Do not repeat, paraphrase, or expand a direction list merely because the user said “continue” or because more ideas are possible. A finite set of strong directions is sufficient.
- Once the current evidence supports a coherent set of directions, stop brainstorming: use complete_plan to hand the advice to story-brainstorm, or use finish when the advice is already ready for the user.
- Before every continue_brainstorm, state internally what new evidence or unresolved decision it will add. If there is none, choose complete_plan or finish.
- In collaborative-explore, prefer complete_plan so story-brainstorm produces the user-facing advice. A finish decision is control-plane completion only; its content is not the final rendered response.

Role action constraints:
- planner may use continue_brainstorm, call_tool, ask_user, complete_plan, finish, or fail. It must never use produce_candidate.
- creator may use continue_brainstorm, call_tool, ask_user, produce_candidate, finish, or fail. It must never use complete_plan or select_skill.

The planner complete_plan content must be exactly one JSON object:
{"intent":"...","brief":"...","contextQuery":"...","creatorTarget":"collaborative-targeted|collaborative-explore","creatorSkillId":"skill-id","outputKind":"proposal|advice|prose","writerRequired":false,"writerInstruction":""}

When a collaborative-targeted request is actually asking for analysis, evaluation, or ideas rather than an artifact, route it to collaborative-explore with story-brainstorm and outputKind=advice. A collaborative-explore run must never route back to a mutating or prose workflow.
Do not mutate the canvas during planning or creation. Return a complete proposal or advice artifact for user review.
Ask the user only about blocking ambiguity or contradictions. Do not ask for routine creative details.`

func (l *Loop) runCollaborative(ctx context.Context, execution *agentcore.Execution, input RunInput, emit Emitter) (RunResult, error) {
	if err := emit(EventSkillSearching, map[string]any{"target": TargetCollaborativePlanner, "role": RolePlanner}); err != nil {
		return RunResult{}, err
	}
	matches, err := l.skills.Search(ctx, TargetCollaborativePlanner, input.Prompt)
	if err != nil {
		return RunResult{}, fmt.Errorf("search planner skills: %w", err)
	}
	if len(matches) == 0 {
		return RunResult{}, fmt.Errorf("no planner skill supports target %q", TargetCollaborativePlanner)
	}
	if err := emit(EventSkillMatched, map[string]any{"matches": matches, "role": RolePlanner}); err != nil {
		return RunResult{}, err
	}
	state := loopState{input: input, matches: matches, role: RolePlanner}
	plannerSkillID := "creative-planner"
	if !matchContains(matches, plannerSkillID) {
		plannerSkillID = matches[0].ID
	}
	if err := l.selectSkill(ctx, &state, plannerSkillID, emit); err != nil {
		return RunResult{}, err
	}
	if err := emit(EventRoleStarted, map[string]any{"role": RolePlanner, "skillId": plannerSkillID}); err != nil {
		return RunResult{}, err
	}

	model := execution.Model()
	return runWritingTurns(ctx, execution, func(ctx context.Context, step int) (RunResult, bool, error) {
		decision, _, err := requestDecision(ctx, model, input.ModelID,
			state.decisionPrompt(step, l.tools), execution.RemainingModelCalls(), emit,
			collaborationDecisionSystem(state.role))
		if err != nil {
			return RunResult{}, false, err
		}

		switch decision.Kind {
		case DecisionContinueBrainstorm:
			if err := emit(EventBrainstormStarted, map[string]any{"step": step, "role": state.role}); err != nil {
				return RunResult{}, false, err
			}
			state.observations = append(state.observations, Observation{Source: "brainstorm", Summary: decision.Content})
			if err := emit(EventBrainstormCompleted, map[string]string{"summary": decision.Content}); err != nil {
				return RunResult{}, false, err
			}
		case DecisionCompletePlan:
			if state.role != RolePlanner {
				if err := rejectRoleDecision(&state, decision, "creator must use produce_candidate or finish", emit); err != nil {
					return RunResult{}, false, err
				}
				return RunResult{}, false, nil
			}
			plan, err := parseCollaborationPlan(decision.Content, input.Target)
			if err != nil {
				state.observations = append(state.observations, Observation{
					Source:  "plan_validation.error",
					Summary: err.Error(),
				})
				if emitErr := emit(EventDecisionInvalid, map[string]any{
					"kind":    DecisionCompletePlan,
					"role":    RolePlanner,
					"stage":   "plan_validation",
					"message": err.Error(),
				}); emitErr != nil {
					return RunResult{}, false, emitErr
				}
				return RunResult{}, false, nil
			}
			state.plan = decision.Content
			state.collaborationPlan = &plan
			if err := emit(EventPlanStarted, map[string]any{"step": step, "role": RolePlanner}); err != nil {
				return RunResult{}, false, err
			}
			if err := emit(EventPlanCompleted, map[string]any{"plan": plan, "role": RolePlanner}); err != nil {
				return RunResult{}, false, err
			}
			if err := emit(EventRoleCompleted, map[string]any{"role": RolePlanner}); err != nil {
				return RunResult{}, false, err
			}
			if err := l.handoffToCreator(ctx, &state, plan, emit); err != nil {
				return RunResult{}, false, err
			}
		case DecisionSelectSkill:
			if state.role != RolePlanner {
				if err := rejectRoleDecision(&state, decision, "creator cannot select another skill; use the active skill", emit); err != nil {
					return RunResult{}, false, err
				}
				return RunResult{}, false, nil
			}
			if err := l.selectSkill(ctx, &state, decision.SkillID, emit); err != nil {
				return RunResult{}, false, err
			}
		case DecisionCallTool:
			if state.activeSkill.ID == "" {
				return RunResult{}, false, errors.New("collaborative agent requested a tool before selecting a skill")
			}
			result, err := execution.CallTool(ctx, decision.ToolName, agentcore.ToolInvocation{
				RunID: input.RunID, WorkID: input.WorkID,
				SkillID: state.activeSkill.ID, SkillVersion: state.activeSkill.Version,
				AllowedTools: state.activeSkill.AllowedTools, Args: decision.ToolArgs,
			}, map[string]any{"role": state.role})
			if err != nil {
				if errors.Is(err, agentcore.ErrToolNotAllowed) || errors.Is(err, agentcore.ErrToolNotFound) {
					return RunResult{}, false, fmt.Errorf("call tool %q: %w", decision.ToolName, err)
				}
				state.observations = append(state.observations, Observation{Source: decision.ToolName + ".error", Summary: err.Error()})
				return RunResult{}, false, nil
			}
			state.observations = append(state.observations, Observation{Source: decision.ToolName, Summary: summarize(result)})
		case DecisionAskUser:
			if err := emit(EventApprovalRequired, map[string]any{"question": decision.Question, "reason": decision.Reason, "options": decision.Options, "role": state.role}); err != nil {
				return RunResult{}, false, err
			}
			return RunResult{}, false, ErrApprovalRequired
		case DecisionProduceCandidate:
			if state.role != RoleCreator {
				if err := rejectRoleDecision(&state, decision, "planner must use finish when complete or complete_plan when work remains", emit); err != nil {
					return RunResult{}, false, err
				}
				return RunResult{}, false, nil
			}
			result, err := l.produceCollaborativeResult(ctx, execution, &state, model, emit, execution.RemainingModelCalls())
			return result, err == nil, err
		case DecisionFinish:
			if err := emit(EventRoleCompleted, map[string]any{"role": state.role, "reason": decision.Content}); err != nil {
				return RunResult{}, false, err
			}
			if input.Target == TargetCollaborativeExplore {
				response, err := l.streamCollaborativeFinish(ctx, state, decision.Content, model, emit)
				if err != nil {
					return RunResult{}, false, err
				}
				return RunResult{
					Content: response, Role: state.role,
					SkillID: state.activeSkill.ID, SkillVersion: state.activeSkill.Version,
				}, true, nil
			}
			return RunResult{Message: strings.TrimSpace(decision.Content), Role: state.role, SkillID: state.activeSkill.ID, SkillVersion: state.activeSkill.Version}, true, nil
		case DecisionFail:
			return RunResult{}, false, fmt.Errorf("collaborative agent stopped: %s", decision.Reason)
		default:
			return RunResult{}, false, fmt.Errorf("unsupported collaborative decision kind %q", decision.Kind)
		}
		return RunResult{}, false, nil
	})
}

func rejectRoleDecision(state *loopState, decision Decision, correction string, emit Emitter) error {
	state.observations = append(state.observations, Observation{
		Source:  "role_guardrail",
		Summary: correction,
	})
	return emit(EventDecisionInvalid, map[string]any{
		"kind": decision.Kind, "role": state.role, "message": correction,
	})
}

func collaborationDecisionSystem(role AgentRole) string {
	return decisionInstruction + "\n\n" + collaborationDecisionContract +
		fmt.Sprintf("\n\nCurrent role: %s.", role)
}

func parseCollaborationPlan(value, runTarget string) (CollaborationPlan, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	value = strings.TrimSpace(value)
	var plan CollaborationPlan
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return CollaborationPlan{}, fmt.Errorf("decode collaboration plan: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return CollaborationPlan{}, errors.New("collaboration plan must contain exactly one JSON object")
	}
	if strings.TrimSpace(plan.Intent) == "" || strings.TrimSpace(plan.Brief) == "" {
		return CollaborationPlan{}, errors.New("collaboration plan requires intent and brief")
	}
	if strings.TrimSpace(plan.CreatorSkillID) == "" {
		return CollaborationPlan{}, errors.New("collaboration plan requires creatorSkillId")
	}
	switch plan.OutputKind {
	case "proposal", "advice", "prose":
	default:
		return CollaborationPlan{}, errors.New("collaboration plan outputKind must be proposal, advice, or prose")
	}
	if plan.WriterRequired && plan.OutputKind != "prose" {
		return CollaborationPlan{}, errors.New("writerRequired is only valid for prose output")
	}
	if runTarget == TargetCollaborativeExplore || plan.OutputKind == "advice" {
		if plan.CreatorSkillID != "story-brainstorm" || plan.OutputKind != "advice" || plan.WriterRequired {
			return CollaborationPlan{}, errors.New("divergent exploration requires story-brainstorm advice without a writer")
		}
		plan.CreatorTarget = TargetCollaborativeExplore
		return plan, nil
	}
	if strings.TrimSpace(plan.CreatorTarget) == "" {
		plan.CreatorTarget = TargetCollaborativeTargeted
	}
	if plan.CreatorTarget != TargetCollaborativeTargeted {
		return CollaborationPlan{}, fmt.Errorf("collaboration plan creatorTarget must be %q", TargetCollaborativeTargeted)
	}
	if plan.OutputKind == "proposal" {
		if plan.CreatorSkillID != "chapter-creator" && plan.CreatorSkillID != "entity-creator" {
			return CollaborationPlan{}, errors.New("proposal output requires chapter-creator or entity-creator")
		}
		if plan.WriterRequired {
			return CollaborationPlan{}, errors.New("proposal output cannot request a writer")
		}
	}
	if plan.OutputKind == "prose" && plan.CreatorSkillID != "prose-creator" {
		return CollaborationPlan{}, errors.New("prose output requires prose-creator")
	}
	return plan, nil
}

func (l *Loop) handoffToCreator(ctx context.Context, state *loopState, plan CollaborationPlan, emit Emitter) error {
	matches, err := l.skills.Search(ctx, plan.CreatorTarget, plan.Brief)
	if err != nil {
		return fmt.Errorf("search creator skills: %w", err)
	}
	if !matchContains(matches, plan.CreatorSkillID) {
		return fmt.Errorf("creator skill %q does not support target %q", plan.CreatorSkillID, plan.CreatorTarget)
	}
	if err := emit(EventRoleHandoff, map[string]any{"from": RolePlanner, "to": RoleCreator, "skillId": plan.CreatorSkillID}); err != nil {
		return err
	}
	if err := emit(EventSkillSearching, map[string]any{"target": plan.CreatorTarget, "role": RoleCreator}); err != nil {
		return err
	}
	if err := emit(EventSkillMatched, map[string]any{"matches": matches, "role": RoleCreator}); err != nil {
		return err
	}
	state.matches = matches
	state.role = RoleCreator
	if err := l.selectSkill(ctx, state, plan.CreatorSkillID, emit); err != nil {
		return err
	}
	return emit(EventRoleStarted, map[string]any{"role": RoleCreator, "skillId": plan.CreatorSkillID})
}

func (l *Loop) produceCollaborativeResult(
	ctx context.Context,
	execution *agentcore.Execution,
	state *loopState,
	model TextModel,
	emit Emitter,
	remainingModelCalls int,
) (RunResult, error) {
	plan := state.collaborationPlan
	if plan == nil {
		return RunResult{}, errors.New("collaborative creator has no approved plan")
	}
	if remainingModelCalls <= 0 {
		return RunResult{}, errors.New("model call budget exceeded before collaborative generation")
	}
	if plan.WriterRequired && remainingModelCalls < 2 {
		return RunResult{}, errors.New("model call budget cannot cover creator and writer generation")
	}
	creatorModelCalls := remainingModelCalls
	if plan.WriterRequired {
		creatorModelCalls--
	}
	if err := emit(EventGenerationStarted, map[string]any{"mode": "collaborative", "role": RoleCreator, "target": state.input.Target}); err != nil {
		return RunResult{}, err
	}
	var response string
	responseStreamed := false
	var err error
	if plan.OutputKind == "proposal" {
		response, err = requestProposalSet(
			ctx,
			model,
			state.input.ModelID,
			canvasContextSystemPrompt(state.activeSkill.Instructions),
			state.candidatePrompt(),
			creatorModelCalls,
			emit,
		)
	} else if plan.OutputKind == "advice" {
		response, err = l.streamExplorationAdvice(ctx, *state, model, emit)
		responseStreamed = err == nil
	} else {
		response, _, err = model.Complete(ctx, ModelRequest{
			ModelID: state.input.ModelID,
			System:  canvasContextSystemPrompt(state.activeSkill.Instructions),
			Prompt:  state.candidatePrompt(),
		})
	}
	if err != nil {
		return RunResult{}, fmt.Errorf("produce collaborative result: %w", err)
	}
	response = strings.TrimSpace(response)
	if response == "" {
		return RunResult{}, errors.New("collaborative creator returned an empty result")
	}
	if proposalHasNoChanges(response) && hasAcceptedCollaborativeCandidate(state.input.CollaborativeCandidates) {
		if err := emit(EventSkillCompleted, map[string]any{"skillId": state.activeSkill.ID, "version": state.activeSkill.Version}); err != nil {
			return RunResult{}, err
		}
		if err := emit(EventValidationCompleted, map[string]any{"valid": true, "noOp": true}); err != nil {
			return RunResult{}, err
		}
		if err := emit(EventRoleCompleted, map[string]any{"role": RoleCreator, "reason": "proposal already satisfied"}); err != nil {
			return RunResult{}, err
		}
		return RunResult{
			Message: "已确认现有候选节点已经覆盖本次目标，本轮未重复创建新的节点。",
			Role:    state.role, SkillID: state.activeSkill.ID, SkillVersion: state.activeSkill.Version,
		}, nil
	}
	if proposalHasNoChanges(response) {
		return RunResult{}, errors.New("collaborative creator returned no changes without an accepted candidate")
	}
	if err := emit(EventRoleCompleted, map[string]any{"role": RoleCreator}); err != nil {
		return RunResult{}, err
	}
	if !plan.WriterRequired {
		if plan.OutputKind != "proposal" && !responseStreamed {
			if err := emit(EventMessageDelta, map[string]string{"delta": response}); err != nil {
				return RunResult{}, err
			}
		}
		return l.completeContent(ctx, execution, *state, response, emit)
	}
	return l.polishCollaborativeResult(ctx, execution, *state, response, model, emit)
}

func (l *Loop) streamExplorationAdvice(ctx context.Context, state loopState, model TextModel, emit Emitter) (string, error) {
	request := ModelRequest{
		ModelID: state.input.ModelID,
		System:  canvasContextSystemPrompt(state.activeSkill.Instructions),
		Prompt:  state.candidatePrompt(),
	}
	return streamAdviceRequest(ctx, request, state.input.ModelID, model, emit)
}

func (l *Loop) streamCollaborativeFinish(
	ctx context.Context,
	state loopState,
	decisionContent string,
	model TextModel,
	emit Emitter,
) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"request":          state.input.Prompt,
		"completionReason": decisionContent,
		"plan":             state.collaborationPlan,
		"observations":     state.observations,
		"instruction":      "Answer the user's current request directly with concise, natural-language creative advice. Do not mention agent roles, plans, tools, completion state, or this instruction. Do not output JSON or a schema.",
	})
	if err != nil {
		return "", fmt.Errorf("encode collaborative response: %w", err)
	}
	return streamAdviceRequest(ctx, ModelRequest{
		ModelID: state.input.ModelID,
		System:  "You are Warmmo's user-facing collaborative writing partner. Return only the final conversational answer in the user's language.",
		Prompt:  string(payload),
	}, state.input.ModelID, model, emit)
}

func streamAdviceRequest(
	ctx context.Context,
	request ModelRequest,
	modelID string,
	model TextModel,
	emit Emitter,
) (string, error) {
	var raw strings.Builder
	var visible strings.Builder
	var prefix strings.Builder
	var pending strings.Builder
	decided := false
	structured := false
	flush := func() error {
		if pending.Len() == 0 {
			return nil
		}
		delta := pending.String()
		pending.Reset()
		return emit(EventMessageDelta, map[string]string{"delta": delta})
	}
	emitDelta := func(delta string) error {
		if delta == "" {
			return nil
		}
		visible.WriteString(delta)
		pending.WriteString(delta)
		if pending.Len() >= 48 || strings.ContainsAny(delta, "\r\n") {
			return flush()
		}
		return nil
	}
	_, err := model.Stream(ctx, request, func(delta string) error {
		raw.WriteString(delta)
		if decided {
			if structured {
				return nil
			}
			return emitDelta(delta)
		}
		prefix.WriteString(delta)
		trimmed := strings.TrimSpace(prefix.String())
		if trimmed == "" {
			return nil
		}
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "```") {
			decided = true
			structured = true
			return nil
		}
		// A code fence can be split across provider deltas. Hold a partial
		// prefix until it is distinguishable from ordinary prose.
		if strings.HasPrefix(trimmed, "`") && len(trimmed) < 3 {
			return nil
		}
		decided = true
		return emitDelta(prefix.String())
	})
	if err != nil {
		return "", err
	}
	if !decided {
		if err := emitDelta(prefix.String()); err != nil {
			return "", err
		}
	}
	if !structured {
		if err := flush(); err != nil {
			return "", err
		}
		return strings.TrimSpace(visible.String()), nil
	}
	if formatted, ok := formatStructuredExplorationAdvice(raw.String()); ok {
		if err := emitDelta(formatted); err != nil {
			return "", err
		}
		if err := flush(); err != nil {
			return "", err
		}
		return strings.TrimSpace(visible.String()), nil
	}
	_, err = model.Stream(ctx, ModelRequest{
		ModelID: modelID,
		System:  "你是 Warmmo 的闲聊创作顾问。把输入的内部结构化创作建议改写成自然、简洁、可直接发给用户的中文 prose。不要输出 JSON、代码块、字段名或解释转换过程。",
		Prompt:  "请将下面内容改写为自然语言建议：\n\n" + raw.String(),
	}, emitDelta)
	if err != nil {
		return "", fmt.Errorf("stream normalized exploration advice: %w", err)
	}
	if err := flush(); err != nil {
		return "", err
	}
	return strings.TrimSpace(visible.String()), nil
}

func formatStructuredExplorationAdvice(response string) (string, bool) {
	var payload struct {
		Directions []struct {
			Title            string   `json:"title"`
			Idea             string   `json:"idea"`
			Tension          string   `json:"tension"`
			Payoff           string   `json:"payoff"`
			RequiredNewNodes []string `json:"requiredNewNodes"`
			Risks            []string `json:"risks"`
		} `json:"directions"`
	}
	cleaned := strings.TrimSpace(response)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimSuffix(strings.TrimSpace(cleaned), "```")
	if err := json.Unmarshal([]byte(strings.TrimSpace(cleaned)), &payload); err == nil && len(payload.Directions) > 0 {
		var builder strings.Builder
		builder.WriteString("可以从以下几条并行线扩展：\n")
		for index, direction := range payload.Directions {
			if strings.TrimSpace(direction.Title) != "" {
				fmt.Fprintf(&builder, "\n%d. %s\n", index+1, strings.TrimSpace(direction.Title))
			}
			if strings.TrimSpace(direction.Idea) != "" {
				fmt.Fprintf(&builder, "%s\n", strings.TrimSpace(direction.Idea))
			}
			if strings.TrimSpace(direction.Tension) != "" {
				fmt.Fprintf(&builder, "戏剧张力：%s\n", strings.TrimSpace(direction.Tension))
			}
			if strings.TrimSpace(direction.Payoff) != "" {
				fmt.Fprintf(&builder, "可能回收：%s\n", strings.TrimSpace(direction.Payoff))
			}
			if len(direction.RequiredNewNodes) > 0 {
				fmt.Fprintf(&builder, "可继续创建：%s\n", strings.Join(direction.RequiredNewNodes, "、"))
			}
			if len(direction.Risks) > 0 {
				fmt.Fprintf(&builder, "注意：%s\n", strings.Join(direction.Risks, "；"))
			}
		}
		return strings.TrimSpace(builder.String()), true
	}
	return "", false
}

func requestProposalSet(
	ctx context.Context,
	model TextModel,
	modelID string,
	system string,
	prompt string,
	remainingModelCalls int,
	emit Emitter,
) (string, error) {
	attempts := min(maxProposalAttempts, remainingModelCalls)
	currentPrompt := prompt
	var lastErr error
	var lastResponse string
	for attempt := 1; attempt <= attempts; attempt++ {
		response, _, err := model.Complete(ctx, ModelRequest{
			ModelID:        modelID,
			System:         system,
			Prompt:         currentPrompt,
			ResponseFormat: ModelResponseFormatJSONObject,
		})
		if err != nil {
			return "", err
		}
		response = strings.TrimSpace(response)
		validationErr := validateProposalSet(response)
		if validationErr == nil {
			return response, nil
		}
		// A structurally valid empty ProposalSet is a creator no-op. It means
		// the approved artifact is already present and must not be retried as a
		// schema failure or turned into a duplicate candidate.
		if proposalHasNoChanges(response) {
			return response, nil
		}
		lastErr = validationErr
		lastResponse = response
		if err := emit(EventDecisionInvalid, map[string]any{
			"attempt":         attempt,
			"message":         validationErr.Error(),
			"responsePreview": truncateRunes(response, maxDecisionDiagnosticRunes),
			"role":            RoleCreator,
			"stage":           "proposal_validation",
		}); err != nil {
			return "", err
		}
		if attempt < attempts {
			currentPrompt = proposalRepairPrompt(prompt, response, validationErr)
		}
	}
	return "", fmt.Errorf(
		"invalid proposal set after %d attempt(s): %v (response preview: %q)",
		attempts,
		lastErr,
		truncateRunes(lastResponse, maxDecisionDiagnosticRunes),
	)
}

func proposalHasNoChanges(value string) bool {
	var proposal ProposalSet
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(value)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil {
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return false
	}
	return len(proposal.Nodes) == 0 && len(proposal.Updates) == 0
}

func hasAcceptedCollaborativeCandidate(candidates []CollaborativeCandidate) bool {
	for _, candidate := range candidates {
		if candidate.Status == CandidateStatusAccepted {
			return true
		}
	}
	return false
}

func proposalRepairPrompt(originalPrompt, invalidResponse string, validationErr error) string {
	payload := map[string]any{
		"task":            "repair_proposal_set",
		"validationError": validationErr.Error(),
		"invalidResponse": truncateRunes(invalidResponse, maxProposalRepairRunes),
		"originalInput":   json.RawMessage(originalPrompt),
		"requiredShape": map[string]any{
			"baseRevisions": map[string]int64{},
			"nodes": []map[string]string{{
				"clientId": "new-character-1", "kind": "character", "title": "角色名", "content": "完整角色画像",
			}},
			"updates":   []map[string]any{},
			"edges":     []map[string]string{},
			"reasons":   []string{"创建该节点的原因"},
			"questions": []string{},
		},
		"rules": []string{
			"Return exactly one JSON object and nothing else.",
			"Use exactly these top-level fields: baseRevisions, nodes, updates, edges, reasons, questions.",
			"Do not wrap the object in proposalSet, data, result, or any other field.",
			"Do not add metadata or any other unknown field.",
			"Each edge may use only sourceId, targetId, and kind.",
			"Preserve the intended creative content while repairing only the schema.",
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return originalPrompt
	}
	return string(encoded)
}

func validateProposalSet(value string) error {
	if !json.Valid([]byte(value)) {
		return errors.New("proposal set must be valid JSON")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &fields); err != nil || fields == nil {
		return errors.New("proposal set must be a JSON object")
	}
	for _, field := range []string{"baseRevisions", "nodes", "updates", "edges", "reasons", "questions"} {
		if _, exists := fields[field]; !exists {
			return fmt.Errorf("proposal set requires top-level field %q", field)
		}
	}
	var proposal ProposalSet
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil {
		return fmt.Errorf("decode proposal set: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("proposal set must contain exactly one JSON object")
	}
	if len(proposal.Nodes) == 0 && len(proposal.Updates) == 0 {
		return errors.New("proposal set must contain at least one node or update")
	}
	if len(proposal.Nodes) > 1 {
		return errors.New("proposal set must contain at most one node per generation")
	}
	for index, node := range proposal.Nodes {
		if strings.TrimSpace(node.ClientID) == "" || strings.TrimSpace(node.Kind) == "" ||
			strings.TrimSpace(node.Title) == "" || strings.TrimSpace(node.Content) == "" {
			return fmt.Errorf("proposal node %d requires clientId, kind, title, and content", index)
		}
	}
	for index, update := range proposal.Updates {
		if strings.TrimSpace(update.NodeID) == "" || update.BaseRevision < 1 ||
			strings.TrimSpace(update.Title) == "" || strings.TrimSpace(update.Content) == "" {
			return fmt.Errorf("proposal update %d requires nodeId, positive baseRevision, title, and content", index)
		}
	}
	for index, edge := range proposal.Edges {
		if strings.TrimSpace(edge.SourceID) == "" || strings.TrimSpace(edge.TargetID) == "" || strings.TrimSpace(edge.Kind) == "" {
			return fmt.Errorf("proposal edge %d requires sourceId, targetId, and kind", index)
		}
	}
	return nil
}

func (l *Loop) polishCollaborativeResult(ctx context.Context, execution *agentcore.Execution, state loopState, draft string, model TextModel, emit Emitter) (RunResult, error) {
	matches, err := l.skills.Search(ctx, TargetWritingPolish, state.collaborationPlan.WriterInstruction)
	if err != nil {
		return RunResult{}, fmt.Errorf("search writer skills: %w", err)
	}
	if len(matches) == 0 {
		return RunResult{}, errors.New("no writer skill is available")
	}
	writerSkillID := "prose-writer"
	if !matchContains(matches, writerSkillID) {
		writerSkillID = matches[0].ID
	}
	if err := emit(EventRoleHandoff, map[string]any{"from": RoleCreator, "to": RoleWriter, "skillId": writerSkillID}); err != nil {
		return RunResult{}, err
	}
	if err := l.selectSkill(ctx, &state, writerSkillID, emit); err != nil {
		return RunResult{}, err
	}
	state.role = RoleWriter
	if err := emit(EventRoleStarted, map[string]any{"role": RoleWriter, "skillId": writerSkillID}); err != nil {
		return RunResult{}, err
	}
	payload, _ := json.Marshal(map[string]any{
		"request":           state.input.Prompt,
		"brief":             state.collaborationPlan.Brief,
		"writerInstruction": state.collaborationPlan.WriterInstruction,
		"draft":             draft,
	})
	polished, _, err := model.Complete(ctx, ModelRequest{
		ModelID:        state.input.ModelID,
		System:         canvasContextSystemPrompt(state.activeSkill.Instructions),
		Prompt:         string(payload),
		ResponseFormat: ModelResponseFormatText,
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("polish collaborative result: %w", err)
	}
	polished = strings.TrimSpace(polished)
	if polished == "" {
		return RunResult{}, errors.New("writer returned an empty result")
	}
	if err := emit(EventMessageDelta, map[string]string{"delta": polished}); err != nil {
		return RunResult{}, err
	}
	if err := emit(EventRoleCompleted, map[string]any{"role": RoleWriter}); err != nil {
		return RunResult{}, err
	}
	state.activeSkill = Skill{ID: writerSkillID, Version: state.activeSkill.Version}
	return l.completeContent(ctx, execution, state, polished, emit)
}

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
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

The planner complete_plan content must be exactly one JSON object:
{"intent":"...","brief":"...","contextQuery":"...","creatorTarget":"collaborative-targeted|collaborative-explore","creatorSkillId":"skill-id","outputKind":"proposal|advice|prose","writerRequired":false,"writerInstruction":""}

When a collaborative-targeted request is actually asking for analysis, evaluation, or ideas rather than an artifact, route it to collaborative-explore with story-brainstorm and outputKind=advice. A collaborative-explore run must never route back to a mutating or prose workflow.
Do not mutate the canvas during planning or creation. Return a complete proposal or advice artifact for user review.
Ask the user only about blocking ambiguity or contradictions. Do not ask for routine creative details.`

func (l *Loop) runCollaborative(ctx context.Context, input RunInput, model TextModel, emit Emitter) (RunResult, error) {
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

	modelCalls, toolCalls := 0, 0
	for step := 1; step <= l.budget.MaxSteps; step++ {
		if modelCalls >= l.budget.MaxModelCalls {
			return RunResult{}, fmt.Errorf("model call budget exceeded: %d", l.budget.MaxModelCalls)
		}
		decision, calls, err := requestDecision(ctx, model, input.ModelID,
			state.decisionPrompt(step, l.tools), l.budget.MaxModelCalls-modelCalls, emit,
			collaborationDecisionSystem(state.role))
		modelCalls += calls
		if err != nil {
			return RunResult{}, err
		}

		switch decision.Kind {
		case DecisionContinueBrainstorm:
			if err := emit(EventBrainstormStarted, map[string]any{"step": step, "role": state.role}); err != nil {
				return RunResult{}, err
			}
			state.observations = append(state.observations, Observation{Source: "brainstorm", Summary: decision.Content})
			if err := emit(EventBrainstormCompleted, map[string]string{"summary": decision.Content}); err != nil {
				return RunResult{}, err
			}
		case DecisionCompletePlan:
			if state.role != RolePlanner {
				return RunResult{}, errors.New("creator cannot complete another collaboration plan")
			}
			plan, err := parseCollaborationPlan(decision.Content, input.Target)
			if err != nil {
				return RunResult{}, err
			}
			state.plan = decision.Content
			state.collaborationPlan = &plan
			if err := emit(EventPlanStarted, map[string]any{"step": step, "role": RolePlanner}); err != nil {
				return RunResult{}, err
			}
			if err := emit(EventPlanCompleted, map[string]any{"plan": plan, "role": RolePlanner}); err != nil {
				return RunResult{}, err
			}
			if err := emit(EventRoleCompleted, map[string]any{"role": RolePlanner}); err != nil {
				return RunResult{}, err
			}
			if err := l.handoffToCreator(ctx, &state, plan, emit); err != nil {
				return RunResult{}, err
			}
		case DecisionSelectSkill:
			if state.role != RolePlanner {
				return RunResult{}, errors.New("creator cannot select another skill")
			}
			if err := l.selectSkill(ctx, &state, decision.SkillID, emit); err != nil {
				return RunResult{}, err
			}
		case DecisionCallTool:
			if state.activeSkill.ID == "" {
				return RunResult{}, errors.New("collaborative agent requested a tool before selecting a skill")
			}
			if toolCalls >= l.budget.MaxToolCalls {
				return RunResult{}, fmt.Errorf("tool call budget exceeded: %d", l.budget.MaxToolCalls)
			}
			if err := emit(EventToolRequested, map[string]any{"name": decision.ToolName, "arguments": decision.ToolArgs, "role": state.role}); err != nil {
				return RunResult{}, err
			}
			if err := emit(EventToolStarted, map[string]string{"name": decision.ToolName}); err != nil {
				return RunResult{}, err
			}
			result, err := l.tools.Call(ctx, decision.ToolName, ToolInvocation{RunID: input.RunID, WorkID: input.WorkID, Skill: state.activeSkill, Args: decision.ToolArgs})
			toolCalls++
			if err != nil {
				_ = emit(EventToolFailed, map[string]string{"name": decision.ToolName, "message": err.Error()})
				if errors.Is(err, ErrToolNotAllowed) || errors.Is(err, ErrToolNotFound) {
					return RunResult{}, fmt.Errorf("call tool %q: %w", decision.ToolName, err)
				}
				state.observations = append(state.observations, Observation{Source: decision.ToolName + ".error", Summary: err.Error()})
				continue
			}
			state.observations = append(state.observations, Observation{Source: decision.ToolName, Summary: summarize(result)})
			if err := emit(EventToolCompleted, map[string]string{"name": decision.ToolName, "summary": summarize(result)}); err != nil {
				return RunResult{}, err
			}
		case DecisionAskUser:
			if err := emit(EventApprovalRequired, map[string]any{"question": decision.Question, "reason": decision.Reason, "options": decision.Options, "role": state.role}); err != nil {
				return RunResult{}, err
			}
			return RunResult{}, ErrApprovalRequired
		case DecisionProduceCandidate:
			if state.role != RoleCreator {
				return RunResult{}, errors.New("only creator may produce a collaborative result")
			}
			return l.produceCollaborativeResult(ctx, &state, model, emit, l.budget.MaxModelCalls-modelCalls)
		case DecisionFinish:
			return RunResult{}, errors.New("collaborative roles must use complete_plan or produce_candidate before finishing")
		case DecisionFail:
			return RunResult{}, fmt.Errorf("collaborative agent stopped: %s", decision.Reason)
		default:
			return RunResult{}, fmt.Errorf("unsupported collaborative decision kind %q", decision.Kind)
		}
	}
	return RunResult{}, fmt.Errorf("agent step budget exceeded: %d", l.budget.MaxSteps)
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
	if err := emit(EventGenerationStarted, map[string]any{"mode": "collaborative", "role": RoleCreator, "target": state.input.Target}); err != nil {
		return RunResult{}, err
	}
	var response string
	var err error
	if plan.OutputKind == "proposal" {
		response, err = requestProposalSet(
			ctx,
			model,
			state.input.ModelID,
			canvasContextSystemPrompt(state.activeSkill.Instructions),
			state.candidatePrompt(),
			remainingModelCalls,
			emit,
		)
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
	if err := emit(EventRoleCompleted, map[string]any{"role": RoleCreator}); err != nil {
		return RunResult{}, err
	}
	if !plan.WriterRequired {
		if err := emit(EventMessageDelta, map[string]string{"delta": response}); err != nil {
			return RunResult{}, err
		}
		return l.completeContent(ctx, *state, response, emit)
	}
	return l.polishCollaborativeResult(ctx, *state, response, model, emit)
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

func (l *Loop) polishCollaborativeResult(ctx context.Context, state loopState, draft string, model TextModel, emit Emitter) (RunResult, error) {
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
	return l.completeContent(ctx, state, polished, emit)
}

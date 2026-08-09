package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	maxDecisionAttempts         = 2
	maxDecisionDiagnosticRunes  = 512
	maxDecisionRepairRunes      = 4 * 1024
	canvasContextAccessContract = `# On-Demand Canvas Context

- targetNode and availableContextNodes contain only node IDs and types. No canvas title or content is preloaded.
- priorityContextNodeIds is the user-selected subset of availableContextNodes. Treat it as higher priority, not as mandatory context.
- Use canvas.get_nodes only for targetNode.id or IDs listed in availableContextNodes when their content is relevant to the request.
- Before calling canvas.get_nodes, select the smallest complete set of nodes required for the current task.
- Read that set in as few calls as possible by passing multiple IDs in nodeIds (up to 64 IDs per call). Do not read nodes one at a time by default.
- Split reads only when more than 64 nodes are required. Make another call later only when the previous result reveals a concrete need for additional context.
- If a tool result reports that it was truncated, retry only that batch as smaller non-overlapping batches.
- Never infer a node's content from its ID or type alone.`
	nodeUpdateMergeContract = `# Existing Node Merge Contract

The targetNode object only identifies the existing node being updated.
- Before generating a merge, read targetNode.id with canvas.get_nodes and treat the returned node as the authoritative baseline.
- Preserve its existing title, content, identity, and established facts unless the user's request explicitly changes them.
- Apply the requested changes to the target node instead of generating a replacement concept from related nodes.
- Return the complete merged title and content because the result replaces the stored node fields in full.
- Never silently omit established targetNode details that are unrelated to the requested change.`
	nodeUpdateReplaceContract = `# Existing Node Replacement Contract

The selected target is a storage slot whose previous semantic content may be replaced.
- Create a completely new node concept that follows the user's request and userResponses.
- Do not preserve, continue, paraphrase, or reuse the previous node's title, identity, or content.
- Read related nodes only when their actual content is needed to satisfy the request.
- Return a complete new title and content because the result replaces the stored node fields in full.`
	decisionInstruction = `You are the decision maker inside Warmnote's explicit novel-writing agent loop.
Choose exactly one next action and return exactly one JSON object without markdown or commentary.
The top-level "kind" field is required and must be one of: continue_brainstorm, complete_plan, select_skill, call_tool, ask_user, produce_candidate, finish, fail.
The JSON object uses this shape:
{"kind":"produce_candidate","reason":"optional reason","content":"action content","skillId":"skill-id","toolName":"tool-name","toolArgs":{},"question":"question for user","options":["option"]}
Required fields by kind:
- continue_brainstorm: content
- complete_plan: content
- select_skill: skillId
- call_tool: toolName and toolArgs as a JSON object
- ask_user: question; options is optional
- produce_candidate: no additional fields
- finish: content
- fail: reason
Examples:
{"kind":"select_skill","skillId":"chapter-section-writing"}
{"kind":"produce_candidate"}
Do not nest the decision under "decision", "action", or any other field.
Use produce_candidate when enough context and planning exist to write the requested output.
An interactive follow-up channel is available, but infer ordinary creative details from the request and context; do not choose ask_user for optional details such as names, ages, occupations, locations, or scene styling.
Only choose ask_user when proceeding would necessarily contradict an explicit established fact, not merely because a detail was omitted.
When userResponses is present, treat those answers as authoritative clarification and do not repeat an answered question.
Do not put the final novel prose in this decision response.`
)

type Budget struct {
	MaxSteps      int
	MaxModelCalls int
	MaxToolCalls  int
	MaxDuration   time.Duration
}

func requestDecision(
	ctx context.Context,
	model TextModel,
	modelID string,
	prompt string,
	remainingModelCalls int,
	emit Emitter,
	systemPrompts ...string,
) (Decision, int, error) {
	attempts := min(maxDecisionAttempts, remainingModelCalls)
	if attempts <= 0 {
		return Decision{}, 0, errors.New("model call budget exceeded before agent decision")
	}

	currentPrompt := prompt
	var lastParseErr error
	var lastPreview string
	system := decisionInstruction
	if len(systemPrompts) > 0 && strings.TrimSpace(systemPrompts[0]) != "" {
		system = systemPrompts[0]
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		decisionText, _, err := model.Complete(ctx, ModelRequest{
			ModelID:        modelID,
			System:         system,
			Prompt:         currentPrompt,
			ResponseFormat: ModelResponseFormatJSONObject,
		})
		if err != nil {
			return Decision{}, attempt, fmt.Errorf("request agent decision: %w", err)
		}
		decision, parseErr := parseDecision(decisionText)
		if parseErr == nil {
			return decision, attempt, nil
		}

		lastParseErr = parseErr
		lastPreview = truncateRunes(strings.TrimSpace(decisionText), maxDecisionDiagnosticRunes)
		if err := emit(EventDecisionInvalid, map[string]any{
			"attempt":         attempt,
			"message":         parseErr.Error(),
			"responsePreview": lastPreview,
		}); err != nil {
			return Decision{}, attempt, err
		}
		if attempt < attempts {
			currentPrompt = decisionRepairPrompt(prompt, decisionText, parseErr)
		}
	}
	return Decision{}, attempts, fmt.Errorf(
		"%w after %d attempt(s): %v (response preview: %q)",
		ErrInvalidDecision,
		attempts,
		lastParseErr,
		lastPreview,
	)
}

func decisionRepairPrompt(originalPrompt, invalidResponse string, parseErr error) string {
	payload := map[string]any{
		"task":              "repair_agent_decision",
		"validationError":   parseErr.Error(),
		"invalidResponse":   truncateRunes(strings.TrimSpace(invalidResponse), maxDecisionRepairRunes),
		"originalLoopState": json.RawMessage(originalPrompt),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return originalPrompt
	}
	return string(encoded)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func DefaultBudget() Budget {
	return Budget{MaxSteps: 16, MaxModelCalls: 16, MaxToolCalls: 16, MaxDuration: 3 * time.Minute}
}

type Loop struct {
	skills SkillCatalog
	tools  *ToolRegistry
	budget Budget
}

func NewLoop(skills SkillCatalog, tools *ToolRegistry, budget Budget) *Loop {
	return &Loop{skills: skills, tools: tools, budget: budget}
}

func (l *Loop) Run(ctx context.Context, input RunInput, model TextModel, emit Emitter) (RunResult, error) {
	if model == nil || emit == nil || l.skills == nil || l.tools == nil {
		return RunResult{}, errors.New("agent loop dependencies are not configured")
	}
	if l.budget.MaxSteps <= 0 || l.budget.MaxModelCalls <= 0 || l.budget.MaxDuration <= 0 {
		return RunResult{}, errors.New("invalid agent budget")
	}
	runCtx, cancel := context.WithTimeout(ctx, l.budget.MaxDuration)
	defer cancel()

	nodeCount := len(input.ContextNodes)
	if !IsCollaborativeTarget(input.Target) {
		nodeCount++
	}
	if err := emit(EventContextPreparing, map[string]any{"nodeCount": nodeCount, "mode": "on-demand"}); err != nil {
		return RunResult{}, err
	}
	if err := emit(EventContextReady, map[string]any{"nodeCount": nodeCount, "mode": "on-demand"}); err != nil {
		return RunResult{}, err
	}
	if IsCollaborativeTarget(input.Target) {
		return l.runCollaborative(runCtx, input, model, emit)
	}

	if err := emit(EventSkillSearching, map[string]any{"target": input.Target}); err != nil {
		return RunResult{}, err
	}
	matches, err := l.skills.Search(runCtx, input.Target, input.Prompt)
	if err != nil {
		return RunResult{}, fmt.Errorf("search skills: %w", err)
	}
	if len(matches) == 0 {
		return RunResult{}, fmt.Errorf("no skill supports target %q", input.Target)
	}
	if err := emit(EventSkillMatched, map[string]any{"matches": matches}); err != nil {
		return RunResult{}, err
	}

	state := loopState{input: input, matches: matches}
	if len(matches) == 1 {
		if err := l.selectSkill(runCtx, &state, matches[0].ID, emit); err != nil {
			return RunResult{}, err
		}
	}

	modelCalls := 0
	toolCalls := 0
	for step := 1; step <= l.budget.MaxSteps; step++ {
		if modelCalls >= l.budget.MaxModelCalls {
			return RunResult{}, fmt.Errorf("model call budget exceeded: %d", l.budget.MaxModelCalls)
		}
		decision, calls, err := requestDecision(
			runCtx,
			model,
			input.ModelID,
			state.decisionPrompt(step, l.tools),
			l.budget.MaxModelCalls-modelCalls,
			emit,
		)
		modelCalls += calls
		if err != nil {
			return RunResult{}, err
		}

		switch decision.Kind {
		case DecisionContinueBrainstorm:
			if err := emit(EventBrainstormStarted, map[string]any{"step": step}); err != nil {
				return RunResult{}, err
			}
			state.observations = append(state.observations, Observation{Source: "brainstorm", Summary: decision.Content})
			if err := emit(EventBrainstormCompleted, map[string]string{"summary": decision.Content}); err != nil {
				return RunResult{}, err
			}
		case DecisionCompletePlan:
			if err := emit(EventPlanStarted, map[string]any{"step": step}); err != nil {
				return RunResult{}, err
			}
			state.plan = decision.Content
			if err := emit(EventPlanCompleted, map[string]string{"plan": decision.Content}); err != nil {
				return RunResult{}, err
			}
		case DecisionSelectSkill:
			if err := l.selectSkill(runCtx, &state, decision.SkillID, emit); err != nil {
				return RunResult{}, err
			}
		case DecisionCallTool:
			if state.activeSkill.ID == "" {
				return RunResult{}, errors.New("model requested a tool before selecting a skill")
			}
			if toolCalls >= l.budget.MaxToolCalls {
				return RunResult{}, fmt.Errorf("tool call budget exceeded: %d", l.budget.MaxToolCalls)
			}
			if err := emit(EventToolRequested, map[string]any{"name": decision.ToolName, "arguments": decision.ToolArgs}); err != nil {
				return RunResult{}, err
			}
			if err := emit(EventToolStarted, map[string]string{"name": decision.ToolName}); err != nil {
				return RunResult{}, err
			}
			result, err := l.tools.Call(runCtx, decision.ToolName, ToolInvocation{
				RunID: input.RunID, WorkID: input.WorkID, Skill: state.activeSkill, Args: decision.ToolArgs,
			})
			toolCalls++
			if err != nil {
				_ = emit(EventToolFailed, map[string]string{"name": decision.ToolName, "message": err.Error()})
				if errors.Is(err, ErrToolNotAllowed) || errors.Is(err, ErrToolNotFound) {
					return RunResult{}, fmt.Errorf("call tool %q: %w", decision.ToolName, err)
				}
				state.observations = append(state.observations, Observation{
					Source:  decision.ToolName + ".error",
					Summary: err.Error(),
				})
				continue
			}
			summary := summarize(result)
			state.observations = append(state.observations, Observation{Source: decision.ToolName, Summary: summary})
			if err := emit(EventToolCompleted, map[string]string{"name": decision.ToolName, "summary": summary}); err != nil {
				return RunResult{}, err
			}
		case DecisionAskUser:
			if err := emit(EventApprovalRequired, map[string]any{
				"question": decision.Question, "reason": decision.Reason, "options": decision.Options,
			}); err != nil {
				return RunResult{}, err
			}
			return RunResult{}, ErrApprovalRequired
		case DecisionProduceCandidate:
			if state.activeSkill.ID == "" {
				return RunResult{}, errors.New("model requested candidate production before selecting a skill")
			}
			if modelCalls >= l.budget.MaxModelCalls {
				return RunResult{}, fmt.Errorf("model call budget exceeded: %d", l.budget.MaxModelCalls)
			}
			result, err := l.produceCandidate(runCtx, state, model, emit)
			if err != nil {
				return RunResult{}, err
			}
			return result, nil
		case DecisionFinish:
			if state.activeSkill.ID == "" {
				return RunResult{}, errors.New("model finished before selecting a skill")
			}
			if strings.TrimSpace(decision.Content) == "" {
				return RunResult{}, errors.New("finish decision requires content")
			}
			return l.completeContent(runCtx, state, strings.TrimSpace(decision.Content), emit)
		case DecisionFail:
			return RunResult{}, fmt.Errorf("agent stopped: %s", decision.Reason)
		default:
			return RunResult{}, fmt.Errorf("unsupported decision kind %q", decision.Kind)
		}
	}
	return RunResult{}, fmt.Errorf("agent step budget exceeded: %d", l.budget.MaxSteps)
}

func (l *Loop) selectSkill(ctx context.Context, state *loopState, skillID string, emit Emitter) error {
	if !matchContains(state.matches, skillID) {
		return fmt.Errorf("skill %q was not returned by search", skillID)
	}
	skill, err := l.skills.Load(ctx, skillID)
	if err != nil {
		return fmt.Errorf("load skill %q: %w", skillID, err)
	}
	state.activeSkill = skill
	return emit(EventSkillLoaded, map[string]any{"skillId": skill.ID, "version": skill.Version})
}

func (l *Loop) produceCandidate(ctx context.Context, state loopState, model TextModel, emit Emitter) (RunResult, error) {
	if IsNodeUpdateTarget(state.input.Target) {
		return l.produceNodeUpdate(ctx, state, model, emit)
	}
	if state.input.Target == TargetSectionOutlineBatch || state.input.Target == TargetChapterSection || state.input.Target == TargetChapterArchive {
		return l.produceDerivation(ctx, state, model, emit)
	}
	var content strings.Builder
	_, err := model.Stream(ctx, ModelRequest{
		ModelID: state.input.ModelID,
		System:  canvasContextSystemPrompt(state.activeSkill.Instructions),
		Prompt:  state.candidatePrompt(),
	}, func(delta string) error {
		if delta == "" {
			return nil
		}
		content.WriteString(delta)
		return emit(EventMessageDelta, map[string]string{"delta": delta})
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("produce candidate: %w", err)
	}
	result := strings.TrimSpace(content.String())
	if result == "" {
		return RunResult{}, errors.New("model returned an empty candidate")
	}
	return l.completeContent(ctx, state, result, emit)
}

func (l *Loop) produceDerivation(ctx context.Context, state loopState, model TextModel, emit Emitter) (RunResult, error) {
	if err := emit(EventGenerationStarted, map[string]any{
		"nodeId": state.input.TargetNodeID,
		"mode":   "derive",
		"target": state.input.Target,
	}); err != nil {
		return RunResult{}, err
	}
	response, _, err := model.Complete(ctx, ModelRequest{
		ModelID:        state.input.ModelID,
		System:         canvasContextSystemPrompt(state.activeSkill.Instructions),
		Prompt:         state.candidatePrompt(),
		ResponseFormat: ModelResponseFormatJSONObject,
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("produce node derivation: %w", err)
	}
	response = strings.TrimSpace(response)
	if response == "" {
		return RunResult{}, errors.New("model returned an empty derivation")
	}
	if !json.Valid([]byte(response)) {
		return RunResult{}, errors.New("model returned an invalid derivation JSON object")
	}
	if err := emit(EventMessageDelta, map[string]string{"delta": response}); err != nil {
		return RunResult{}, err
	}
	return l.completeContent(ctx, state, response, emit)
}

func (l *Loop) produceNodeUpdate(ctx context.Context, state loopState, model TextModel, emit Emitter) (RunResult, error) {
	mode := nodeUpdateMode(state.input)
	if err := emit(EventGenerationStarted, map[string]any{"nodeId": state.input.TargetNodeID, "mode": mode}); err != nil {
		return RunResult{}, err
	}
	response, _, err := model.Complete(ctx, ModelRequest{
		ModelID:        state.input.ModelID,
		System:         nodeUpdateSystemPrompt(state.activeSkill.Instructions, mode),
		Prompt:         state.candidatePrompt(),
		ResponseFormat: ModelResponseFormatJSONObject,
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("produce node update: %w", err)
	}
	update, err := parseNodeUpdate(response)
	if err != nil {
		return RunResult{}, err
	}
	if err := emit(EventMessageDelta, map[string]string{"delta": update.Content}); err != nil {
		return RunResult{}, err
	}
	return l.completeContent(ctx, state, response, emit)
}

func (l *Loop) completeContent(ctx context.Context, state loopState, content string, emit Emitter) (RunResult, error) {
	if err := emit(EventSkillCompleted, map[string]any{"skillId": state.activeSkill.ID, "version": state.activeSkill.Version}); err != nil {
		return RunResult{}, err
	}
	if err := emit(EventValidationCompleted, map[string]any{"valid": true}); err != nil {
		return RunResult{}, err
	}
	if IsNodeUpdateTarget(state.input.Target) {
		update, err := parseNodeUpdate(content)
		if err != nil {
			return RunResult{}, err
		}
		revision, err := targetNodeRevision(state)
		if err != nil {
			return RunResult{}, err
		}
		return RunResult{
			Title: update.Title, Content: update.Content,
			Role:    RoleCreator,
			SkillID: state.activeSkill.ID, SkillVersion: state.activeSkill.Version,
			ExpectedRevision: revision,
		}, nil
	}
	if state.input.Target == TargetSectionOutlineBatch || state.input.Target == TargetChapterSection || state.input.Target == TargetChapterArchive {
		revision, err := targetNodeRevision(state)
		if err != nil {
			return RunResult{}, err
		}
		return RunResult{
			Content: content, Role: state.role, SkillID: state.activeSkill.ID,
			SkillVersion: state.activeSkill.Version, ExpectedRevision: revision,
		}, nil
	}
	if IsCollaborativeTarget(state.input.Target) {
		return RunResult{Content: content, Role: state.role, SkillID: state.activeSkill.ID, SkillVersion: state.activeSkill.Version}, nil
	}
	toolArgs, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return RunResult{}, fmt.Errorf("encode candidate tool input: %w", err)
	}
	if err := emit(EventToolRequested, map[string]any{"name": "canvas.create_candidate"}); err != nil {
		return RunResult{}, err
	}
	if err := emit(EventToolStarted, map[string]string{"name": "canvas.create_candidate"}); err != nil {
		return RunResult{}, err
	}
	created, err := l.tools.Call(ctx, "canvas.create_candidate", ToolInvocation{
		RunID: state.input.RunID, WorkID: state.input.WorkID, Skill: state.activeSkill, Args: toolArgs,
	})
	if err != nil {
		_ = emit(EventToolFailed, map[string]string{"name": "canvas.create_candidate", "message": err.Error()})
		return RunResult{}, fmt.Errorf("create candidate: %w", err)
	}
	candidate, ok := created.(Candidate)
	if !ok {
		return RunResult{}, errors.New("canvas.create_candidate returned an invalid result")
	}
	if err := emit(EventToolCompleted, map[string]string{"name": "canvas.create_candidate", "candidateId": candidate.ID}); err != nil {
		return RunResult{}, err
	}
	if err := emit(EventCandidateCreated, map[string]any{
		"candidateId": candidate.ID, "skillId": candidate.SkillID, "skillVersion": candidate.SkillVersion,
	}); err != nil {
		return RunResult{}, err
	}
	return RunResult{
		Content: content, SkillID: state.activeSkill.ID, SkillVersion: state.activeSkill.Version, CandidateID: candidate.ID,
	}, nil
}

type nodeUpdate struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func parseNodeUpdate(value string) (nodeUpdate, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```") {
		if newline := strings.IndexByte(value, '\n'); newline >= 0 {
			value = strings.TrimSpace(value[newline+1:])
		}
		value = strings.TrimSpace(strings.TrimSuffix(value, "```"))
	}
	var update nodeUpdate
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&update); err != nil {
		return nodeUpdate{}, fmt.Errorf("decode node update: %w", err)
	}
	update.Title = strings.TrimSpace(update.Title)
	update.Content = strings.TrimSpace(update.Content)
	if update.Title == "" || update.Content == "" {
		return nodeUpdate{}, errors.New("node update requires title and content")
	}
	return update, nil
}

func targetNodeRevision(state loopState) (int64, error) {
	if state.input.TargetNodeRevision < 1 {
		return 0, errors.New("target node revision is invalid")
	}
	return state.input.TargetNodeRevision, nil
}

type loopState struct {
	input             RunInput
	matches           []SkillMatch
	activeSkill       Skill
	plan              string
	observations      []Observation
	role              AgentRole
	collaborationPlan *CollaborationPlan
}

func (s loopState) decisionPrompt(step int, tools *ToolRegistry) string {
	payload := s.promptPayload()
	payload["step"] = step
	payload["skillMatches"] = s.matches
	if s.role != "" {
		payload["agentRole"] = s.role
	}
	if s.activeSkill.ID != "" {
		payload["activeSkill"] = map[string]any{
			"id": s.activeSkill.ID, "version": s.activeSkill.Version,
			"instructions": s.activeSkill.Instructions, "tools": tools.Specs(s.activeSkill.AllowedTools),
		}
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func (s loopState) candidatePrompt() string {
	payload := s.promptPayload()
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func (s loopState) promptPayload() map[string]any {
	payload := map[string]any{
		"request": s.input.Prompt, "target": s.input.Target,
		"plan": s.plan, "observations": s.observations,
	}
	if len(s.input.UserResponses) > 0 {
		payload["userResponses"] = s.input.UserResponses
	}
	if IsCollaborativeTarget(s.input.Target) {
		collaborativeCandidates := s.input.CollaborativeCandidates
		if collaborativeCandidates == nil {
			collaborativeCandidates = []CollaborativeCandidate{}
		}
		payload["collaborativeCandidates"] = collaborativeCandidates
		payload["operation"] = "targeted_creation"
		if s.input.Target == TargetCollaborativeExplore {
			payload["operation"] = "divergent_exploration"
		}
		if s.role != "" {
			payload["agentRole"] = s.role
		}
		if s.collaborationPlan != nil {
			payload["collaborationPlan"] = s.collaborationPlan
		}
		availableContextNodes := s.input.ContextNodes
		if availableContextNodes == nil {
			availableContextNodes = []NodeReference{}
		}
		payload["availableContextNodes"] = availableContextNodes
		payload["contextAccessPolicy"] = []string{
			"Use canvas.search_context to discover candidates from the local graph/vector index when available.",
			"Read authoritative content with canvas.get_nodes only after selecting candidate IDs.",
			"Batch selected IDs into calls of at most 64 nodes.",
		}
		return payload
	}
	if !IsNodeUpdateTarget(s.input.Target) &&
		s.input.Target != TargetSectionOutlineBatch &&
		s.input.Target != TargetChapterSection &&
		s.input.Target != TargetChapterArchive {
		return payload
	}

	payload["targetNodeId"] = s.input.TargetNodeID
	payload["targetNode"] = NodeReference{ID: s.input.TargetNodeID, Type: s.input.TargetNodeType}
	availableContextNodes := s.input.ContextNodes
	if availableContextNodes == nil {
		availableContextNodes = []NodeReference{}
	}
	payload["availableContextNodes"] = availableContextNodes
	payload["contextAccessPolicy"] = []string{
		"Node titles and content are not preloaded into this request.",
		"targetNode and availableContextNodes contain only ID and type metadata.",
		"Select the smallest complete set of relevant nodes before reading their content.",
		"Pass all selected IDs to canvas.get_nodes together, with at most 64 IDs per call; do not read nodes one at a time by default.",
		"Split reads only above the per-call limit, or when a completed read reveals a concrete need for additional context.",
		"If a tool result is marked truncated, retry only that batch as smaller non-overlapping batches.",
	}
	if s.input.Target == TargetSectionOutlineBatch || s.input.Target == TargetChapterSection || s.input.Target == TargetChapterArchive {
		payload["operation"] = "derive_child_nodes"
		if s.input.Target == TargetChapterArchive {
			payload["operation"] = "archive_chapter_and_propose_entity_versions"
		}
		return payload
	}

	mode := nodeUpdateMode(s.input)
	payload["operation"] = "update_existing_node"
	payload["mutationMode"] = mode
	if priorityContextNodeIDs := s.priorityContextNodeIDs(); len(priorityContextNodeIDs) > 0 {
		payload["priorityContextNodeIds"] = priorityContextNodeIDs
	}
	if mode == "replace" {
		payload["updatePolicy"] = []string{
			"Replace the target node's previous semantic content with a completely new concept.",
			"Do not reuse the previous title, identity, or content.",
			"Return the complete new title and content for full-field replacement.",
		}
		return payload
	}
	payload["updatePolicy"] = []string{
		"Read targetNode before using it as the authoritative existing baseline.",
		"Preserve details not explicitly changed by the request.",
		"Return the complete merged title and content for full-field replacement.",
	}
	return payload
}

func (s loopState) priorityContextNodeIDs() []string {
	if !IsNodeUpdateTarget(s.input.Target) {
		return nil
	}
	availableNodeIDs := make(map[string]struct{}, len(s.input.ContextNodes))
	for _, node := range s.input.ContextNodes {
		availableNodeIDs[node.ID] = struct{}{}
	}
	priorityNodeIDs := make([]string, 0, len(s.input.ContextNodeIDs))
	seenNodeIDs := make(map[string]struct{}, len(s.input.ContextNodeIDs))
	for _, nodeID := range s.input.ContextNodeIDs {
		if nodeID == s.input.TargetNodeID {
			continue
		}
		if _, available := availableNodeIDs[nodeID]; !available {
			continue
		}
		if _, seen := seenNodeIDs[nodeID]; seen {
			continue
		}
		seenNodeIDs[nodeID] = struct{}{}
		priorityNodeIDs = append(priorityNodeIDs, nodeID)
	}
	return priorityNodeIDs
}

func nodeUpdateSystemPrompt(skillInstructions, mode string) string {
	contract := nodeUpdateMergeContract
	if mode == "replace" {
		contract = nodeUpdateReplaceContract
	}
	return canvasContextSystemPrompt(skillInstructions) + "\n\n" + contract
}

func canvasContextSystemPrompt(skillInstructions string) string {
	return strings.TrimSpace(skillInstructions) + "\n\n" + canvasContextAccessContract
}

func nodeUpdateMode(input RunInput) string {
	if requestsNodeReplacement(input.Prompt, strings.TrimPrefix(input.Target, TargetNodeUpdate+":")) {
		return "replace"
	}
	return "merge"
}

func requestsNodeReplacement(prompt, nodeKind string) bool {
	normalized := strings.ToLower(strings.TrimSpace(prompt))
	for _, phrase := range []string{
		"替换这个节点", "替换当前节点", "重做这个节点", "重写这个节点", "清空重写", "完全重写",
		"replace this node", "rewrite this node", "start this node over",
	} {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	phrasesByKind := map[string][]string{
		"character": {"新角色", "新人物", "另一个角色", "另一个人物", "创建角色", "创建一个角色", "new character", "another character", "create a character"},
		"world":     {"新世界观", "另一个世界观", "创建世界观", "new world", "create a world"},
		"location":  {"新地点", "新场景", "另一个地点", "创建地点", "new location", "create a location"},
		"event":     {"新事件", "另一个事件", "创建事件", "new event", "create an event"},
		"mechanism": {"新机制", "另一个机制", "创建机制", "new mechanism", "create a mechanism"},
		"chapter-outline": {
			"新章节", "新的章节", "下一章", "下一个章节", "续写新章", "续写下一章",
			"新大纲", "新的章节概览", "创建章节", "创建大纲", "new chapter", "next chapter", "new outline",
		},
	}
	for _, phrase := range phrasesByKind[nodeKind] {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	return false
}

func parseDecision(value string) (Decision, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	value = strings.TrimSpace(value)
	start, end := strings.IndexByte(value, '{'), strings.LastIndexByte(value, '}')
	if start < 0 || end < start {
		return Decision{}, errors.New("decision is not a JSON object")
	}
	var decision Decision
	if err := json.Unmarshal([]byte(value[start:end+1]), &decision); err != nil {
		return Decision{}, err
	}
	decision.Kind = DecisionKind(strings.TrimSpace(string(decision.Kind)))
	if decision.Kind == "" {
		return Decision{}, errors.New("decision kind is required")
	}
	decision.Reason = strings.TrimSpace(decision.Reason)
	decision.SkillID = strings.TrimSpace(decision.SkillID)
	decision.ToolName = strings.TrimSpace(decision.ToolName)
	decision.Question = strings.TrimSpace(decision.Question)
	if err := validateDecision(decision); err != nil {
		return Decision{}, err
	}
	return decision, nil
}

func matchContains(matches []SkillMatch, skillID string) bool {
	for _, match := range matches {
		if match.ID == skillID {
			return true
		}
	}
	return false
}

func summarize(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	// One batched read replaces up to 16 former 8 KiB observations while keeping
	// the decision prompt bounded. An explicit marker lets the model split only
	// an oversized batch instead of silently reasoning from incomplete context.
	const maxSummaryBytes = 128 * 1024
	if len(encoded) > maxSummaryBytes {
		return strings.ToValidUTF8(string(encoded[:maxSummaryBytes]), "") +
			"\n[tool result truncated; retry this batch as smaller non-overlapping batches]"
	}
	return string(encoded)
}

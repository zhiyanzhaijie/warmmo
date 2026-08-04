package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	maxDecisionAttempts        = 2
	maxDecisionDiagnosticRunes = 512
	maxDecisionRepairRunes     = 4 * 1024
	decisionInstruction        = `You are the decision maker inside Warmnote's explicit novel-writing agent loop.
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
{"kind":"select_skill","skillId":"chapter-drafting"}
{"kind":"produce_candidate"}
Do not nest the decision under "decision", "action", or any other field.
Use produce_candidate when enough context and planning exist to write the requested output.
This is a single-request API without an interactive follow-up channel. Infer ordinary creative details from the request and context; do not choose ask_user for optional details such as names, ages, occupations, locations, or scene styling.
Only choose ask_user when proceeding would necessarily contradict an explicit established fact, not merely because a detail was omitted.
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
) (Decision, int, error) {
	attempts := min(maxDecisionAttempts, remainingModelCalls)
	if attempts <= 0 {
		return Decision{}, 0, errors.New("model call budget exceeded before agent decision")
	}

	currentPrompt := prompt
	var lastParseErr error
	var lastPreview string
	for attempt := 1; attempt <= attempts; attempt++ {
		decisionText, _, err := model.Complete(ctx, ModelRequest{
			ModelID:        modelID,
			System:         decisionInstruction,
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
	return Budget{MaxSteps: 8, MaxModelCalls: 8, MaxToolCalls: 4, MaxDuration: 3 * time.Minute}
}

type Loop struct {
	contextReader ContextReader
	skills        SkillCatalog
	tools         *ToolRegistry
	budget        Budget
}

func NewLoop(contextReader ContextReader, skills SkillCatalog, tools *ToolRegistry, budget Budget) *Loop {
	return &Loop{contextReader: contextReader, skills: skills, tools: tools, budget: budget}
}

func (l *Loop) Run(ctx context.Context, input RunInput, model TextModel, emit Emitter) (RunResult, error) {
	if model == nil || emit == nil || l.contextReader == nil || l.skills == nil || l.tools == nil {
		return RunResult{}, errors.New("agent loop dependencies are not configured")
	}
	if l.budget.MaxSteps <= 0 || l.budget.MaxModelCalls <= 0 || l.budget.MaxDuration <= 0 {
		return RunResult{}, errors.New("invalid agent budget")
	}
	runCtx, cancel := context.WithTimeout(ctx, l.budget.MaxDuration)
	defer cancel()

	if err := emit(EventContextPreparing, map[string]any{"nodeCount": len(input.ContextNodeIDs)}); err != nil {
		return RunResult{}, err
	}
	snapshot, err := l.contextReader.BuildSnapshot(runCtx, input.WorkID, input.ContextNodeIDs)
	if err != nil {
		return RunResult{}, fmt.Errorf("build canvas context: %w", err)
	}
	if err := emit(EventContextReady, map[string]any{"snapshotId": snapshot.ID, "nodeCount": len(snapshot.Nodes)}); err != nil {
		return RunResult{}, err
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

	state := loopState{input: input, snapshot: snapshot, matches: matches}
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
				return RunResult{}, fmt.Errorf("call tool %q: %w", decision.ToolName, err)
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
	var content strings.Builder
	_, err := model.Stream(ctx, ModelRequest{
		ModelID: state.input.ModelID,
		System:  state.activeSkill.Instructions,
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

func (l *Loop) produceNodeUpdate(ctx context.Context, state loopState, model TextModel, emit Emitter) (RunResult, error) {
	response, _, err := model.Complete(ctx, ModelRequest{
		ModelID: state.input.ModelID,
		System:  state.activeSkill.Instructions,
		Prompt:  state.candidatePrompt(),
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
			SkillID: state.activeSkill.ID, SkillVersion: state.activeSkill.Version,
			ExpectedRevision: revision,
		}, nil
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
	for _, node := range state.snapshot.Nodes {
		if node.ID != state.input.TargetNodeID {
			continue
		}
		revision, err := strconv.ParseInt(node.Revision, 10, 64)
		if err != nil || revision < 1 {
			return 0, errors.New("target node revision is invalid")
		}
		return revision, nil
	}
	return 0, fmt.Errorf("target node %q is not in context", state.input.TargetNodeID)
}

type loopState struct {
	input        RunInput
	snapshot     ContextSnapshot
	matches      []SkillMatch
	activeSkill  Skill
	plan         string
	observations []Observation
}

func (s loopState) decisionPrompt(step int, tools *ToolRegistry) string {
	payload := map[string]any{
		"step": step, "request": s.input.Prompt, "target": s.input.Target,
		"context": s.snapshot, "skillMatches": s.matches, "plan": s.plan, "observations": s.observations,
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
	payload := map[string]any{
		"request": s.input.Prompt, "target": s.input.Target, "context": s.snapshot,
		"plan": s.plan, "observations": s.observations,
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
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
	const maxSummaryBytes = 8 * 1024
	if len(encoded) > maxSummaryBytes {
		return string(encoded[:maxSummaryBytes]) + "..."
	}
	return string(encoded)
}

type PromptOnlyContextReader struct{}

func (PromptOnlyContextReader) BuildSnapshot(_ context.Context, workID string, nodeIDs []string) (ContextSnapshot, error) {
	if len(nodeIDs) > 0 {
		return ContextSnapshot{}, ErrCanvasUnavailable
	}
	return ContextSnapshot{ID: "prompt-only", WorkID: workID}, nil
}

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const decisionInstruction = `You are the decision maker inside Warmnote's explicit novel-writing agent loop.
Choose exactly one next action and return one JSON object without markdown.
Allowed kinds: continue_brainstorm, complete_plan, select_skill, call_tool, ask_user, produce_candidate, finish, fail.
Use select_skill with skillId, call_tool with toolName and toolArgs, ask_user with question and optional options.
Use produce_candidate when enough context and planning exist to write the requested output.
Do not put the final novel prose in this decision response.`

type Budget struct {
	MaxSteps      int
	MaxModelCalls int
	MaxToolCalls  int
	MaxDuration   time.Duration
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
		decisionText, _, err := model.Complete(runCtx, ModelRequest{
			ModelID: input.ModelID,
			System:  decisionInstruction,
			Prompt:  state.decisionPrompt(step, l.tools),
		})
		modelCalls++
		if err != nil {
			return RunResult{}, fmt.Errorf("request agent decision: %w", err)
		}
		decision, err := parseDecision(decisionText)
		if err != nil {
			return RunResult{}, fmt.Errorf("parse agent decision: %w", err)
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
			return l.createCandidate(runCtx, state, strings.TrimSpace(decision.Content), emit)
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
	return l.createCandidate(ctx, state, result, emit)
}

func (l *Loop) createCandidate(ctx context.Context, state loopState, content string, emit Emitter) (RunResult, error) {
	if err := emit(EventSkillCompleted, map[string]any{"skillId": state.activeSkill.ID, "version": state.activeSkill.Version}); err != nil {
		return RunResult{}, err
	}
	if err := emit(EventValidationCompleted, map[string]any{"valid": true}); err != nil {
		return RunResult{}, err
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
	if decision.Kind == "" {
		return Decision{}, errors.New("decision kind is required")
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

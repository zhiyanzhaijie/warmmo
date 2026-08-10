package writing

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type DecisionKind string

const (
	DecisionContinueBrainstorm DecisionKind = "continue_brainstorm"
	DecisionCompletePlan       DecisionKind = "complete_plan"
	DecisionSelectSkill        DecisionKind = "select_skill"
	DecisionCallTool           DecisionKind = "call_tool"
	DecisionAskUser            DecisionKind = "ask_user"
	DecisionProduceCandidate   DecisionKind = "produce_candidate"
	DecisionFinish             DecisionKind = "finish"
	DecisionFail               DecisionKind = "fail"
)

type Decision struct {
	Kind     DecisionKind    `json:"kind"`
	Reason   string          `json:"reason,omitempty"`
	Content  string          `json:"content,omitempty"`
	SkillID  string          `json:"skillId,omitempty"`
	ToolName string          `json:"toolName,omitempty"`
	ToolArgs json.RawMessage `json:"toolArgs,omitempty"`
	Question string          `json:"question,omitempty"`
	Options  []string        `json:"options,omitempty"`
}

type Observation struct {
	Source  string `json:"source"`
	Summary string `json:"summary"`
}

func validateDecision(decision Decision) error {
	switch decision.Kind {
	case DecisionContinueBrainstorm:
		return requireDecisionText("continue_brainstorm", "content", decision.Content)
	case DecisionCompletePlan:
		return requireDecisionText("complete_plan", "content", decision.Content)
	case DecisionSelectSkill:
		return requireDecisionText("select_skill", "skillId", decision.SkillID)
	case DecisionCallTool:
		if err := requireDecisionText("call_tool", "toolName", decision.ToolName); err != nil {
			return err
		}
		if len(decision.ToolArgs) == 0 {
			return errors.New("call_tool decision requires toolArgs")
		}
		var toolArgs map[string]json.RawMessage
		if err := json.Unmarshal(decision.ToolArgs, &toolArgs); err != nil || toolArgs == nil {
			return errors.New("call_tool decision requires toolArgs to be a JSON object")
		}
		return nil
	case DecisionAskUser:
		return requireDecisionText("ask_user", "question", decision.Question)
	case DecisionProduceCandidate:
		return nil
	case DecisionFinish:
		return requireDecisionText("finish", "content", decision.Content)
	case DecisionFail:
		return requireDecisionText("fail", "reason", decision.Reason)
	default:
		return fmt.Errorf("unsupported decision kind %q", decision.Kind)
	}
}

func requireDecisionText(kind, field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s decision requires %s", kind, field)
	}
	return nil
}

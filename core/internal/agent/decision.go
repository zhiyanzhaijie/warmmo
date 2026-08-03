package agent

import (
	"encoding/json"
	"errors"
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

var ErrApprovalRequired = errors.New("agent requires user input")

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

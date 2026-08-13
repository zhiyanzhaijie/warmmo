package adk

import (
	"context"
	"encoding/json"

	appharness "warmmo/core/internal/application/harness"
)

const AskUserToolName = "ask_user"

type askUserTool struct{}

func (askUserTool) Spec() appharness.ToolSpec {
	return appharness.ToolSpec{
		Name:        AskUserToolName,
		Description: "Pause this turn and ask the user one blocking question. Use only when proceeding would violate an explicit constraint or unresolved contradiction.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{"type": "string", "minLength": 1},
				"options": map[string]any{
					"type": "array", "items": map[string]any{"type": "string", "minLength": 1}, "maxItems": 8,
				},
			},
			"required": []string{"question"}, "additionalProperties": false,
		},
		OutputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"status": map[string]any{"type": "string"}},
			"required":   []string{"status"}, "additionalProperties": false,
		},
		SideEffect: appharness.SideEffectNone, Approval: appharness.ApprovalNever,
		MaxResultBytes: 1024, LongRunning: true, ModelCallable: true,
	}
}

func (askUserTool) Call(context.Context, appharness.ToolInvocation) (any, error) {
	return map[string]any{"status": "awaiting_user"}, nil
}

func askUserResponse(answer string) map[string]any {
	return map[string]any{"answer": answer}
}

func pendingQuestion(payload json.RawMessage) string {
	var args struct {
		Question string `json:"question"`
	}
	_ = json.Unmarshal(payload, &args)
	return args.Question
}

var _ appharness.Tool = askUserTool{}

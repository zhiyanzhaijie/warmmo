package writing

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		wantKind  DecisionKind
		wantError string
	}{
		{name: "produce candidate", value: `{"kind":"produce_candidate"}`, wantKind: DecisionProduceCandidate},
		{name: "fenced selection", value: "```json\n{\"kind\":\"select_skill\",\"skillId\":\"chapter-section-writing\"}\n```", wantKind: DecisionSelectSkill},
		{name: "tool call", value: `{"kind":"call_tool","toolName":"canvas.get_nodes","toolArgs":{"nodeIds":["node-1"]}}`, wantKind: DecisionCallTool},
		{name: "missing kind", value: `{"action":"produce_candidate"}`, wantError: "decision kind is required"},
		{name: "unknown kind", value: `{"kind":"write_chapter"}`, wantError: `unsupported decision kind "write_chapter"`},
		{name: "brainstorm content required", value: `{"kind":"continue_brainstorm"}`, wantError: "continue_brainstorm decision requires content"},
		{name: "skill id required", value: `{"kind":"select_skill"}`, wantError: "select_skill decision requires skillId"},
		{name: "tool args must be object", value: `{"kind":"call_tool","toolName":"canvas.get_nodes","toolArgs":[]}`, wantError: "call_tool decision requires toolArgs to be a JSON object"},
		{name: "question required", value: `{"kind":"ask_user"}`, wantError: "ask_user decision requires question"},
		{name: "finish content required", value: `{"kind":"finish"}`, wantError: "finish decision requires content"},
		{name: "failure reason required", value: `{"kind":"fail"}`, wantError: "fail decision requires reason"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decision, err := parseDecision(test.value)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("parseDecision() error = %v, want containing %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDecision() error = %v", err)
			}
			if decision.Kind != test.wantKind {
				t.Fatalf("parseDecision() kind = %q, want %q", decision.Kind, test.wantKind)
			}
		})
	}
}

func TestRequestDecisionRepairsInvalidResponse(t *testing.T) {
	t.Parallel()

	model := &scriptedTextModel{
		responses: []string{
			`{"action":"produce_candidate"}`,
			`{"kind":"produce_candidate"}`,
		},
	}
	var events []recordedEvent
	decision, calls, err := requestDecision(
		context.Background(),
		model,
		"deepseek-reasoner",
		`{"step":1}`,
		2,
		func(eventType EventType, data any) error {
			events = append(events, recordedEvent{eventType: eventType, data: data})
			return nil
		},
	)
	if err != nil {
		t.Fatalf("requestDecision() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("requestDecision() calls = %d, want 2", calls)
	}
	if decision.Kind != DecisionProduceCandidate {
		t.Fatalf("requestDecision() kind = %q, want %q", decision.Kind, DecisionProduceCandidate)
	}
	if len(model.requests) != 2 {
		t.Fatalf("model requests = %d, want 2", len(model.requests))
	}
	for index, request := range model.requests {
		if request.ResponseFormat != ModelResponseFormatJSONObject {
			t.Errorf("request %d response format = %q, want %q", index, request.ResponseFormat, ModelResponseFormatJSONObject)
		}
	}
	var repairPrompt map[string]any
	if err := json.Unmarshal([]byte(model.requests[1].Prompt), &repairPrompt); err != nil {
		t.Fatalf("decode repair prompt: %v", err)
	}
	if repairPrompt["task"] != "repair_agent_decision" {
		t.Errorf("repair task = %v", repairPrompt["task"])
	}
	if len(events) != 1 || events[0].eventType != EventDecisionInvalid {
		t.Fatalf("events = %#v, want one decision.invalid event", events)
	}
}

func TestRequestDecisionReturnsBoundedDiagnostics(t *testing.T) {
	t.Parallel()

	invalidResponse := strings.Repeat("界", maxDecisionDiagnosticRunes+100) + "{}"
	model := &scriptedTextModel{responses: []string{invalidResponse}}
	var diagnostic map[string]any
	_, calls, err := requestDecision(
		context.Background(),
		model,
		"deepseek-reasoner",
		`{"step":1}`,
		1,
		func(_ EventType, data any) error {
			diagnostic = data.(map[string]any)
			return nil
		},
	)
	if err == nil || !errors.Is(err, ErrInvalidDecision) || !strings.Contains(err.Error(), "after 1 attempt") {
		t.Fatalf("requestDecision() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("requestDecision() calls = %d, want 1", calls)
	}
	preview, ok := diagnostic["responsePreview"].(string)
	if !ok {
		t.Fatalf("responsePreview = %#v", diagnostic["responsePreview"])
	}
	if len([]rune(preview)) != maxDecisionDiagnosticRunes+3 {
		t.Fatalf("response preview rune length = %d, want %d", len([]rune(preview)), maxDecisionDiagnosticRunes+3)
	}
}

type scriptedTextModel struct {
	responses []string
	requests  []ModelRequest
}

func (m *scriptedTextModel) Complete(_ context.Context, request ModelRequest) (string, ModelUsage, error) {
	m.requests = append(m.requests, request)
	if len(m.responses) == 0 {
		return "", ModelUsage{}, errors.New("no scripted response")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, ModelUsage{}, nil
}

func (*scriptedTextModel) Stream(context.Context, ModelRequest, func(string) error) (ModelUsage, error) {
	return ModelUsage{}, errors.New("unexpected stream call")
}

type recordedEvent struct {
	eventType EventType
	data      any
}

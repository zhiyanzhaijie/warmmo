package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	agentcore "warmmo/core/internal/agent/core"
)

func TestCompletePropagatesJSONResponseFormat(t *testing.T) {
	t.Parallel()

	requests := make(chan chatRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body chatRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		requests <- body
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"kind\\\":\\\"produce_candidate\\\"}\"}}]}\n\n")
		fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	model := &adkTextModel{
		llm: newCompatibleLLM(ModelConfig{
			BaseURL: server.URL,
			APIKey:  "test-key",
			ModelID: "deepseek-reasoner",
		}, server.Client()),
	}
	content, _, err := model.Complete(context.Background(), agentcore.ModelRequest{
		ModelID:        "deepseek-reasoner",
		System:         "Return JSON.",
		Prompt:         "{}",
		ResponseFormat: agentcore.ModelResponseFormatJSONObject,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if content != `{"kind":"produce_candidate"}` {
		t.Fatalf("Complete() content = %q", content)
	}

	request := <-requests
	if request.ResponseFormat == nil || request.ResponseFormat.Type != "json_object" {
		t.Fatalf("response_format = %#v, want json_object", request.ResponseFormat)
	}
	if !request.Stream {
		t.Fatal("stream = false, want true")
	}
}

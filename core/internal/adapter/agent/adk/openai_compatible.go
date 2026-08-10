package adk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"

	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"

	agentcore "warmmo/core/internal/adapter/agent/core"
)

type ModelConfig struct {
	BaseURL string
	APIKey  string
	ModelID string
}

type compatibleLLM struct {
	config ModelConfig
	client *http.Client
}

type chatResponseFormat struct {
	Type string `json:"type"`
}

func newCompatibleLLM(config ModelConfig, client *http.Client) adkmodel.LLM {
	return &compatibleLLM{config: config, client: client}
}

func (m *compatibleLLM) Name() string {
	return m.config.ModelID
}

func (m *compatibleLLM) GenerateContent(ctx context.Context, request *adkmodel.LLMRequest, stream bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		if !stream {
			yield(nil, errors.New("Warmmo requires streaming model calls"))
			return
		}
		messages := make([]chatMessage, 0, len(request.Contents))
		for index, content := range request.Contents {
			if content == nil {
				continue
			}
			var text strings.Builder
			for _, part := range content.Parts {
				text.WriteString(part.Text)
			}
			if text.Len() > 0 {
				role := string(content.Role)
				if index == 0 {
					role = "system"
				}
				messages = append(messages, chatMessage{Role: role, Content: text.String()})
			}
		}
		chatRequest := chatRequest{Model: m.config.ModelID, Messages: messages, Stream: true}
		if request.Config != nil && request.Config.ResponseMIMEType == "application/json" {
			chatRequest.ResponseFormat = &chatResponseFormat{Type: "json_object"}
		}
		payload, err := json.Marshal(chatRequest)
		if err != nil {
			yield(nil, fmt.Errorf("encode chat request: %w", err))
			return
		}
		httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(m.config.BaseURL, "/")+"/chat/completions", bytes.NewReader(payload))
		if err != nil {
			yield(nil, fmt.Errorf("create chat request: %w", err))
			return
		}
		httpRequest.Header.Set("Authorization", "Bearer "+m.config.APIKey)
		httpRequest.Header.Set("Content-Type", "application/json")
		httpRequest.Header.Set("Accept", "text/event-stream")

		response, err := m.client.Do(httpRequest)
		if err != nil {
			yield(nil, fmt.Errorf("call model provider: %w", err))
			return
		}
		defer response.Body.Close()
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
			yield(nil, fmt.Errorf("model provider returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body))))
			return
		}

		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				return
			}
			var chunk chatChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				yield(nil, fmt.Errorf("decode model stream: %w", err))
				return
			}
			if chunk.Error != nil {
				yield(nil, errors.New(chunk.Error.Message))
				return
			}
			for _, choice := range chunk.Choices {
				if choice.Delta.Content == "" {
					continue
				}
				if !yield(&adkmodel.LLMResponse{
					Content: genai.NewContentFromText(choice.Delta.Content, genai.RoleModel),
					Partial: true,
				}, nil) {
					return
				}
			}
		}
		if err := scanner.Err(); err != nil {
			yield(nil, fmt.Errorf("read model stream: %w", err))
		}
	}
}

type adkTextModel struct {
	llm adkmodel.LLM
}

func NewTextModel(config ModelConfig, client *http.Client) agentcore.TextModel {
	return &adkTextModel{llm: newCompatibleLLM(config, client)}
}

func (m *adkTextModel) Complete(ctx context.Context, request agentcore.ModelRequest) (string, agentcore.ModelUsage, error) {
	var content strings.Builder
	usage, err := m.Stream(ctx, request, func(delta string) error {
		content.WriteString(delta)
		return nil
	})
	if err != nil {
		return "", agentcore.ModelUsage{}, err
	}
	return content.String(), usage, nil
}

func (m *adkTextModel) Stream(ctx context.Context, request agentcore.ModelRequest, onDelta func(string) error) (agentcore.ModelUsage, error) {
	contents := []*genai.Content{
		genai.NewContentFromText(request.System, genai.RoleUser),
		genai.NewContentFromText(request.Prompt, genai.RoleUser),
	}
	llmRequest := &adkmodel.LLMRequest{Model: request.ModelID, Contents: contents}
	if request.ResponseFormat == agentcore.ModelResponseFormatJSONObject {
		llmRequest.Config = &genai.GenerateContentConfig{ResponseMIMEType: "application/json"}
	}
	for response, err := range m.llm.GenerateContent(ctx, llmRequest, true) {
		if err != nil {
			return agentcore.ModelUsage{}, err
		}
		if response == nil || response.Content == nil {
			continue
		}
		for _, part := range response.Content.Parts {
			if part.Text != "" {
				if err := onDelta(part.Text); err != nil {
					return agentcore.ModelUsage{}, err
				}
			}
		}
	}
	return agentcore.ModelUsage{}, nil
}

type chatRequest struct {
	Model          string              `json:"model"`
	Messages       []chatMessage       `json:"messages"`
	Stream         bool                `json:"stream"`
	ResponseFormat *chatResponseFormat `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

package adk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"

	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"
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

// toolNameCodec keeps internal tool names stable while adapting them to the
// restricted function-name grammar used by OpenAI-compatible providers.
type toolNameCodec struct {
	internalToExternal map[string]string
	externalToInternal map[string]string
}

func newToolNameCodec(tools []*genai.Tool) (toolNameCodec, error) {
	codec := toolNameCodec{
		internalToExternal: make(map[string]string),
		externalToInternal: make(map[string]string),
	}
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		if len(tool.FunctionDeclarations) == 0 {
			return toolNameCodec{}, fmt.Errorf("OpenAI-compatible model only supports function tools")
		}
		for _, declaration := range tool.FunctionDeclarations {
			if declaration == nil || strings.TrimSpace(declaration.Name) == "" {
				return toolNameCodec{}, fmt.Errorf("function tool name is required")
			}
			name := declaration.Name
			if _, exists := codec.internalToExternal[name]; exists {
				return toolNameCodec{}, fmt.Errorf("duplicate function tool %q", name)
			}
			external := providerToolName(name)
			if previous, exists := codec.externalToInternal[external]; exists {
				return toolNameCodec{}, fmt.Errorf("function tool names %q and %q encode to the same provider name %q", previous, name, external)
			}
			codec.internalToExternal[name] = external
			codec.externalToInternal[external] = name
		}
	}
	return codec, nil
}

func firstToolNameCodec(codecs []toolNameCodec) toolNameCodec {
	if len(codecs) == 0 {
		return toolNameCodec{}
	}
	return codecs[0]
}

func (c toolNameCodec) external(name string) (string, error) {
	if external, ok := c.internalToExternal[name]; ok {
		return external, nil
	}
	if len(c.internalToExternal) == 0 && isProviderToolName(name) {
		return name, nil
	}
	return "", fmt.Errorf("function tool %q is not registered", name)
}

func (c toolNameCodec) internal(name string) (string, error) {
	if internal, ok := c.externalToInternal[name]; ok {
		return internal, nil
	}
	if internal, ok := c.legacyInternal(name); ok {
		return internal, nil
	}
	if len(c.externalToInternal) == 0 && isProviderToolName(name) {
		return name, nil
	}
	return "", fmt.Errorf("model returned unknown function tool %q", name)
}

func (c toolNameCodec) legacyInternal(name string) (string, bool) {
	const legacyPrefix = "warmmo__"
	legacyName := strings.TrimPrefix(name, legacyPrefix)
	if legacyName == name || legacyName == "" {
		return "", false
	}
	if decoded, err := hex.DecodeString(legacyName); err == nil {
		internal := string(decoded)
		if _, registered := c.internalToExternal[internal]; registered {
			return internal, true
		}
	}
	if internal, registered := c.externalToInternal[legacyName]; registered {
		return internal, true
	}
	return "", false
}

func isProviderToolName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range []byte(name) {
		if isProviderToolNameChar(char) {
			continue
		}
		return false
	}
	return true
}

func providerToolName(name string) string {
	if isProviderToolName(name) {
		return name
	}
	var encoded strings.Builder
	encoded.Grow(len(name))
	for _, char := range []byte(name) {
		if isProviderToolNameChar(char) {
			encoded.WriteByte(char)
		} else {
			encoded.WriteByte('_')
		}
	}
	return encoded.String()
}

func isProviderToolNameChar(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9') || char == '_' || char == '-'
}

func NewLLM(config ModelConfig, client *http.Client) adkmodel.LLM {
	if client == nil {
		client = http.DefaultClient
	}
	return &compatibleLLM{config: config, client: client}
}

func newCompatibleLLM(config ModelConfig, client *http.Client) adkmodel.LLM {
	return NewLLM(config, client)
}

func (m *compatibleLLM) Name() string {
	return m.config.ModelID
}

func (m *compatibleLLM) GenerateContent(ctx context.Context, request *adkmodel.LLMRequest, stream bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		chatRequest, err := buildChatRequest(request, m.config.ModelID, stream)
		if err != nil {
			yield(nil, err)
			return
		}
		payload, err := json.Marshal(chatRequest)
		if err != nil {
			yield(nil, fmt.Errorf("encode chat request: %w", err))
			return
		}
		httpRequest, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			strings.TrimRight(m.config.BaseURL, "/")+"/chat/completions",
			bytes.NewReader(payload),
		)
		if err != nil {
			yield(nil, fmt.Errorf("create chat request: %w", err))
			return
		}
		if m.config.APIKey != "" {
			httpRequest.Header.Set("Authorization", "Bearer "+m.config.APIKey)
		}
		httpRequest.Header.Set("Content-Type", "application/json")
		if stream {
			httpRequest.Header.Set("Accept", "text/event-stream")
		}

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
		if stream {
			streamChatResponse(response.Body, yield, chatRequest.codec)
			return
		}
		completeChatResponse(response.Body, yield, chatRequest.codec)
	}
}

func buildChatRequest(request *adkmodel.LLMRequest, fallbackModel string, stream bool) (chatRequest, error) {
	if request == nil {
		return chatRequest{}, fmt.Errorf("model request is required")
	}
	modelID := strings.TrimSpace(request.Model)
	if modelID == "" {
		modelID = fallbackModel
	}
	var tools []*genai.Tool
	if request.Config != nil {
		tools = request.Config.Tools
	}
	codec, err := newToolNameCodec(tools)
	if err != nil {
		return chatRequest{}, err
	}
	messages, err := convertContents(request, codec)
	if err != nil {
		return chatRequest{}, err
	}
	result := chatRequest{Model: modelID, Messages: messages, Stream: stream, codec: codec}
	if stream {
		result.StreamOptions = &chatStreamOptions{IncludeUsage: true}
	}
	if request.Config == nil {
		return result, nil
	}
	config := request.Config
	if config.ResponseMIMEType == "application/json" {
		result.ResponseFormat = &chatResponseFormat{Type: "json_object"}
	}
	result.Temperature = config.Temperature
	result.TopP = config.TopP
	result.MaxTokens = config.MaxOutputTokens
	result.Stop = config.StopSequences
	result.Tools, err = convertTools(config.Tools, codec)
	if err != nil {
		return chatRequest{}, err
	}
	if len(result.Tools) > 0 {
		parallelToolCalls := false
		result.ParallelToolCalls = &parallelToolCalls
	}
	return result, nil
}

func convertContents(request *adkmodel.LLMRequest, codecs ...toolNameCodec) ([]chatMessage, error) {
	codec := firstToolNameCodec(codecs)
	messages := make([]chatMessage, 0, len(request.Contents)+1)
	if request.Config != nil && request.Config.SystemInstruction != nil {
		text, err := textOnly(request.Config.SystemInstruction)
		if err != nil {
			return nil, fmt.Errorf("convert system instruction: %w", err)
		}
		if text != "" {
			messages = append(messages, newTextMessage("system", text))
		}
	}
	for _, content := range request.Contents {
		if content == nil {
			continue
		}
		converted, err := convertContent(content, codec)
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted...)
	}
	return messages, nil
}

func convertContent(content *genai.Content, codecs ...toolNameCodec) ([]chatMessage, error) {
	codec := firstToolNameCodec(codecs)
	role := string(content.Role)
	if role == "" {
		role = string(genai.RoleUser)
	}
	var text strings.Builder
	var reasoning strings.Builder
	functionCalls := make([]chatToolCall, 0)
	functionResponses := make([]chatMessage, 0)
	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		switch {
		case part.FunctionCall != nil:
			arguments, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				return nil, fmt.Errorf("encode function call %q arguments: %w", part.FunctionCall.Name, err)
			}
			name, err := codec.external(part.FunctionCall.Name)
			if err != nil {
				return nil, err
			}
			functionCalls = append(functionCalls, chatToolCall{
				ID: part.FunctionCall.ID, Type: "function",
				Function: chatToolCallFunction{Name: name, Arguments: string(arguments)},
			})
		case part.FunctionResponse != nil:
			encoded, err := json.Marshal(part.FunctionResponse.Response)
			if err != nil {
				return nil, fmt.Errorf("encode function response %q: %w", part.FunctionResponse.Name, err)
			}
			value := string(encoded)
			name, err := codec.external(part.FunctionResponse.Name)
			if err != nil {
				return nil, err
			}
			functionResponses = append(functionResponses, chatMessage{
				Role: "tool", Content: &value, Name: name,
				ToolCallID: part.FunctionResponse.ID,
			})
		case part.Text != "":
			if role == string(genai.RoleModel) && part.Thought {
				reasoning.WriteString(part.Text)
			} else {
				text.WriteString(part.Text)
			}
		case hasUnsupportedPart(part):
			return nil, fmt.Errorf("OpenAI-compatible model does not support this content part")
		}
	}

	if role == string(genai.RoleModel) {
		if len(functionResponses) > 0 {
			return nil, fmt.Errorf("model content cannot contain function responses")
		}
		if text.Len() == 0 && reasoning.Len() == 0 && len(functionCalls) == 0 {
			return nil, nil
		}
		message := chatMessage{Role: "assistant", ToolCalls: functionCalls}
		if text.Len() > 0 {
			value := text.String()
			message.Content = &value
		}
		if reasoning.Len() > 0 {
			value := reasoning.String()
			message.ReasoningContent = &value
		}
		return []chatMessage{message}, nil
	}
	if len(functionCalls) > 0 {
		return nil, fmt.Errorf("user content cannot contain function calls")
	}
	messages := make([]chatMessage, 0, len(functionResponses)+1)
	if text.Len() > 0 {
		messages = append(messages, newTextMessage("user", text.String()))
	}
	messages = append(messages, functionResponses...)
	return messages, nil
}

func textOnly(content *genai.Content) (string, error) {
	var text strings.Builder
	for _, part := range content.Parts {
		if part == nil {
			continue
		}
		if part.Text != "" {
			text.WriteString(part.Text)
			continue
		}
		if part.FunctionCall != nil || part.FunctionResponse != nil || hasUnsupportedPart(part) {
			return "", fmt.Errorf("only text content is supported")
		}
	}
	return text.String(), nil
}

func hasUnsupportedPart(part *genai.Part) bool {
	return part.InlineData != nil || part.FileData != nil || part.ExecutableCode != nil ||
		part.CodeExecutionResult != nil || part.ToolCall != nil || part.ToolResponse != nil
}

func convertTools(tools []*genai.Tool, codecs ...toolNameCodec) ([]chatTool, error) {
	codec := firstToolNameCodec(codecs)
	converted := make([]chatTool, 0)
	seen := make(map[string]struct{})
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		if len(tool.FunctionDeclarations) == 0 {
			return nil, fmt.Errorf("OpenAI-compatible model only supports function tools")
		}
		for _, declaration := range tool.FunctionDeclarations {
			if declaration == nil || strings.TrimSpace(declaration.Name) == "" {
				return nil, fmt.Errorf("function tool name is required")
			}
			if _, exists := seen[declaration.Name]; exists {
				return nil, fmt.Errorf("duplicate function tool %q", declaration.Name)
			}
			seen[declaration.Name] = struct{}{}
			parameters := declaration.ParametersJsonSchema
			if parameters == nil {
				parameters = declaration.Parameters
			}
			if parameters == nil {
				parameters = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			name, err := codec.external(declaration.Name)
			if err != nil {
				return nil, err
			}
			converted = append(converted, chatTool{Type: "function", Function: chatFunctionDefinition{
				Name: name, Description: declaration.Description, Parameters: parameters,
			}})
		}
	}
	return converted, nil
}

func newTextMessage(role, value string) chatMessage {
	return chatMessage{Role: role, Content: &value}
}

func streamChatResponse(body io.Reader, yield func(*adkmodel.LLMResponse, error) bool, codecs ...toolNameCodec) {
	codec := firstToolNameCodec(codecs)
	accumulator := newStreamAccumulator(codec)
	completed := false
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			completed = true
			break
		}
		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			yield(nil, fmt.Errorf("decode model stream: %w", err))
			return
		}
		if chunk.Error != nil {
			yield(nil, fmt.Errorf("model provider error: %s", chunk.Error.Message))
			return
		}
		if chunk.Usage != nil {
			accumulator.usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				continue
			}
			accumulator.received = true
			if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
				accumulator.finishReason = *choice.FinishReason
				completed = true
			}
			if choice.Delta.Content != "" {
				accumulator.text.WriteString(choice.Delta.Content)
				if !yield(&adkmodel.LLMResponse{
					Content: genai.NewContentFromText(choice.Delta.Content, genai.RoleModel), Partial: true,
				}, nil) {
					return
				}
			}
			if choice.Delta.ReasoningContent != "" {
				accumulator.reasoning.WriteString(choice.Delta.ReasoningContent)
				if !yield(&adkmodel.LLMResponse{
					Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: choice.Delta.ReasoningContent, Thought: true}}},
					Partial: true,
				}, nil) {
					return
				}
			}
			for _, toolCall := range choice.Delta.ToolCalls {
				accumulator.addToolCall(toolCall)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		yield(nil, fmt.Errorf("read model stream: %w", err))
		return
	}
	if !completed {
		yield(nil, errors.New("model provider stream ended before completion"))
		return
	}
	response, err := accumulator.finalResponse()
	if err != nil {
		yield(nil, err)
		return
	}
	yield(response, nil)
}

func completeChatResponse(body io.Reader, yield func(*adkmodel.LLMResponse, error) bool, codecs ...toolNameCodec) {
	codec := firstToolNameCodec(codecs)
	var response chatCompletion
	if err := json.NewDecoder(body).Decode(&response); err != nil {
		yield(nil, fmt.Errorf("decode model response: %w", err))
		return
	}
	if response.Error != nil {
		yield(nil, fmt.Errorf("model provider error: %s", response.Error.Message))
		return
	}
	if len(response.Choices) == 0 {
		yield(nil, fmt.Errorf("model provider returned no choices"))
		return
	}
	choice := response.Choices[0]
	if err := terminalFinishError(choice.FinishReason); err != nil {
		yield(nil, err)
		return
	}
	content, err := completionContent(choice.Message, codec)
	if err != nil {
		yield(nil, err)
		return
	}
	yield(&adkmodel.LLMResponse{
		Content: content, UsageMetadata: response.Usage.metadata(),
		FinishReason: finishReason(choice.FinishReason), TurnComplete: true,
	}, nil)
}

func completionContent(message chatCompletionMessage, codecs ...toolNameCodec) (*genai.Content, error) {
	codec := firstToolNameCodec(codecs)
	parts := make([]*genai.Part, 0, len(message.ToolCalls)+2)
	if message.ReasoningContent != "" {
		parts = append(parts, &genai.Part{Text: message.ReasoningContent, Thought: true})
	}
	if message.Content != "" {
		parts = append(parts, genai.NewPartFromText(message.Content))
	}
	for _, call := range message.ToolCalls {
		functionCall, err := call.functionCall(codec)
		if err != nil {
			return nil, err
		}
		parts = append(parts, &genai.Part{FunctionCall: functionCall})
	}
	return &genai.Content{Role: genai.RoleModel, Parts: parts}, nil
}

type streamAccumulator struct {
	received     bool
	text         strings.Builder
	reasoning    strings.Builder
	toolCalls    map[int]*chatToolCall
	toolOrder    []int
	usage        *chatUsage
	finishReason string
	codec        toolNameCodec
}

func newStreamAccumulator(codecs ...toolNameCodec) *streamAccumulator {
	return &streamAccumulator{
		toolCalls: make(map[int]*chatToolCall),
		codec:     firstToolNameCodec(codecs),
	}
}

func (a *streamAccumulator) addToolCall(delta chatToolCallDelta) {
	call, exists := a.toolCalls[delta.Index]
	if !exists {
		call = &chatToolCall{Type: "function"}
		a.toolCalls[delta.Index] = call
		a.toolOrder = append(a.toolOrder, delta.Index)
	}
	if delta.ID != "" {
		call.ID = delta.ID
	}
	if delta.Type != "" {
		call.Type = delta.Type
	}
	call.Function.Name += delta.Function.Name
	call.Function.Arguments += delta.Function.Arguments
}

func (a *streamAccumulator) finalResponse() (*adkmodel.LLMResponse, error) {
	if !a.received && a.text.Len() == 0 && a.reasoning.Len() == 0 && len(a.toolCalls) == 0 {
		return nil, fmt.Errorf("model provider returned an empty stream")
	}
	if err := terminalFinishError(a.finishReason); err != nil {
		return nil, err
	}
	parts := make([]*genai.Part, 0, len(a.toolCalls)+2)
	if a.reasoning.Len() > 0 {
		parts = append(parts, &genai.Part{Text: a.reasoning.String(), Thought: true})
	}
	if a.text.Len() > 0 {
		parts = append(parts, genai.NewPartFromText(a.text.String()))
	}
	for _, index := range a.toolOrder {
		call := a.toolCalls[index]
		functionCall, err := call.functionCall(a.codec)
		if err != nil {
			return nil, err
		}
		parts = append(parts, &genai.Part{FunctionCall: functionCall})
	}
	var usage *genai.GenerateContentResponseUsageMetadata
	if a.usage != nil {
		usage = a.usage.metadata()
	}
	return &adkmodel.LLMResponse{
		Content:       &genai.Content{Role: genai.RoleModel, Parts: parts},
		UsageMetadata: usage, FinishReason: finishReason(a.finishReason), TurnComplete: true,
	}, nil
}

func terminalFinishError(value string) error {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "length":
		return errors.New("model provider exhausted the configured output token budget")
	case "content_filter":
		return errors.New("model provider stopped generation because of its content filter")
	default:
		return nil
	}
}

func (c chatToolCall) functionCall(codecs ...toolNameCodec) (*genai.FunctionCall, error) {
	codec := firstToolNameCodec(codecs)
	arguments := strings.TrimSpace(c.Function.Arguments)
	if arguments == "" {
		arguments = "{}"
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return nil, fmt.Errorf("decode function call %q (call %q) arguments: %w", c.Function.Name, c.ID, err)
	}
	if args == nil {
		args = make(map[string]any)
	}
	name, err := codec.internal(c.Function.Name)
	if err != nil {
		return nil, err
	}
	return &genai.FunctionCall{ID: c.ID, Name: name, Args: args}, nil
}

func finishReason(value string) genai.FinishReason {
	switch strings.ToLower(value) {
	case "stop", "tool_calls", "function_call":
		return genai.FinishReasonStop
	case "length":
		return genai.FinishReasonMaxTokens
	case "content_filter":
		return genai.FinishReasonSafety
	case "":
		return genai.FinishReasonUnspecified
	default:
		return genai.FinishReasonOther
	}
}

type chatRequest struct {
	Model             string              `json:"model"`
	Messages          []chatMessage       `json:"messages"`
	Stream            bool                `json:"stream"`
	StreamOptions     *chatStreamOptions  `json:"stream_options,omitempty"`
	ResponseFormat    *chatResponseFormat `json:"response_format,omitempty"`
	Tools             []chatTool          `json:"tools,omitempty"`
	ParallelToolCalls *bool               `json:"parallel_tool_calls,omitempty"`
	Temperature       *float32            `json:"temperature,omitempty"`
	TopP              *float32            `json:"top_p,omitempty"`
	MaxTokens         int32               `json:"max_tokens,omitempty"`
	Stop              []string            `json:"stop,omitempty"`
	codec             toolNameCodec       `json:"-"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatResponseFormat struct {
	Type string `json:"type"`
}

type chatMessage struct {
	Role             string         `json:"role"`
	Content          *string        `json:"content,omitempty"`
	ReasoningContent *string        `json:"reasoning_content,omitempty"`
	Name             string         `json:"name,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	ToolCalls        []chatToolCall `json:"tool_calls,omitempty"`
}

type chatTool struct {
	Type     string                 `json:"type"`
	Function chatFunctionDefinition `json:"function"`
}

type chatFunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters"`
}

type chatToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function chatToolCallFunction `json:"function"`
}

type chatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatChunk struct {
	Choices []struct {
		Index        int     `json:"index"`
		FinishReason *string `json:"finish_reason"`
		Delta        struct {
			Content          string              `json:"content"`
			ReasoningContent string              `json:"reasoning_content"`
			ToolCalls        []chatToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *chatUsage `json:"usage,omitempty"`
	Error *chatError `json:"error,omitempty"`
}

type chatToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatCompletion struct {
	Choices []struct {
		Index        int                   `json:"index"`
		FinishReason string                `json:"finish_reason"`
		Message      chatCompletionMessage `json:"message"`
	} `json:"choices"`
	Usage chatUsage  `json:"usage"`
	Error *chatError `json:"error,omitempty"`
}

type chatCompletionMessage struct {
	Role             string         `json:"role"`
	Content          string         `json:"content"`
	ReasoningContent string         `json:"reasoning_content"`
	ToolCalls        []chatToolCall `json:"tool_calls"`
}

type chatUsage struct {
	PromptTokens     int32 `json:"prompt_tokens"`
	CompletionTokens int32 `json:"completion_tokens"`
	TotalTokens      int32 `json:"total_tokens"`
	PromptDetails    *struct {
		CachedTokens int32 `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

func (u chatUsage) metadata() *genai.GenerateContentResponseUsageMetadata {
	metadata := &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount: u.PromptTokens, CandidatesTokenCount: u.CompletionTokens, TotalTokenCount: u.TotalTokens,
	}
	if u.PromptDetails != nil {
		metadata.CachedContentTokenCount = u.PromptDetails.CachedTokens
	}
	return metadata
}

type chatError struct {
	Message string `json:"message"`
}

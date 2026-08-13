package adk

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/genai"

	appharness "warmmo/core/internal/application/harness"
)

func genAISchema(schema map[string]any) (*genai.Schema, error) {
	if len(schema) == 0 {
		return nil, nil
	}
	compatible, err := genAICompatibleSchema(schema)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(compatible)
	if err != nil {
		return nil, err
	}
	var result genai.Schema
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func genAICompatibleSchema(value any) (any, error) {
	switch current := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, child := range current {
			if key == "const" {
				constant, ok := child.(string)
				if !ok {
					return nil, fmt.Errorf("ADK response schema cannot represent non-string const %v", child)
				}
				if _, exists := current["enum"]; exists {
					return nil, errors.New("response schema cannot contain both const and enum")
				}
				result["enum"] = []string{constant}
				continue
			}
			converted, err := genAICompatibleSchema(child)
			if err != nil {
				return nil, err
			}
			result[key] = converted
		}
		return result, nil
	case []any:
		result := make([]any, len(current))
		for index, child := range current {
			converted, err := genAICompatibleSchema(child)
			if err != nil {
				return nil, err
			}
			result[index] = converted
		}
		return result, nil
	default:
		return value, nil
	}
}

func validateStructuredOutput(schema map[string]any, message *appharness.Message) (json.RawMessage, error) {
	if message == nil || strings.TrimSpace(message.Content) == "" {
		return nil, fmt.Errorf("%w: agent completed without required structured output", appharness.ErrInvalidOutput)
	}
	output, err := decodeStructuredOutput(schema, message.Content)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", appharness.ErrInvalidOutput, err)
	}
	encodedSchema, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encode response schema: %w", err)
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(encodedSchema, &parsed); err != nil {
		return nil, fmt.Errorf("decode response schema: %w", err)
	}
	resolved, err := parsed.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolve response schema: %w", err)
	}
	if err := resolved.Validate(output); err != nil {
		return nil, fmt.Errorf("%w: validate structured output: %v", appharness.ErrInvalidOutput, err)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("encode structured output: %w", err)
	}
	return encoded, nil
}

func decodeStructuredOutput(schema map[string]any, content string) (any, error) {
	var output any
	err := json.Unmarshal([]byte(content), &output)
	if schema["type"] == "string" {
		if text, ok := output.(string); err == nil && ok {
			return text, nil
		}
		return content, nil
	}
	if err != nil {
		return nil, fmt.Errorf("decode structured output: %w", err)
	}
	return output, nil
}

func cloneMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned map[string]any
	if json.Unmarshal(encoded, &cloned) != nil {
		return value
	}
	return cloned
}

func structuredOutputCorrection(err error) *genai.Content {
	cause := truncateUTF8(err.Error(), 2048)
	message := "Your previous structured response was invalid: " + cause +
		"\nReturn the entire result again. It must be valid JSON matching the required response schema. Do not omit fields, truncate the response, use markdown fences, or include explanatory text."
	return genai.NewContentFromText(message, genai.RoleUser)
}

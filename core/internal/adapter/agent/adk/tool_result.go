package adk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"

	appharness "warmmo/core/internal/application/harness"
)

const (
	toolMetadataKey         = "_warmmo"
	defaultToolSummaryBytes = 4096
)

type toolResultCodec struct {
	maxBytes int
}

func (c toolResultCodec) encode(spec appharness.ToolSpec, outputSchema *jsonschema.Resolved, value any) (map[string]any, error) {
	normalized, err := normalizeToolResult(value)
	if err != nil {
		return nil, appharness.NewToolError(appharness.ToolErrorInternal, false, err)
	}
	if outputSchema != nil {
		if err := outputSchema.Validate(normalized); err != nil {
			return nil, appharness.NewToolError(appharness.ToolErrorInternal, false, fmt.Errorf("tool %q returned an invalid result: %w", spec.Name, err))
		}
	}
	redacted, ok := redactToolValue(normalized).(map[string]any)
	if !ok {
		return nil, appharness.NewToolError(appharness.ToolErrorInternal, false, errors.New("normalized tool result is not an object"))
	}
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return nil, appharness.NewToolError(appharness.ToolErrorInternal, false, fmt.Errorf("encode redacted tool result: %w", err))
	}
	limit := spec.MaxResultBytes
	if limit <= 0 || limit > c.maxBytes {
		limit = c.maxBytes
	}
	if len(encoded) > limit {
		return nil, appharness.NewToolError(appharness.ToolErrorCapability, false, fmt.Errorf("tool %q result is %d bytes; maximum is %d", spec.Name, len(encoded), limit))
	}
	summary := truncateUTF8(string(encoded), min(defaultToolSummaryBytes, max(limit/4, 256)))
	redacted[toolMetadataKey] = map[string]any{"summary": summary, "resultBytes": len(encoded), "truncated": false}
	return redacted, nil
}

func toolErrorResult(err error) map[string]any {
	code, retryable := classifyToolError(err)
	message := truncateUTF8(err.Error(), 2048)
	return map[string]any{
		"error":         map[string]any{"code": string(code), "message": message, "retryable": retryable},
		toolMetadataKey: map[string]any{"summary": message, "truncated": false},
	}
}

func classifyToolError(err error) (appharness.ToolErrorCode, bool) {
	var typed *appharness.ToolError
	if errors.As(err, &typed) {
		return typed.Code, typed.Retryable
	}
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return appharness.ToolErrorCancelled, false
	case errors.Is(err, appharness.ErrBudgetExceeded):
		return appharness.ToolErrorBudget, false
	case errors.Is(err, appharness.ErrInvalidOutput):
		return appharness.ToolErrorInvalidArgument, false
	case errors.Is(err, appharness.ErrToolCallInDoubt), errors.Is(err, appharness.ErrToolCallConflict):
		return appharness.ToolErrorConflict, false
	case errors.Is(err, appharness.ErrInvalidToolArguments):
		return appharness.ToolErrorInvalidArgument, false
	case errors.Is(err, appharness.ErrToolNotFound):
		return appharness.ToolErrorNotFound, false
	case errors.Is(err, appharness.ErrToolNotAllowed):
		return appharness.ToolErrorPermission, false
	case errors.Is(err, appharness.ErrToolCapability):
		return appharness.ToolErrorCapability, false
	}
	return appharness.ToolErrorInternal, false
}

func redactToolValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, child := range current {
			if sensitiveToolKey(key) {
				result[key] = "[REDACTED]"
			} else {
				result[key] = redactToolValue(child)
			}
		}
		return result
	case []any:
		result := make([]any, len(current))
		for i, child := range current {
			result[i] = redactToolValue(child)
		}
		return result
	default:
		return value
	}
}

func sensitiveToolKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	for _, marker := range []string{"api_key", "apikey", "authorization", "password", "secret", "access_token", "refresh_token"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func truncateUTF8(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	end := limit
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end] + "..."
}

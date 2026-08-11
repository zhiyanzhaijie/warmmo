package harness

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrToolCallNotFound = errors.New("agent tool call not found")
	ErrToolCallConflict = errors.New("agent tool call conflict")
	ErrToolCallInDoubt  = errors.New("agent side-effect tool call outcome is in doubt")
)

type ToolCallStatus string

const (
	ToolCallStarted   ToolCallStatus = "started"
	ToolCallCompleted ToolCallStatus = "completed"
)

type ToolCallRecord struct {
	CallID     string          `json:"callId"`
	RunID      string          `json:"runId"`
	TurnID     string          `json:"turnId"`
	ToolName   string          `json:"toolName"`
	ArgsHash   string          `json:"argsHash"`
	SideEffect string          `json:"sideEffect"`
	Status     ToolCallStatus  `json:"status"`
	Result     json.RawMessage `json:"result,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

type ToolCallStore interface {
	ClaimToolCall(context.Context, ToolCallRecord) (ToolCallRecord, bool, error)
	CompleteToolCall(context.Context, string, string, json.RawMessage) (ToolCallRecord, error)
}

package harness

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	ErrToolNotFound         = errors.New("tool not found")
	ErrToolNotAllowed       = errors.New("tool is not allowed by active agent")
	ErrInvalidToolArguments = errors.New("invalid tool arguments")
	ErrToolCapability       = errors.New("tool capability is unavailable")
)

type SideEffectClass string

const (
	SideEffectNone     SideEffectClass = "none"
	SideEffectRead     SideEffectClass = "read"
	SideEffectDelegate SideEffectClass = "delegate"
	SideEffectWrite    SideEffectClass = "write"
	SideEffectExternal SideEffectClass = "external"
)

func (s SideEffectClass) Mutates() bool {
	return s == SideEffectWrite || s == SideEffectExternal
}

func (s SideEffectClass) RequiresIdempotency() bool {
	return s == SideEffectDelegate || s.Mutates()
}

type ApprovalPolicy string

const (
	ApprovalNever  ApprovalPolicy = "never"
	ApprovalAlways ApprovalPolicy = "always"
)

type ToolErrorCode string

const (
	ToolErrorInvalidArgument ToolErrorCode = "invalid_argument"
	ToolErrorNotFound        ToolErrorCode = "not_found"
	ToolErrorConflict        ToolErrorCode = "conflict"
	ToolErrorPermission      ToolErrorCode = "permission_denied"
	ToolErrorCapability      ToolErrorCode = "capability_unavailable"
	ToolErrorBudget          ToolErrorCode = "budget_exceeded"
	ToolErrorCancelled       ToolErrorCode = "cancelled"
	ToolErrorInternal        ToolErrorCode = "internal"
)

type ToolError struct {
	Code      ToolErrorCode
	Retryable bool
	Err       error
}

func (e *ToolError) Error() string {
	if e == nil || e.Err == nil {
		return "tool execution failed"
	}
	return e.Err.Error()
}

func (e *ToolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewToolError(code ToolErrorCode, retryable bool, err error) error {
	if err == nil {
		return nil
	}
	return &ToolError{Code: code, Retryable: retryable, Err: err}
}

type ToolSpec struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	InputSchema    map[string]any  `json:"inputSchema,omitempty"`
	OutputSchema   map[string]any  `json:"outputSchema,omitempty"`
	SideEffect     SideEffectClass `json:"sideEffect"`
	Approval       ApprovalPolicy  `json:"approval"`
	MaxResultBytes int             `json:"maxResultBytes,omitempty"`
	LongRunning    bool            `json:"longRunning,omitempty"`
	Terminal       bool            `json:"terminal,omitempty"`
	ModelCallable  bool            `json:"-"`
}

type ToolInvocation struct {
	RunID        string
	TurnID       string
	CallID       string
	WorkID       string
	SkillID      string
	SkillVersion string
	AllowedTools []string
	Args         json.RawMessage
}

type Tool interface {
	Spec() ToolSpec
	Call(context.Context, ToolInvocation) (any, error)
}

// ToolCatalog exposes the capabilities visible to an agent. Its registry and
// storage are adapter concerns; application only defines the port.
type ToolCatalog interface {
	StrictSnapshot([]string) ([]Tool, error)
}

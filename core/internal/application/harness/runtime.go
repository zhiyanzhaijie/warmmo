package harness

import (
	"context"
	"encoding/json"
)

// RuntimeRequest describes one agent execution without exposing the SDK used
// to run it. Product orchestration owns this request; an adapter executes it.
type RuntimeRequest struct {
	RunID                   string
	TurnID                  string
	AgentID                 string
	AgentName               string
	Description             string
	Instruction             string
	DefinitionVersion       string
	DefinitionHash          string
	PromptHash              string
	ToolsetHash             string
	ProviderID              string
	ModelID                 string
	ConversationSessionID   string
	UserID                  string
	SessionID               string
	Prompt                  string
	ConversationUserContent string
	PublishConversation     bool
	AllowedTools            []string
	ControlTools            []string
	Extension               json.RawMessage
	ToolInvocation          ToolInvocation
	Budget                  BudgetPolicy
	Context                 ContextPolicy
	Memory                  MemoryPolicy
	ResponseSchema          map[string]any
	Resume                  *ResumeInput
}

type RuntimeEventType string

const ControlToolAskUser = "ask_user"

const (
	RuntimeEventMessageDelta       RuntimeEventType = "message.delta"
	RuntimeEventReasoningStarted   RuntimeEventType = "reasoning.started"
	RuntimeEventReasoningDelta     RuntimeEventType = "reasoning.delta"
	RuntimeEventReasoningCompleted RuntimeEventType = "reasoning.completed"
	RuntimeEventToolRequested      RuntimeEventType = "tool.requested"
	RuntimeEventToolStarted        RuntimeEventType = "tool.started"
	RuntimeEventToolCompleted      RuntimeEventType = "tool.completed"
	RuntimeEventToolFailed         RuntimeEventType = "tool.failed"
	RuntimeEventMemoryFailed       RuntimeEventType = "memory.failed"
	RuntimeEventConversationFailed RuntimeEventType = "conversation.failed"
	RuntimeEventCompleted          RuntimeEventType = "runtime.completed"
	RuntimeEventPaused             RuntimeEventType = "runtime.paused"
	RuntimeEventStopped            RuntimeEventType = "runtime.stopped"
	RuntimeEventCancelled          RuntimeEventType = "runtime.cancelled"
	RuntimeEventFailed             RuntimeEventType = "runtime.failed"
)

type RuntimeEvent struct {
	Type        RuntimeEventType
	EventID     string
	AgentName   string
	Text        string
	ToolName    string
	ToolCallID  string
	ErrorCode   string
	Retryable   bool
	Summary     string
	ResultBytes int
	Truncated   bool
	Payload     json.RawMessage
	Usage       Usage
	Budget      BudgetUsage
}

type RuntimeEmitter func(RuntimeEvent) error

// AgentRuntime is the single execution seam used by chat agents, specialists,
// and application workflows. The implementation owns the model/tool loop.
type AgentRuntime interface {
	Run(context.Context, RuntimeRequest, RuntimeEmitter) (TurnOutcome, error)
	Resume(context.Context, string, string, RuntimeEmitter) (TurnOutcome, error)
	Restore(context.Context, Checkpoint) (TurnOutcome, error)
}

// DynamicToolProvider supplies invocation-scoped capabilities such as
// specialist delegation without coupling application orchestration to an SDK.
type DynamicToolProvider func(RuntimeRequest, RuntimeEmitter) ([]Tool, error)

// DelegationPort is defined by the application and consumed by runtime
// adapters that expose specialists as model-callable tools.
type DelegationPort interface {
	Capabilities(RuntimeRequest) ([]DelegationCapability, error)
	Delegate(context.Context, DelegationRequest) (ArtifactRef, error)
}

type DelegationCapability struct {
	Name        string
	Description string
}

type DelegationRequest struct {
	Runtime RuntimeRequest
	Target  string
	Task    string
}

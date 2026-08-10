package core

import "context"

type EventType string

const (
	EventToolRequested EventType = "tool.requested"
	EventToolStarted   EventType = "tool.started"
	EventToolCompleted EventType = "tool.completed"
	EventToolFailed    EventType = "tool.failed"
)

type Emitter func(EventType, any) error

type ModelResponseFormat string

const (
	ModelResponseFormatText       ModelResponseFormat = ""
	ModelResponseFormatJSONObject ModelResponseFormat = "json_object"
)

type ModelRequest struct {
	ModelID        string
	System         string
	Prompt         string
	ResponseFormat ModelResponseFormat
}

type ModelUsage struct {
	InputTokens  int64
	OutputTokens int64
}

// TextModel is the only model capability required by the agent runtime.
// Provider and ADK concerns stay behind this port.
type TextModel interface {
	Complete(context.Context, ModelRequest) (string, ModelUsage, error)
	Stream(context.Context, ModelRequest, func(string) error) (ModelUsage, error)
}

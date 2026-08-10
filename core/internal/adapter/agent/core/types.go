package core

import appagent "warmmo/core/internal/application/agent"

type EventType = appagent.EventType
type Emitter = appagent.Emitter
type ModelResponseFormat = appagent.ModelResponseFormat
type ModelRequest = appagent.ModelRequest
type ModelUsage = appagent.ModelUsage
type TextModel = appagent.TextModel

const (
	EventToolRequested = appagent.EventToolRequested
	EventToolStarted   = appagent.EventToolStarted
	EventToolCompleted = appagent.EventToolCompleted
	EventToolFailed    = appagent.EventToolFailed

	ModelResponseFormatText       = appagent.ModelResponseFormatText
	ModelResponseFormatJSONObject = appagent.ModelResponseFormatJSONObject
)

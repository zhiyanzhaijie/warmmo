package core

import appagent "warmmo/core/internal/application/agent"

type EventType = appagent.EventType
type Emitter = appagent.Emitter

const (
	EventToolRequested = appagent.EventToolRequested
	EventToolStarted   = appagent.EventToolStarted
	EventToolCompleted = appagent.EventToolCompleted
	EventToolFailed    = appagent.EventToolFailed
)

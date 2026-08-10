package application

import (
	"context"
	"runtime"
	"time"
)

type RuntimeService struct {
	version string
}

func NewRuntimeService(version string) *RuntimeService {
	return &RuntimeService{version: version}
}

func (s *RuntimeService) GetInfo(_ context.Context, requestID string) RuntimeInfo {
	return RuntimeInfo{
		Name:       "Warmnote Core",
		Version:    s.version,
		Status:     "ready",
		GoVersion:  runtime.Version(),
		Platform:   runtime.GOOS + "/" + runtime.GOARCH,
		ServerTime: time.Now().UTC(),
		RequestID:  requestID,
	}
}

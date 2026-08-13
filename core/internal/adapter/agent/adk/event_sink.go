package adk

import "sync"

type eventSink struct {
	emit LLMTurnEmitter
	mu   sync.Mutex
	err  error
}

func (s *eventSink) project(event LLMTurnEvent) error {
	if s == nil || s.emit == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.emit(event); err != nil && s.err == nil {
		s.err = err
	}
	return nil
}

func (s *eventSink) Error() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

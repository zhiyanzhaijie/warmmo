package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/adk/session"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appharness "warmmo/core/internal/application/harness"
)

const (
	sessionStateScopeApp  = "app"
	sessionStateScopeUser = "user"
)

type AgentSessionService struct {
	database *gorm.DB
}

func NewAgentSessionService(database *Database) *AgentSessionService {
	if database == nil {
		return &AgentSessionService{}
	}
	return &AgentSessionService{database: database.DB}
}

func (s *AgentSessionService) Create(ctx context.Context, request *session.CreateRequest) (*session.CreateResponse, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("agent session service is not configured")
	}
	if request == nil || strings.TrimSpace(request.AppName) == "" || strings.TrimSpace(request.UserID) == "" {
		return nil, errors.New("app name and user ID are required")
	}
	sessionID := strings.TrimSpace(request.SessionID)
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	appState, userState, localState := splitSessionState(request.State)
	model := agentSessionModel{
		ID: sessionID, AppName: request.AppName, UserID: request.UserID,
		State: localState, CreatedAt: now, UpdatedAt: now,
	}
	var merged map[string]any
	err := s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model).Error; err != nil {
			return fmt.Errorf("create agent session: %w", err)
		}
		storedApp, err := mergeScopedState(tx, sessionStateScopeApp, request.AppName, "", appState, now)
		if err != nil {
			return err
		}
		storedUser, err := mergeScopedState(tx, sessionStateScopeUser, request.AppName, request.UserID, userState, now)
		if err != nil {
			return err
		}
		merged = mergeSessionStates(storedApp, storedUser, localState)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &session.CreateResponse{Session: newDurableSession(model, merged, nil)}, nil
}

func (s *AgentSessionService) Get(ctx context.Context, request *session.GetRequest) (*session.GetResponse, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("agent session service is not configured")
	}
	if request == nil || request.AppName == "" || request.UserID == "" || request.SessionID == "" {
		return nil, errors.New("app name, user ID and session ID are required")
	}
	var model agentSessionModel
	err := s.database.WithContext(ctx).Where(
		"app_name = ? AND user_id = ? AND id = ?", request.AppName, request.UserID, request.SessionID,
	).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %s", appharness.ErrSessionNotFound, request.SessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("read agent session: %w", err)
	}
	appState, userState, err := s.loadScopedStates(ctx, request.AppName, request.UserID)
	if err != nil {
		return nil, err
	}
	events, err := s.loadEvents(ctx, request, model)
	if err != nil {
		return nil, err
	}
	return &session.GetResponse{Session: newDurableSession(
		model, mergeSessionStates(appState, userState, model.State), events,
	)}, nil
}

func (s *AgentSessionService) List(ctx context.Context, request *session.ListRequest) (*session.ListResponse, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("agent session service is not configured")
	}
	if request == nil || request.AppName == "" {
		return nil, errors.New("app name is required")
	}
	query := s.database.WithContext(ctx).Where("app_name = ?", request.AppName)
	if request.UserID != "" {
		query = query.Where("user_id = ?", request.UserID)
	}
	var models []agentSessionModel
	if err := query.Order("updated_at DESC, id").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list agent sessions: %w", err)
	}
	appState, err := loadScopedState(s.database.WithContext(ctx), sessionStateScopeApp, request.AppName, "")
	if err != nil {
		return nil, err
	}
	userStates := make(map[string]map[string]any)
	result := make([]session.Session, 0, len(models))
	for _, model := range models {
		userState, ok := userStates[model.UserID]
		if !ok {
			userState, err = loadScopedState(s.database.WithContext(ctx), sessionStateScopeUser, request.AppName, model.UserID)
			if err != nil {
				return nil, err
			}
			userStates[model.UserID] = userState
		}
		result = append(result, newDurableSession(model, mergeSessionStates(appState, userState, model.State), nil))
	}
	return &session.ListResponse{Sessions: result}, nil
}

func (s *AgentSessionService) Delete(ctx context.Context, request *session.DeleteRequest) error {
	if s == nil || s.database == nil {
		return errors.New("agent session service is not configured")
	}
	if request == nil || request.AppName == "" || request.UserID == "" || request.SessionID == "" {
		return errors.New("app name, user ID and session ID are required")
	}
	return s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		where := "app_name = ? AND user_id = ? AND session_id = ?"
		if err := tx.Where(where, request.AppName, request.UserID, request.SessionID).Delete(&agentSessionEventModel{}).Error; err != nil {
			return fmt.Errorf("delete canonical session events: %w", err)
		}
		if err := tx.Where("app_name = ? AND user_id = ? AND id = ?", request.AppName, request.UserID, request.SessionID).Delete(&agentSessionModel{}).Error; err != nil {
			return fmt.Errorf("delete agent session: %w", err)
		}
		return nil
	})
}

func (s *AgentSessionService) AppendEvent(ctx context.Context, current session.Session, event *session.Event) error {
	if s == nil || s.database == nil {
		return errors.New("agent session service is not configured")
	}
	if current == nil || event == nil {
		return errors.New("session and event are required")
	}
	if event.Partial {
		return nil
	}
	local, ok := current.(*durableSession)
	if !ok {
		return fmt.Errorf("unexpected agent session type %T", current)
	}
	local.mu.Lock()
	defer local.mu.Unlock()

	canonical := cloneCanonicalEvent(event)
	canonical.Timestamp = canonical.Timestamp.UTC().Truncate(time.Microsecond)
	if canonical.ID == "" {
		canonical.ID = uuid.NewString()
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return fmt.Errorf("encode canonical session event: %w", err)
	}
	appDelta, userDelta, sessionDelta := splitSessionState(canonical.Actions.StateDelta)
	err = s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stored agentSessionModel
		if err := tx.Where("app_name = ? AND user_id = ? AND id = ?", local.appName, local.userID, local.id).First(&stored).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: %s", appharness.ErrSessionNotFound, local.id)
		} else if err != nil {
			return fmt.Errorf("read agent session before append: %w", err)
		}
		if stored.UpdatedAt.After(local.updatedAt) {
			return fmt.Errorf("stale agent session %s", local.id)
		}
		if stored.State == nil {
			stored.State = make(map[string]any)
		}
		maps.Copy(stored.State, sessionDelta)
		encodedState, err := json.Marshal(stored.State)
		if err != nil {
			return fmt.Errorf("encode agent session state: %w", err)
		}
		if _, err := mergeScopedState(tx, sessionStateScopeApp, local.appName, "", appDelta, canonical.Timestamp); err != nil {
			return err
		}
		if _, err := mergeScopedState(tx, sessionStateScopeUser, local.appName, local.userID, userDelta, canonical.Timestamp); err != nil {
			return err
		}
		var sequence int64
		if err := tx.Model(&agentSessionEventModel{}).
			Where("app_name = ? AND user_id = ? AND session_id = ?", local.appName, local.userID, local.id).
			Select("COALESCE(MAX(sequence), 0) + 1").Scan(&sequence).Error; err != nil {
			return fmt.Errorf("allocate canonical event sequence: %w", err)
		}
		eventModel := agentSessionEventModel{
			ID: canonical.ID, AppName: local.appName, UserID: local.userID, SessionID: local.id,
			Sequence: sequence, InvocationID: canonical.InvocationID, Author: canonical.Author,
			Branch: canonical.Branch, EventJSON: string(encoded), CreatedAt: canonical.Timestamp,
		}
		if err := tx.Create(&eventModel).Error; err != nil {
			return fmt.Errorf("append canonical session event: %w", err)
		}
		stored.UpdatedAt = canonical.Timestamp
		// Use an explicit map update so GORM does not replace the event timestamp
		// through its automatic UpdatedAt callback. The in-memory session uses
		// this timestamp as its optimistic-concurrency version.
		if err := tx.Model(&agentSessionModel{}).
			Where("app_name = ? AND user_id = ? AND id = ?", local.appName, local.userID, local.id).
			Updates(map[string]any{"state_json": string(encodedState), "updated_at": stored.UpdatedAt}).Error; err != nil {
			return fmt.Errorf("update agent session: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for key, value := range event.Actions.StateDelta {
		local.state[key] = value
	}
	local.events = append(local.events, canonical)
	local.updatedAt = canonical.Timestamp
	return nil
}

func (s *AgentSessionService) loadScopedStates(ctx context.Context, appName, userID string) (map[string]any, map[string]any, error) {
	appState, err := loadScopedState(s.database.WithContext(ctx), sessionStateScopeApp, appName, "")
	if err != nil {
		return nil, nil, err
	}
	userState, err := loadScopedState(s.database.WithContext(ctx), sessionStateScopeUser, appName, userID)
	if err != nil {
		return nil, nil, err
	}
	return appState, userState, nil
}

func (s *AgentSessionService) loadEvents(ctx context.Context, request *session.GetRequest, model agentSessionModel) ([]*session.Event, error) {
	query := s.database.WithContext(ctx).Where(
		"app_name = ? AND user_id = ? AND session_id = ?", model.AppName, model.UserID, model.ID,
	)
	if !request.After.IsZero() {
		query = query.Where("created_at >= ?", request.After.UTC())
	}
	query = query.Order("sequence DESC")
	if request.NumRecentEvents > 0 {
		query = query.Limit(request.NumRecentEvents)
	}
	var stored []agentSessionEventModel
	if err := query.Find(&stored).Error; err != nil {
		return nil, fmt.Errorf("read canonical session events: %w", err)
	}
	sort.Slice(stored, func(i, j int) bool { return stored[i].Sequence < stored[j].Sequence })
	events := make([]*session.Event, 0, len(stored))
	for _, model := range stored {
		var event session.Event
		if err := json.Unmarshal([]byte(model.EventJSON), &event); err != nil {
			return nil, fmt.Errorf("decode canonical session event %s: %w", model.ID, err)
		}
		events = append(events, &event)
	}
	return events, nil
}

func splitSessionState(values map[string]any) (map[string]any, map[string]any, map[string]any) {
	appState := make(map[string]any)
	userState := make(map[string]any)
	localState := make(map[string]any)
	for key, value := range values {
		if clean, ok := strings.CutPrefix(key, session.KeyPrefixApp); ok {
			appState[clean] = value
		} else if clean, ok := strings.CutPrefix(key, session.KeyPrefixUser); ok {
			userState[clean] = value
		} else if !strings.HasPrefix(key, session.KeyPrefixTemp) {
			localState[key] = value
		}
	}
	return appState, userState, localState
}

func mergeSessionStates(appState, userState, localState map[string]any) map[string]any {
	merged := maps.Clone(localState)
	if merged == nil {
		merged = make(map[string]any)
	}
	for key, value := range appState {
		merged[session.KeyPrefixApp+key] = value
	}
	for key, value := range userState {
		merged[session.KeyPrefixUser+key] = value
	}
	return merged
}

func loadScopedState(tx *gorm.DB, scope, appName, userID string) (map[string]any, error) {
	var model agentSessionScopedStateModel
	err := tx.Where("scope = ? AND app_name = ? AND user_id = ?", scope, appName, userID).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return make(map[string]any), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s agent state: %w", scope, err)
	}
	state := maps.Clone(model.State)
	if state == nil {
		state = make(map[string]any)
	}
	return state, nil
}

func mergeScopedState(tx *gorm.DB, scope, appName, userID string, delta map[string]any, now time.Time) (map[string]any, error) {
	state, err := loadScopedState(tx, scope, appName, userID)
	if err != nil {
		return nil, err
	}
	if len(delta) == 0 {
		return state, nil
	}
	maps.Copy(state, delta)
	model := agentSessionScopedStateModel{Scope: scope, AppName: appName, UserID: userID, State: state, UpdatedAt: now}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "scope"}, {Name: "app_name"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"state_json", "updated_at"}),
	}).Create(&model).Error; err != nil {
		return nil, fmt.Errorf("persist %s agent state: %w", scope, err)
	}
	return state, nil
}

func cloneCanonicalEvent(event *session.Event) *session.Event {
	cloned := *event
	cloned.Actions = event.Actions
	cloned.Actions.StateDelta = make(map[string]any)
	for key, value := range event.Actions.StateDelta {
		if !strings.HasPrefix(key, session.KeyPrefixTemp) {
			cloned.Actions.StateDelta[key] = value
		}
	}
	cloned.Actions.ArtifactDelta = maps.Clone(event.Actions.ArtifactDelta)
	cloned.Actions.RequestedToolConfirmations = maps.Clone(event.Actions.RequestedToolConfirmations)
	cloned.LongRunningToolIDs = slices.Clone(event.LongRunningToolIDs)
	return &cloned
}

type durableSession struct {
	mu        sync.RWMutex
	id        string
	appName   string
	userID    string
	state     map[string]any
	events    []*session.Event
	updatedAt time.Time
}

func newDurableSession(model agentSessionModel, state map[string]any, events []*session.Event) *durableSession {
	clonedState := maps.Clone(state)
	if clonedState == nil {
		clonedState = make(map[string]any)
	}
	return &durableSession{
		id: model.ID, appName: model.AppName, userID: model.UserID,
		state: clonedState, events: slices.Clone(events), updatedAt: model.UpdatedAt,
	}
}

func (s *durableSession) ID() string           { return s.id }
func (s *durableSession) AppName() string      { return s.appName }
func (s *durableSession) UserID() string       { return s.userID }
func (s *durableSession) State() session.State { return &durableState{session: s} }
func (s *durableSession) Events() session.Events {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return durableEvents(slices.Clone(s.events))
}
func (s *durableSession) LastUpdateTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}

type durableState struct{ session *durableSession }

func (s *durableState) Get(key string) (any, error) {
	s.session.mu.RLock()
	defer s.session.mu.RUnlock()
	value, ok := s.session.state[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return value, nil
}
func (s *durableState) Set(key string, value any) error {
	s.session.mu.Lock()
	defer s.session.mu.Unlock()
	if s.session.state == nil {
		s.session.state = make(map[string]any)
	}
	s.session.state[key] = value
	return nil
}
func (s *durableState) All() iter.Seq2[string, any] {
	s.session.mu.RLock()
	values := maps.Clone(s.session.state)
	s.session.mu.RUnlock()
	return func(yield func(string, any) bool) {
		for key, value := range values {
			if !yield(key, value) {
				return
			}
		}
	}
}

type durableEvents []*session.Event

func (e durableEvents) All() iter.Seq[*session.Event] { return slices.Values(e) }
func (e durableEvents) Len() int                      { return len(e) }
func (e durableEvents) At(index int) *session.Event {
	if index < 0 || index >= len(e) {
		return nil
	}
	return e[index]
}

var _ session.Service = (*AgentSessionService)(nil)
var _ session.Session = (*durableSession)(nil)
var _ session.State = (*durableState)(nil)
var _ session.Events = durableEvents(nil)

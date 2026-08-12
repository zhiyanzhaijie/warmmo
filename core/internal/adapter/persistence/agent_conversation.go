package persistence

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	appharness "warmmo/core/internal/application/harness"
)

const (
	conversationContextTurns = 24
	conversationLineLimit    = 2 * 1024
)

// AgentConversationStore persists the bounded, canvas-scoped conversation
// projection. Full model/tool events remain in AgentSessionService.
type AgentConversationStore struct {
	database *gorm.DB
}

func NewAgentConversationStore(database *Database) *AgentConversationStore {
	if database == nil {
		return &AgentConversationStore{}
	}
	return &AgentConversationStore{database: database.DB}
}

func (s *AgentConversationStore) AppendTurn(ctx context.Context, turn appharness.ConversationTurn) error {
	if s == nil || s.database == nil {
		return errors.New("agent conversation store is not configured")
	}
	if strings.TrimSpace(turn.ID) == "" || strings.TrimSpace(turn.WorkID) == "" {
		return errors.New("conversation turn ID and work ID are required")
	}
	if strings.TrimSpace(turn.UserContent) == "" && strings.TrimSpace(turn.AssistantContent) == "" {
		return errors.New("conversation turn content is required")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	return s.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conversation := agentConversationModel{ID: turn.WorkID, WorkID: turn.WorkID, CreatedAt: now, UpdatedAt: now}
		if err := tx.Where("work_id = ?", turn.WorkID).First(&conversation).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&conversation).Error; err != nil {
				var existing agentConversationModel
				if loadErr := tx.Where("work_id = ?", turn.WorkID).First(&existing).Error; loadErr != nil {
					return fmt.Errorf("create agent conversation: %w", err)
				}
				conversation = existing
			}
		} else if err != nil {
			return fmt.Errorf("load agent conversation: %w", err)
		}

		model := agentConversationTurnModel{
			ID: turn.ID, ConversationID: conversation.ID, RunID: turn.RunID,
			SessionID: strings.TrimSpace(turn.SessionID), ProviderID: strings.TrimSpace(turn.ProviderID), ModelID: strings.TrimSpace(turn.ModelID),
			AgentID: turn.AgentID, AgentName: turn.AgentName,
			UserContent: turn.UserContent, AssistantContent: turn.AssistantContent,
			Status: string(turn.Status), InputTokens: turn.Usage.InputTokens, CachedInputTokens: turn.Usage.CachedInputTokens, OutputTokens: turn.Usage.OutputTokens, CreatedAt: turn.CreatedAt,
		}
		if model.CreatedAt.IsZero() {
			model.CreatedAt = now
		}
		if err := tx.Create(&model).Error; err != nil {
			var existing agentConversationTurnModel
			if loadErr := tx.First(&existing, "id = ?", turn.ID).Error; loadErr != nil {
				return fmt.Errorf("append conversation turn: %w", err)
			}
			return nil
		}
		if err := tx.Model(&agentConversationModel{}).Where("id = ?", conversation.ID).
			Updates(map[string]any{"updated_at": now}).Error; err != nil {
			return fmt.Errorf("update agent conversation: %w", err)
		}
		return nil
	})
}

func (s *AgentConversationStore) ListSessions(ctx context.Context, workID string, limit int) (appharness.ConversationSnapshot, error) {
	snapshot := appharness.ConversationSnapshot{WorkID: strings.TrimSpace(workID), Sessions: []appharness.ConversationSession{}}
	if s == nil || s.database == nil {
		return snapshot, errors.New("agent conversation store is not configured")
	}
	if snapshot.WorkID == "" {
		return snapshot, errors.New("work ID is required")
	}
	if limit <= 0 {
		limit = 20
	}
	var conversation agentConversationModel
	if err := s.database.WithContext(ctx).Where("work_id = ?", snapshot.WorkID).First(&conversation).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return snapshot, nil
	} else if err != nil {
		return snapshot, fmt.Errorf("load agent conversation: %w", err)
	}
	var turns []agentConversationTurnModel
	if err := s.database.WithContext(ctx).Where("conversation_id = ?", conversation.ID).
		Order("created_at DESC, id DESC").Limit(limit * conversationContextTurns).Find(&turns).Error; err != nil {
		return snapshot, fmt.Errorf("load agent conversation sessions: %w", err)
	}
	groups := make(map[string][]agentConversationTurnModel)
	for _, turn := range turns {
		sessionID := strings.TrimSpace(turn.SessionID)
		if sessionID == "" {
			sessionID = "legacy"
		}
		groups[sessionID] = append(groups[sessionID], turn)
	}
	type sessionWithUpdated struct {
		session appharness.ConversationSession
		updated time.Time
	}
	grouped := make([]sessionWithUpdated, 0, len(groups))
	for sessionID, sessionTurns := range groups {
		if len(sessionTurns) == 0 {
			continue
		}
		// Query order is newest first; reverse the durable projection for chat display.
		sort.SliceStable(sessionTurns, func(i, j int) bool { return sessionTurns[i].CreatedAt.Before(sessionTurns[j].CreatedAt) })
		first := sessionTurns[0]
		createdAt, updatedAt := first.CreatedAt, first.CreatedAt
		latest := first
		converted := make([]appharness.ConversationTurn, 0, len(sessionTurns))
		for _, turn := range sessionTurns {
			if turn.CreatedAt.Before(createdAt) {
				createdAt = turn.CreatedAt
			}
			if turn.CreatedAt.After(updatedAt) {
				updatedAt = turn.CreatedAt
				latest = turn
			}
			converted = append(converted, appharness.ConversationTurn{
				ID: turn.ID, WorkID: snapshot.WorkID, SessionID: sessionID, RunID: turn.RunID,
				AgentID: turn.AgentID, AgentName: turn.AgentName, ProviderID: turn.ProviderID, ModelID: turn.ModelID,
				UserContent: turn.UserContent, AssistantContent: turn.AssistantContent, Status: appharness.TurnStatus(turn.Status),
				Usage: appharness.Usage{InputTokens: turn.InputTokens, CachedInputTokens: turn.CachedInputTokens, OutputTokens: turn.OutputTokens}, CreatedAt: turn.CreatedAt,
			})
		}
		title := strings.TrimSpace(first.UserContent)
		if title == "" {
			title = "画布对话"
		}
		if len([]rune(title)) > 48 {
			title = string([]rune(title)[:48]) + "…"
		}
		grouped = append(grouped, sessionWithUpdated{session: appharness.ConversationSession{
			ID: sessionID, WorkID: snapshot.WorkID, Title: title, ProviderID: latest.ProviderID, ModelID: latest.ModelID,
			LatestUsage: appharness.Usage{InputTokens: latest.InputTokens, CachedInputTokens: latest.CachedInputTokens, OutputTokens: latest.OutputTokens},
			TurnCount:   len(converted), CreatedAt: createdAt, UpdatedAt: updatedAt, Turns: converted,
		}, updated: updatedAt})
	}
	sort.SliceStable(grouped, func(i, j int) bool { return grouped[i].updated.After(grouped[j].updated) })
	if len(grouped) > limit {
		grouped = grouped[:limit]
	}
	snapshot.Sessions = make([]appharness.ConversationSession, len(grouped))
	for i, entry := range grouped {
		snapshot.Sessions[i] = entry.session
	}
	return snapshot, nil
}

func (s *AgentConversationStore) BuildContext(ctx context.Context, workID string, maxBytes int) (string, error) {
	return s.buildContext(ctx, workID, "", maxBytes)
}

func (s *AgentConversationStore) BuildSessionContext(ctx context.Context, workID, sessionID string, maxBytes int) (string, error) {
	return s.buildContext(ctx, workID, strings.TrimSpace(sessionID), maxBytes)
}

func (s *AgentConversationStore) buildContext(ctx context.Context, workID, sessionID string, maxBytes int) (string, error) {
	if s == nil || s.database == nil {
		return "", errors.New("agent conversation store is not configured")
	}
	if strings.TrimSpace(workID) == "" || maxBytes <= 0 {
		return "", nil
	}
	var conversation agentConversationModel
	if err := s.database.WithContext(ctx).Where("work_id = ?", workID).First(&conversation).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("load agent conversation context: %w", err)
	}
	var turns []agentConversationTurnModel
	query := s.database.WithContext(ctx).Where("conversation_id = ?", conversation.ID)
	if sessionID != "" {
		query = query.Where("session_id = ?", sessionID)
	}
	if err := query.
		Order("created_at DESC, id DESC").Limit(conversationContextTurns).Find(&turns).Error; err != nil {
		return "", fmt.Errorf("load agent conversation turns: %w", err)
	}
	if len(turns) == 0 {
		return "", nil
	}
	entries := make([]string, 0, len(turns))
	header := "# Canvas Conversation\nPrior user-facing turns for this canvas. Current Canvas context is authoritative.\n"
	used := len(header)
	for _, turn := range turns {
		lines := make([]string, 0, 2)
		if text := truncateStorySpineDatabaseContext(strings.TrimSpace(turn.UserContent), conversationLineLimit); text != "" {
			lines = append(lines, "User: "+text)
		}
		if text := truncateStorySpineDatabaseContext(strings.TrimSpace(turn.AssistantContent), conversationLineLimit); text != "" {
			lines = append(lines, "Agent ("+strings.TrimSpace(turn.AgentName)+"): "+text)
		}
		entry := strings.Join(lines, "\n")
		if entry == "" || used+len(entry)+1 > maxBytes {
			continue
		}
		entries = append(entries, entry)
		used += len(entry) + 1
	}
	var builder strings.Builder
	builder.WriteString(header)
	for index := len(entries) - 1; index >= 0; index-- {
		builder.WriteString(entries[index])
		builder.WriteByte('\n')
	}
	return strings.TrimSpace(builder.String()), nil
}

var _ appharness.ConversationStore = (*AgentConversationStore)(nil)
var _ appharness.SessionConversationStore = (*AgentConversationStore)(nil)

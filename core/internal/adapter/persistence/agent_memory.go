package persistence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	appharness "warmmo/core/internal/application/harness"
)

const maxMemoryContentBytes = 64 * 1024

type AgentMemoryStore struct {
	database *gorm.DB
}

func NewAgentMemoryStore(database *Database) *AgentMemoryStore {
	if database == nil {
		return &AgentMemoryStore{}
	}
	return &AgentMemoryStore{database: database.DB}
}

func (s *AgentMemoryStore) Remember(ctx context.Context, memory appharness.MemoryRecord) (appharness.MemoryRecord, error) {
	if s == nil || s.database == nil {
		return appharness.MemoryRecord{}, errors.New("agent memory store is not configured")
	}
	memory.WorkID = strings.TrimSpace(memory.WorkID)
	memory.Kind = strings.TrimSpace(memory.Kind)
	memory.Content = strings.TrimSpace(memory.Content)
	if memory.WorkID == "" || memory.Kind == "" || memory.Content == "" {
		return appharness.MemoryRecord{}, errors.New("memory work, kind and content are required")
	}
	if len(memory.Content) > maxMemoryContentBytes {
		return appharness.MemoryRecord{}, fmt.Errorf("memory content exceeds %d bytes", maxMemoryContentBytes)
	}
	if memory.ID == "" {
		memory.ID = uuid.NewString()
	}
	if memory.ContentHash == "" {
		digest := sha256.Sum256([]byte(memory.Kind + "\x00" + memory.Content))
		memory.ContentHash = hex.EncodeToString(digest[:])
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	model := agentMemoryModel{
		ID: memory.ID, WorkID: memory.WorkID, Kind: memory.Kind, Content: memory.Content,
		SourceRunID: memory.SourceRunID, SourceArtifactID: memory.SourceArtifactID,
		ContentHash: memory.ContentHash, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.database.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "work_id"}, {Name: "content_hash"}},
		DoNothing: true,
	}).Create(&model).Error; err != nil {
		return appharness.MemoryRecord{}, fmt.Errorf("remember agent memory: %w", err)
	}
	var stored agentMemoryModel
	if err := s.database.WithContext(ctx).Where("work_id = ? AND content_hash = ?", memory.WorkID, memory.ContentHash).First(&stored).Error; err != nil {
		return appharness.MemoryRecord{}, fmt.Errorf("reload agent memory: %w", err)
	}
	return memoryFromModel(stored), nil
}

func (s *AgentMemoryStore) Recall(ctx context.Context, query appharness.MemoryRecallQuery) ([]appharness.MemoryRecord, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("agent memory store is not configured")
	}
	query.WorkID = strings.TrimSpace(query.WorkID)
	if query.WorkID == "" {
		return nil, errors.New("memory recall work ID is required")
	}
	if query.Limit <= 0 {
		query.Limit = 6
	}
	if query.Limit > 20 {
		query.Limit = 20
	}
	var models []agentMemoryModel
	if err := s.database.WithContext(ctx).Where("work_id = ?", query.WorkID).
		Order("updated_at DESC").Limit(256).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("load recall candidates: %w", err)
	}
	terms := memoryTerms(query.Query)
	type ranked struct {
		model agentMemoryModel
		score int
	}
	rankedMemories := make([]ranked, 0, len(models))
	for _, model := range models {
		normalized := normalizeMemoryText(model.Content)
		score := 0
		for _, term := range terms {
			if strings.Contains(normalized, term) {
				score++
			}
		}
		if len(terms) > 0 && score == 0 {
			continue
		}
		rankedMemories = append(rankedMemories, ranked{model: model, score: score})
	}
	sort.SliceStable(rankedMemories, func(i, j int) bool {
		if rankedMemories[i].score != rankedMemories[j].score {
			return rankedMemories[i].score > rankedMemories[j].score
		}
		return rankedMemories[i].model.UpdatedAt.After(rankedMemories[j].model.UpdatedAt)
	})
	if len(rankedMemories) > query.Limit {
		rankedMemories = rankedMemories[:query.Limit]
	}
	result := make([]appharness.MemoryRecord, len(rankedMemories))
	for index, current := range rankedMemories {
		result[index] = memoryFromModel(current.model)
	}
	return result, nil
}

func (s *AgentMemoryStore) Load(ctx context.Context, workID string, ids []string) ([]appharness.MemoryRecord, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("agent memory store is not configured")
	}
	workID = strings.TrimSpace(workID)
	if workID == "" {
		return nil, errors.New("memory work ID is required")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 20 {
		return nil, errors.New("cannot load more than 20 memories")
	}
	var models []agentMemoryModel
	if err := s.database.WithContext(ctx).Where("work_id = ? AND id IN ?", workID, ids).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("load agent memories: %w", err)
	}
	byID := make(map[string]appharness.MemoryRecord, len(models))
	for _, model := range models {
		byID[model.ID] = memoryFromModel(model)
	}
	result := make([]appharness.MemoryRecord, 0, len(ids))
	for _, id := range ids {
		memory, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: %s", appharness.ErrMemoryNotFound, id)
		}
		result = append(result, memory)
	}
	return result, nil
}

func memoryTerms(value string) []string {
	normalized := normalizeMemoryText(value)
	seen := make(map[string]struct{})
	terms := make([]string, 0)
	for _, field := range strings.Fields(normalized) {
		if len([]rune(field)) < 2 {
			continue
		}
		if _, ok := seen[field]; !ok {
			seen[field] = struct{}{}
			terms = append(terms, field)
			if len(terms) >= 64 {
				return terms
			}
		}
	}
	runes := []rune(strings.ReplaceAll(normalized, " ", ""))
	for index := 0; index+2 <= len(runes); index++ {
		term := string(runes[index : index+2])
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
		if len(terms) >= 64 {
			break
		}
	}
	return terms
}

func normalizeMemoryText(value string) string {
	return strings.ToLower(strings.Map(func(current rune) rune {
		if unicode.IsLetter(current) || unicode.IsNumber(current) {
			return current
		}
		return ' '
	}, value))
}

func memoryFromModel(model agentMemoryModel) appharness.MemoryRecord {
	return appharness.MemoryRecord{
		ID: model.ID, WorkID: model.WorkID, Kind: model.Kind, Content: model.Content,
		SourceRunID: model.SourceRunID, SourceArtifactID: model.SourceArtifactID,
		ContentHash: model.ContentHash, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}

var _ appharness.MemoryStore = (*AgentMemoryStore)(nil)

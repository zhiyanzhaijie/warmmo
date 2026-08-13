package adk

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/genai"

	appharness "warmmo/core/internal/application/harness"
)

const (
	maxRecallBytes         = 6 * 1024
	maxAmbientContextBytes = 12 * 1024
)

type requestContext struct {
	policy                appharness.ContextPolicy
	memory                appharness.MemoryPolicy
	memories              appharness.MemoryStore
	conversation          appharness.ConversationStore
	context               appharness.ContextProvider
	workID                string
	conversationSessionID string
	query                 string
	once                  sync.Once
	blocks                []*genai.Content
	loadErr               error
}

func recallQuery(request LLMTurnRequest) string {
	query := request.Prompt
	if request.Resume == nil {
		return query
	}
	if answer, ok := request.Resume.Response["answer"].(string); ok && strings.TrimSpace(answer) != "" {
		query += "\n" + answer
	}
	return query
}

func (c *requestContext) beforeModel(ctx agent.CallbackContext, request *model.LLMRequest) (*model.LLMResponse, error) {
	if request == nil {
		return nil, errors.New("LLM request is required")
	}
	if c.policy.ReservedOutputTokens > math.MaxInt32 {
		return nil, errors.New("reserved output token budget exceeds the model request limit")
	}
	if request.Config == nil {
		request.Config = &genai.GenerateContentConfig{}
	}
	request.Config.MaxOutputTokens = int32(c.policy.ReservedOutputTokens)

	c.once.Do(func() {
		c.blocks, c.loadErr = c.loadBlocks(ctx)
	})
	if c.loadErr != nil {
		return nil, c.loadErr
	}
	if len(c.blocks) > 0 && len(request.Contents) > 0 && !sameContent(request.Contents[0], c.blocks[0]) {
		request.Contents = append(c.blocks, request.Contents...)
	}
	return nil, nil
}

func (c *requestContext) loadBlocks(ctx context.Context) ([]*genai.Content, error) {
	blocks := make([]*genai.Content, 0, 2)
	if recalled, err := c.recall(ctx); err != nil {
		return nil, err
	} else if recalled != "" {
		blocks = append(blocks, genai.NewContentFromText(recalled, genai.RoleUser))
	}
	if ambient, err := c.ambient(ctx); err != nil {
		return nil, err
	} else if ambient != "" {
		blocks = append(blocks, genai.NewContentFromText(ambient, genai.RoleUser))
	}
	return blocks, nil
}

func sameContent(left, right *genai.Content) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Role == right.Role && len(left.Parts) == len(right.Parts) && len(left.Parts) > 0 && left.Parts[0].Text == right.Parts[0].Text
}

func (c requestContext) recall(ctx context.Context) (string, error) {
	if !c.memory.Recall || c.memories == nil || strings.TrimSpace(c.workID) == "" {
		return "", nil
	}
	memories, err := c.memories.Recall(ctx, appharness.MemoryRecallQuery{
		WorkID: c.workID, Query: c.query, Limit: 6,
	})
	if err != nil {
		return "", fmt.Errorf("recall agent memory: %w", err)
	}
	var result strings.Builder
	result.WriteString("# Recalled Memory\nThese notes are non-authoritative. Verify them against current context.\n")
	for _, memory := range memories {
		line := fmt.Sprintf("- [%s:%s] %s\n", memory.Kind, memory.ID, strings.TrimSpace(memory.Content))
		if result.Len()+len(line) > maxRecallBytes {
			break
		}
		result.WriteString(line)
	}
	if len(memories) == 0 {
		return "", nil
	}
	return strings.TrimSpace(result.String()), nil
}

func (c requestContext) ambient(ctx context.Context) (string, error) {
	blocks := make([]string, 0, 2)
	if c.context != nil {
		value, err := c.context.BuildContext(ctx, c.workID, maxAmbientContextBytes/2)
		if err != nil {
			return "", fmt.Errorf("build authoritative work context: %w", err)
		}
		if strings.TrimSpace(value) != "" {
			blocks = append(blocks, value)
		}
	}
	if c.conversation != nil {
		var value string
		var err error
		if store, ok := c.conversation.(appharness.SessionConversationStore); ok && strings.TrimSpace(c.conversationSessionID) != "" {
			value, err = store.BuildSessionContext(ctx, c.workID, c.conversationSessionID, maxAmbientContextBytes/2)
		} else {
			value, err = c.conversation.BuildContext(ctx, c.workID, maxAmbientContextBytes/2)
		}
		if err != nil {
			return "", fmt.Errorf("build agent conversation context: %w", err)
		}
		if strings.TrimSpace(value) != "" {
			blocks = append(blocks, value)
		}
	}
	return strings.Join(blocks, "\n\n"), nil
}

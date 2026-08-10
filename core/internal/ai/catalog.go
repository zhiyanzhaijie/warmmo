package ai

import (
	"errors"
	"time"
)

var (
	ErrAPIKeyRequired                = errors.New("api key is required")
	ErrProviderConfigurationNotFound = errors.New("provider configuration not found")
)

type ModelCapability string

const (
	ModelCapabilityText          ModelCapability = "text"
	ModelCapabilityImage         ModelCapability = "image"
	ModelCapabilityEmbedding     ModelCapability = "embedding"
	CanonicalEmbeddingProviderID                 = "siliconflow"
	CanonicalEmbeddingModelID                    = "Qwen/Qwen3-Embedding-0.6B"
	CanonicalEmbeddingDimensions                 = 1024
)

type ModelDefinition struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Capability  ModelCapability `json:"capability"`
	Description string          `json:"description"`
	Dimensions  int             `json:"dimensions,omitempty"`
}

type ProviderDefinition struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	DefaultBaseURL string            `json:"defaultBaseUrl"`
	Models         []ModelDefinition `json:"models"`
}

type ProviderConfiguration struct {
	ID               string    `json:"id"`
	ProviderID       string    `json:"providerId"`
	BaseURL          string    `json:"baseUrl"`
	ModelIDs         []string  `json:"modelIds"`
	APIKeyConfigured bool      `json:"apiKeyConfigured"`
	APIKeyHint       string    `json:"apiKeyHint,omitempty"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type SaveProviderConfiguration struct {
	ProviderID string   `json:"providerId"`
	BaseURL    string   `json:"baseUrl"`
	ModelIDs   []string `json:"modelIds"`
	APIKey     string   `json:"apiKey"`
}

type TestProviderConfiguration struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	ModelID string `json:"modelId,omitempty"`
}

type ProviderTestResult struct {
	Valid     bool   `json:"valid"`
	Message   string `json:"message"`
	LatencyMS int64  `json:"latencyMs"`
}

type EnabledModel struct {
	ProviderID   string          `json:"providerId"`
	ProviderName string          `json:"providerName"`
	ModelID      string          `json:"modelId"`
	ModelName    string          `json:"modelName"`
	Capability   ModelCapability `json:"capability"`
}

func DefaultCatalog() []ProviderDefinition {
	return []ProviderDefinition{
		{
			ID: "deepseek", Name: "DeepSeek", DefaultBaseURL: "https://api.deepseek.com",
			Models: []ModelDefinition{
				{ID: "deepseek-chat", Name: "DeepSeek Chat", Capability: ModelCapabilityText, Description: "通用写作、改写与长文本生成"},
				{ID: "deepseek-reasoner", Name: "DeepSeek Reasoner", Capability: ModelCapabilityText, Description: "适合推理、剧情规划与复杂关系分析"},
			},
		},
		{
			ID: "openai", Name: "OpenAI", DefaultBaseURL: "https://api.openai.com/v1",
			Models: []ModelDefinition{
				{ID: "gpt-5", Name: "GPT-5", Capability: ModelCapabilityText, Description: "高质量文本生成与创作推理"},
				{ID: "gpt-5-mini", Name: "GPT-5 mini", Capability: ModelCapabilityText, Description: "更快、更经济的日常文本生成"},
				{ID: "gpt-image-1", Name: "GPT Image 1", Capability: ModelCapabilityImage, Description: "角色、场景与概念图生成"},
			},
		},
		{
			ID: CanonicalEmbeddingProviderID, Name: "硅基流动", DefaultBaseURL: "https://api.siliconflow.cn/v1",
			Models: []ModelDefinition{
				{ID: CanonicalEmbeddingModelID, Name: "Qwen3 Embedding 0.6B", Capability: ModelCapabilityEmbedding, Description: "中文故事上下文检索（固定 1024 维）", Dimensions: CanonicalEmbeddingDimensions},
			},
		},
	}
}

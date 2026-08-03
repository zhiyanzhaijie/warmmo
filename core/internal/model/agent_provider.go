package model

import "time"

type ModelCapability string

const (
	ModelCapabilityText  ModelCapability = "text"
	ModelCapabilityImage ModelCapability = "image"
)

type ModelDefinition struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Capability  ModelCapability `json:"capability"`
	Description string          `json:"description"`
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

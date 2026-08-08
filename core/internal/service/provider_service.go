package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"warmnote/core/internal/model"
	"warmnote/core/internal/repository"
)

var (
	ErrInvalidProviderConfiguration = errors.New("invalid provider configuration")
	ErrProviderNotFound             = errors.New("provider not found")
)

type ProviderService struct {
	repository *repository.ProviderRepository
	catalog    []model.ProviderDefinition
	httpClient *http.Client
}

func NewProviderService(providerRepository *repository.ProviderRepository) *ProviderService {
	return &ProviderService{
		repository: providerRepository,
		catalog:    defaultModelCatalog(),
		httpClient: &http.Client{Timeout: 8 * time.Second},
	}
}

func (s *ProviderService) Catalog() []model.ProviderDefinition {
	return s.catalog
}

func (s *ProviderService) ListConfigurations() ([]model.ProviderConfiguration, error) {
	return s.repository.List()
}

func (s *ProviderService) SaveConfiguration(input model.SaveProviderConfiguration) (model.ProviderConfiguration, error) {
	input.ProviderID = strings.TrimSpace(input.ProviderID)
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.APIKey = strings.TrimSpace(input.APIKey)

	provider, ok := s.findProvider(input.ProviderID)
	if !ok {
		return model.ProviderConfiguration{}, ErrProviderNotFound
	}
	if provider.ID == model.CanonicalEmbeddingProviderID {
		input.BaseURL = provider.DefaultBaseURL
	} else if input.BaseURL == "" {
		input.BaseURL = provider.DefaultBaseURL
	}
	if err := validateBaseURL(input.BaseURL); err != nil {
		return model.ProviderConfiguration{}, fmt.Errorf("%w: baseUrl must be an absolute URL", ErrInvalidProviderConfiguration)
	}
	if len(input.ModelIDs) == 0 {
		return model.ProviderConfiguration{}, fmt.Errorf("%w: select at least one model", ErrInvalidProviderConfiguration)
	}

	uniqueModelIDs := make(map[string]struct{}, len(input.ModelIDs))
	for _, modelID := range input.ModelIDs {
		modelID = strings.TrimSpace(modelID)
		candidate, ok := s.findModel(provider, modelID)
		if !ok || (candidate.Capability == model.ModelCapabilityEmbedding && candidate.ID != model.CanonicalEmbeddingModelID) {
			return model.ProviderConfiguration{}, fmt.Errorf("%w: model %q is unavailable", ErrInvalidProviderConfiguration, modelID)
		}
		uniqueModelIDs[modelID] = struct{}{}
	}
	input.ModelIDs = input.ModelIDs[:0]
	for modelID := range uniqueModelIDs {
		input.ModelIDs = append(input.ModelIDs, modelID)
	}
	sort.Strings(input.ModelIDs)

	configuration, err := s.repository.Save(input)
	if err != nil {
		if errors.Is(err, repository.ErrAPIKeyRequired) {
			return model.ProviderConfiguration{}, fmt.Errorf("%w: apiKey is required", ErrInvalidProviderConfiguration)
		}
		return model.ProviderConfiguration{}, err
	}
	return configuration, nil
}

func (s *ProviderService) TestConfiguration(ctx context.Context, providerID string, input model.TestProviderConfiguration) (model.ProviderTestResult, error) {
	providerID = strings.TrimSpace(providerID)
	provider, ok := s.findProvider(providerID)
	if !ok {
		return model.ProviderTestResult{}, ErrProviderNotFound
	}

	baseURL := strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	if provider.ID == model.CanonicalEmbeddingProviderID || baseURL == "" {
		baseURL = provider.DefaultBaseURL
	}
	if err := validateBaseURL(baseURL); err != nil {
		return model.ProviderTestResult{}, fmt.Errorf("%w: baseUrl must be an absolute HTTP URL", ErrInvalidProviderConfiguration)
	}

	apiKey := strings.TrimSpace(input.APIKey)
	if apiKey == "" {
		var err error
		apiKey, err = s.repository.GetAPIKey(providerID)
		if errors.Is(err, repository.ErrProviderConfigurationNotFound) {
			return model.ProviderTestResult{}, fmt.Errorf("%w: apiKey is required", ErrInvalidProviderConfiguration)
		}
		if err != nil {
			return model.ProviderTestResult{}, err
		}
	}
	if input.ModelID != "" {
		candidate, ok := s.findModel(provider, strings.TrimSpace(input.ModelID))
		if !ok {
			return model.ProviderTestResult{}, fmt.Errorf("%w: model %q is unavailable", ErrInvalidProviderConfiguration, input.ModelID)
		}
		if candidate.Capability == model.ModelCapabilityEmbedding {
			if candidate.ID != model.CanonicalEmbeddingModelID {
				return model.ProviderTestResult{}, fmt.Errorf("%w: only the canonical embedding model is supported", ErrInvalidProviderConfiguration)
			}
			return s.testEmbeddingConfiguration(ctx, baseURL, apiKey, candidate.ID)
		}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return model.ProviderTestResult{}, fmt.Errorf("create provider test request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)

	startedAt := time.Now()
	response, err := s.httpClient.Do(request)
	latencyMS := time.Since(startedAt).Milliseconds()
	if err != nil {
		return model.ProviderTestResult{}, fmt.Errorf("request provider: %w", err)
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices:
		return model.ProviderTestResult{Valid: true, Message: "API Key 有效，Provider 连接正常", LatencyMS: latencyMS}, nil
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return model.ProviderTestResult{Valid: false, Message: "API Key 无效或没有访问权限", LatencyMS: latencyMS}, nil
	default:
		return model.ProviderTestResult{
			Valid: false, Message: fmt.Sprintf("Provider 返回 HTTP %d", response.StatusCode), LatencyMS: latencyMS,
		}, nil
	}
}

func (s *ProviderService) DeleteConfiguration(providerID string) error {
	if _, ok := s.findProvider(providerID); !ok {
		return ErrProviderNotFound
	}
	return s.repository.Delete(providerID)
}

func (s *ProviderService) EnabledModels() ([]model.EnabledModel, error) {
	configurations, err := s.repository.List()
	if err != nil {
		return nil, err
	}

	models := make([]model.EnabledModel, 0)
	for _, configuration := range configurations {
		provider, ok := s.findProvider(configuration.ProviderID)
		if !ok || !configuration.APIKeyConfigured {
			continue
		}
		enabledIDs := make(map[string]struct{}, len(configuration.ModelIDs))
		for _, modelID := range configuration.ModelIDs {
			enabledIDs[modelID] = struct{}{}
		}
		for _, candidate := range provider.Models {
			if _, ok := enabledIDs[candidate.ID]; !ok {
				continue
			}
			models = append(models, model.EnabledModel{
				ProviderID: provider.ID, ProviderName: provider.Name,
				ModelID: candidate.ID, ModelName: candidate.Name, Capability: candidate.Capability,
			})
		}
	}
	return models, nil
}

func (s *ProviderService) findProvider(providerID string) (model.ProviderDefinition, bool) {
	for _, provider := range s.catalog {
		if provider.ID == providerID {
			return provider, true
		}
	}
	return model.ProviderDefinition{}, false
}

func (s *ProviderService) findModel(provider model.ProviderDefinition, modelID string) (model.ModelDefinition, bool) {
	for _, candidate := range provider.Models {
		if candidate.ID == modelID {
			return candidate, true
		}
	}
	return model.ModelDefinition{}, false
}

func (s *ProviderService) testEmbeddingConfiguration(ctx context.Context, baseURL, apiKey, modelID string) (model.ProviderTestResult, error) {
	payload, err := json.Marshal(map[string]any{
		"model": modelID,
		"input": "Warmnote embedding connection test",
		"dimensions": model.CanonicalEmbeddingDimensions,
		"encoding_format": "float",
	})
	if err != nil {
		return model.ProviderTestResult{}, fmt.Errorf("encode embedding test request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return model.ProviderTestResult{}, fmt.Errorf("create embedding test request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)
	startedAt := time.Now()
	response, err := s.httpClient.Do(request)
	latencyMS := time.Since(startedAt).Milliseconds()
	if err != nil {
		return model.ProviderTestResult{}, fmt.Errorf("request embedding provider: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return model.ProviderTestResult{Valid: false, Message: "API Key 无效或没有访问权限", LatencyMS: latencyMS}, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return model.ProviderTestResult{Valid: false, Message: fmt.Sprintf("Embedding Provider 返回 HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body))), LatencyMS: latencyMS}, nil
	}
	var decoded struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(&decoded); err != nil {
		return model.ProviderTestResult{Valid: false, Message: "Embedding Provider 返回内容无法解析", LatencyMS: latencyMS}, nil
	}
	if len(decoded.Data) != 1 || len(decoded.Data[0].Embedding) != model.CanonicalEmbeddingDimensions {
		actualDimensions := 0
		if len(decoded.Data) == 1 {
			actualDimensions = len(decoded.Data[0].Embedding)
		}
		return model.ProviderTestResult{Valid: false, Message: fmt.Sprintf("Embedding 返回维度为 %d，需要 %d", actualDimensions, model.CanonicalEmbeddingDimensions), LatencyMS: latencyMS}, nil
	}
	return model.ProviderTestResult{Valid: true, Message: "Embedding API Key 有效，模型连接正常（1024 维）", LatencyMS: latencyMS}, nil
}

func validateBaseURL(baseURL string) error {
	parsedBaseURL, err := url.ParseRequestURI(baseURL)
	if err != nil || parsedBaseURL.Host == "" {
		return errors.New("invalid base url")
	}
	if parsedBaseURL.Scheme != "http" && parsedBaseURL.Scheme != "https" {
		return errors.New("unsupported base url scheme")
	}
	return nil
}

func defaultModelCatalog() []model.ProviderDefinition {
	return []model.ProviderDefinition{
		{
			ID: "deepseek", Name: "DeepSeek", DefaultBaseURL: "https://api.deepseek.com",
			Models: []model.ModelDefinition{
				{ID: "deepseek-chat", Name: "DeepSeek Chat", Capability: model.ModelCapabilityText, Description: "通用写作、改写与长文本生成"},
				{ID: "deepseek-reasoner", Name: "DeepSeek Reasoner", Capability: model.ModelCapabilityText, Description: "适合推理、剧情规划与复杂关系分析"},
			},
		},
		{
			ID: "openai", Name: "OpenAI", DefaultBaseURL: "https://api.openai.com/v1",
			Models: []model.ModelDefinition{
				{ID: "gpt-5", Name: "GPT-5", Capability: model.ModelCapabilityText, Description: "高质量文本生成与创作推理"},
				{ID: "gpt-5-mini", Name: "GPT-5 mini", Capability: model.ModelCapabilityText, Description: "更快、更经济的日常文本生成"},
				{ID: "gpt-image-1", Name: "GPT Image 1", Capability: model.ModelCapabilityImage, Description: "角色、场景与概念图生成"},
			},
		},
		{
			ID: model.CanonicalEmbeddingProviderID, Name: "硅基流动", DefaultBaseURL: "https://api.siliconflow.cn/v1",
			Models: []model.ModelDefinition{
				{ID: model.CanonicalEmbeddingModelID, Name: "Qwen3 Embedding 0.6B", Capability: model.ModelCapabilityEmbedding, Description: "中文故事上下文检索（固定 1024 维）", Dimensions: model.CanonicalEmbeddingDimensions},
			},
		},
	}
}

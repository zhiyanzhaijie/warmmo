package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"warmmo/core/internal/domain/ai"
)

var (
	ErrInvalidProviderConfiguration  = errors.New("invalid provider configuration")
	ErrProviderNotFound              = errors.New("provider not found")
	ErrProviderConfigurationNotFound = errors.New("provider configuration not found")
)

type ProviderStore interface {
	List() ([]ai.ProviderConfiguration, error)
	Save(ai.SaveProviderConfiguration) (ai.ProviderConfiguration, error)
	GetAPIKey(string) (string, error)
	Delete(string) error
}

type ProviderProbe interface {
	Test(context.Context, string, string, string, ai.ModelCapability) (ai.ProviderTestResult, error)
}

type ProviderService struct {
	store   ProviderStore
	probe   ProviderProbe
	catalog []ai.ProviderDefinition
}

func NewProviderService(store ProviderStore, probe ProviderProbe) *ProviderService {
	return &ProviderService{
		store: store, probe: probe, catalog: ai.DefaultCatalog(),
	}
}

func (s *ProviderService) Catalog() []ai.ProviderDefinition {
	return s.catalog
}

func (s *ProviderService) ListConfigurations() ([]ai.ProviderConfiguration, error) {
	return s.store.List()
}

func (s *ProviderService) SaveConfiguration(input ai.SaveProviderConfiguration) (ai.ProviderConfiguration, error) {
	input.ProviderID = strings.TrimSpace(input.ProviderID)
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.APIKey = strings.TrimSpace(input.APIKey)

	provider, ok := s.findProvider(input.ProviderID)
	if !ok {
		return ai.ProviderConfiguration{}, ErrProviderNotFound
	}
	if provider.ID == ai.CanonicalEmbeddingProviderID {
		input.BaseURL = provider.DefaultBaseURL
	} else if input.BaseURL == "" {
		input.BaseURL = provider.DefaultBaseURL
	}
	if err := validateBaseURL(input.BaseURL); err != nil {
		return ai.ProviderConfiguration{}, fmt.Errorf("%w: baseUrl must be an absolute URL", ErrInvalidProviderConfiguration)
	}
	if len(input.ModelIDs) == 0 {
		return ai.ProviderConfiguration{}, fmt.Errorf("%w: select at least one model", ErrInvalidProviderConfiguration)
	}

	uniqueModelIDs := make(map[string]struct{}, len(input.ModelIDs))
	for _, modelID := range input.ModelIDs {
		modelID = strings.TrimSpace(modelID)
		candidate, ok := s.findModel(provider, modelID)
		if !ok || (candidate.Capability == ai.ModelCapabilityEmbedding && candidate.ID != ai.CanonicalEmbeddingModelID) {
			return ai.ProviderConfiguration{}, fmt.Errorf("%w: model %q is unavailable", ErrInvalidProviderConfiguration, modelID)
		}
		uniqueModelIDs[modelID] = struct{}{}
	}
	input.ModelIDs = input.ModelIDs[:0]
	for modelID := range uniqueModelIDs {
		input.ModelIDs = append(input.ModelIDs, modelID)
	}
	sort.Strings(input.ModelIDs)

	configuration, err := s.store.Save(input)
	if err != nil {
		if errors.Is(err, ai.ErrAPIKeyRequired) {
			return ai.ProviderConfiguration{}, fmt.Errorf("%w: apiKey is required", ErrInvalidProviderConfiguration)
		}
		return ai.ProviderConfiguration{}, err
	}
	return configuration, nil
}

func (s *ProviderService) TestConfiguration(ctx context.Context, providerID string, input ai.TestProviderConfiguration) (ai.ProviderTestResult, error) {
	providerID = strings.TrimSpace(providerID)
	provider, ok := s.findProvider(providerID)
	if !ok {
		return ai.ProviderTestResult{}, ErrProviderNotFound
	}

	baseURL := strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	if provider.ID == ai.CanonicalEmbeddingProviderID || baseURL == "" {
		baseURL = provider.DefaultBaseURL
	}
	if err := validateBaseURL(baseURL); err != nil {
		return ai.ProviderTestResult{}, fmt.Errorf("%w: baseUrl must be an absolute HTTP URL", ErrInvalidProviderConfiguration)
	}

	apiKey := strings.TrimSpace(input.APIKey)
	if apiKey == "" {
		var err error
		apiKey, err = s.store.GetAPIKey(providerID)
		if errors.Is(err, ai.ErrProviderConfigurationNotFound) {
			return ai.ProviderTestResult{}, fmt.Errorf("%w: apiKey is required", ErrInvalidProviderConfiguration)
		}
		if err != nil {
			return ai.ProviderTestResult{}, err
		}
	}
	capability := ai.ModelCapabilityText
	modelID := strings.TrimSpace(input.ModelID)
	if modelID != "" {
		candidate, ok := s.findModel(provider, strings.TrimSpace(input.ModelID))
		if !ok {
			return ai.ProviderTestResult{}, fmt.Errorf("%w: model %q is unavailable", ErrInvalidProviderConfiguration, input.ModelID)
		}
		if candidate.Capability == ai.ModelCapabilityEmbedding {
			if candidate.ID != ai.CanonicalEmbeddingModelID {
				return ai.ProviderTestResult{}, fmt.Errorf("%w: only the canonical embedding model is supported", ErrInvalidProviderConfiguration)
			}
		}
		capability = candidate.Capability
		modelID = candidate.ID
	}
	if s.probe == nil {
		return ai.ProviderTestResult{}, InternalError("test provider configuration", errors.New("provider probe is not configured"))
	}
	result, err := s.probe.Test(ctx, baseURL, apiKey, modelID, capability)
	if err != nil {
		return ai.ProviderTestResult{}, newAppError(
			ErrorUpstream,
			"PROVIDER_UNREACHABLE",
			"无法连接 Provider，请检查 Base URL 和网络",
			"test provider configuration",
			err,
		)
	}
	return result, nil
}

func (s *ProviderService) DeleteConfiguration(providerID string) error {
	if _, ok := s.findProvider(providerID); !ok {
		return ErrProviderNotFound
	}
	err := s.store.Delete(providerID)
	if errors.Is(err, ai.ErrProviderConfigurationNotFound) {
		return ErrProviderConfigurationNotFound
	}
	return err
}

func (s *ProviderService) EnabledModels() ([]ai.EnabledModel, error) {
	configurations, err := s.store.List()
	if err != nil {
		return nil, err
	}

	models := make([]ai.EnabledModel, 0)
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
			models = append(models, ai.EnabledModel{
				ProviderID: provider.ID, ProviderName: provider.Name,
				ModelID: candidate.ID, ModelName: candidate.Name, Capability: candidate.Capability,
			})
		}
	}
	return models, nil
}

func (s *ProviderService) findProvider(providerID string) (ai.ProviderDefinition, bool) {
	for _, provider := range s.catalog {
		if provider.ID == providerID {
			return provider, true
		}
	}
	return ai.ProviderDefinition{}, false
}

func (s *ProviderService) findModel(provider ai.ProviderDefinition, modelID string) (ai.ModelDefinition, bool) {
	for _, candidate := range provider.Models {
		if candidate.ID == modelID {
			return candidate, true
		}
	}
	return ai.ModelDefinition{}, false
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

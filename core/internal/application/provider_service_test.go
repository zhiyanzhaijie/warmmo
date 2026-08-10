package application

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	aiprovider "warmmo/core/internal/adapter/agent/provider"
	"warmmo/core/internal/adapter/persistence"
	"warmmo/core/internal/domain/ai"
)

func TestProviderServiceTestsAPIKey(t *testing.T) {
	const validAPIKey = "sk-valid"
	providerServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/models" {
			http.NotFound(response, request)
			return
		}
		if request.Header.Get("Authorization") != "Bearer "+validAPIKey {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer providerServer.Close()

	providerRepository, err := persistence.NewProviderRepository(t.TempDir())
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	defer providerRepository.Close()
	providerService := NewProviderService(providerRepository, aiprovider.NewProbe(providerServer.Client()))

	validResult, err := providerService.TestConfiguration(context.Background(), "deepseek", ai.TestProviderConfiguration{
		BaseURL: providerServer.URL,
		APIKey:  validAPIKey,
	})
	if err != nil {
		t.Fatalf("test valid api key: %v", err)
	}
	if !validResult.Valid {
		t.Fatalf("expected valid api key result: %+v", validResult)
	}

	invalidResult, err := providerService.TestConfiguration(context.Background(), "deepseek", ai.TestProviderConfiguration{
		BaseURL: providerServer.URL,
		APIKey:  "sk-invalid",
	})
	if err != nil {
		t.Fatalf("test invalid api key: %v", err)
	}
	if invalidResult.Valid {
		t.Fatalf("expected invalid api key result: %+v", invalidResult)
	}
}

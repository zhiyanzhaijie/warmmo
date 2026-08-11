package persistence

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"warmmo/core/internal/domain/ai"
)

func TestProviderRepositoryEncryptsAndPreservesAPIKey(t *testing.T) {
	dataDirectory := t.TempDir()
	repository, err := NewProviderRepository(dataDirectory)
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Errorf("close repository: %v", err)
		}
	})

	const apiKey = "sk-test-plaintext-secret"
	configuration, err := repository.Save(ai.SaveProviderConfiguration{
		ProviderID: "deepseek",
		BaseURL:    "https://api.deepseek.com",
		ModelIDs:   []string{"deepseek-chat"},
		APIKey:     apiKey,
	})
	if err != nil {
		t.Fatalf("save configuration: %v", err)
	}
	if !configuration.APIKeyConfigured || configuration.APIKeyHint != "••••cret" {
		t.Fatalf("unexpected secret metadata: %+v", configuration)
	}

	entries, err := os.ReadDir(dataDirectory)
	if err != nil {
		t.Fatalf("read data directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == ".master-key" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dataDirectory, entry.Name()))
		if err != nil {
			t.Fatalf("read data file %s: %v", entry.Name(), err)
		}
		if bytes.Contains(data, []byte(apiKey)) {
			t.Fatalf("data file %s contains plaintext api key", entry.Name())
		}
	}
	decryptedAPIKey, err := repository.GetAPIKey("deepseek")
	if err != nil {
		t.Fatalf("decrypt api key: %v", err)
	}
	if decryptedAPIKey != apiKey {
		t.Fatalf("unexpected decrypted api key: %q", decryptedAPIKey)
	}

	if _, err := repository.Save(ai.SaveProviderConfiguration{
		ProviderID: "deepseek",
		BaseURL:    "https://api.deepseek.com/v1",
		ModelIDs:   []string{"deepseek-chat", "deepseek-reasoner"},
	}); err != nil {
		t.Fatalf("update configuration without replacing api key: %v", err)
	}

	configurations, err := repository.List()
	if err != nil {
		t.Fatalf("list configurations: %v", err)
	}
	if len(configurations) != 1 || !configurations[0].APIKeyConfigured || configurations[0].APIKeyHint != "••••cret" {
		t.Fatalf("api key metadata was not preserved: %+v", configurations)
	}
}

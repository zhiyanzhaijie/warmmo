package storage

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"warmmo/core/internal/ai"
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

func TestProviderRepositoryMigratesSectionDraftKinds(t *testing.T) {
	dataDirectory := t.TempDir()
	repository, err := NewProviderRepository(dataDirectory)
	if err != nil {
		t.Fatalf("create provider repository: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := repository.database.Exec(`
INSERT INTO canvas_nodes (id, work_id, revision, kind, title, content, x, y, created_at, updated_at)
VALUES ('node-1', 'work-1', 1, 'section-draft', '旧小节', '正文', 0, 0, ?, ?);
INSERT INTO agent_runs (id, work_id, status, prompt, target, target_node_id, provider_id, model_id, context_node_ids_json, created_at, updated_at)
VALUES ('run-1', 'work-1', 'completed', '写小节', 'section-draft', '', 'provider-1', 'model-1', '[]', ?, ?);
INSERT INTO agent_candidates (id, run_id, work_id, skill_id, skill_version, status, kind, title, content, x, y, accepted_node_id, created_at, decided_at)
VALUES ('candidate-1', 'run-1', 'work-1', 'chapter-drafting', '1.0.0', 'pending', 'section-draft', '旧候选', '正文', 0, 0, '', ?, '')`,
		now, now, now, now, now); err != nil {
		t.Fatalf("insert legacy section draft data: %v", err)
	}
	if _, err := repository.database.Exec("PRAGMA user_version = 9"); err != nil {
		t.Fatalf("set legacy schema version: %v", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatalf("close legacy repository: %v", err)
	}

	migrated, err := NewProviderRepository(dataDirectory)
	if err != nil {
		t.Fatalf("migrate provider repository: %v", err)
	}
	t.Cleanup(func() {
		if err := migrated.Close(); err != nil {
			t.Errorf("close migrated repository: %v", err)
		}
	})
	assertStoredKind(t, migrated.database, "SELECT kind FROM canvas_nodes WHERE id = 'node-1'", "chapter-section")
	assertStoredKind(t, migrated.database, "SELECT target FROM agent_runs WHERE id = 'run-1'", "chapter-section")
	assertStoredKind(t, migrated.database, "SELECT kind FROM agent_candidates WHERE id = 'candidate-1'", "chapter-section")
}

func assertStoredKind(t *testing.T, database *sql.DB, query, expected string) {
	t.Helper()
	var value string
	if err := database.QueryRow(query).Scan(&value); err != nil {
		t.Fatalf("read migrated kind: %v", err)
	}
	if value != expected {
		t.Fatalf("migrated kind = %q, want %q", value, expected)
	}
}

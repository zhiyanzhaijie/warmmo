package repository

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"warmnote/core/internal/model"
)

const (
	databaseFileName     = "warmnote.db"
	masterKeySize        = 32
	currentSchemaVersion = 3
	providerSchemaSQL    = `
CREATE TABLE IF NOT EXISTS agent_provider_configurations (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL UNIQUE,
    base_url TEXT NOT NULL,
    model_ids_json TEXT NOT NULL,
    secret_nonce BLOB NOT NULL,
    secret_ciphertext BLOB NOT NULL,
    api_key_hint TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_provider_configurations_updated_at
    ON agent_provider_configurations(updated_at DESC);`
	agentSchemaSQL = `
CREATE TABLE IF NOT EXISTS agent_runs (
    id TEXT PRIMARY KEY,
    work_id TEXT NOT NULL,
    status TEXT NOT NULL,
    prompt TEXT NOT NULL,
    target TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    context_node_ids_json TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_runs_work_created
    ON agent_runs(work_id, created_at DESC);
CREATE TABLE IF NOT EXISTS agent_run_events (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL,
    type TEXT NOT NULL,
    data_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(run_id, sequence)
);
CREATE INDEX IF NOT EXISTS idx_agent_run_events_run_sequence
    ON agent_run_events(run_id, sequence);
CREATE TABLE IF NOT EXISTS agent_candidates (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL UNIQUE REFERENCES agent_runs(id) ON DELETE CASCADE,
    work_id TEXT NOT NULL,
    skill_id TEXT NOT NULL,
    skill_version TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);`
	canvasSchemaSQL = `
CREATE TABLE IF NOT EXISTS canvas_nodes (
    id TEXT PRIMARY KEY,
    work_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    kind TEXT NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_canvas_nodes_work_created
    ON canvas_nodes(work_id, created_at);`
)

var (
	ErrAPIKeyRequired                = errors.New("api key is required")
	ErrProviderConfigurationNotFound = errors.New("provider configuration not found")
)

type ProviderRepository struct {
	database     *sql.DB
	databasePath string
	keyPath      string
}

type encryptedSecret struct {
	Nonce      []byte
	Ciphertext []byte
}

func NewProviderRepository(dataDirectory string) (*ProviderRepository, error) {
	if err := os.MkdirAll(dataDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(dataDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("secure data directory: %w", err)
	}

	databasePath := filepath.Join(dataDirectory, databaseFileName)
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	database.SetMaxOpenConns(1)

	repository := &ProviderRepository{
		database:     database,
		databasePath: databasePath,
		keyPath:      filepath.Join(dataDirectory, ".master-key"),
	}
	if err := repository.initialize(); err != nil {
		database.Close()
		return nil, err
	}
	return repository, nil
}

func (r *ProviderRepository) Close() error {
	return r.database.Close()
}

func (r *ProviderRepository) DatabasePath() string {
	return r.databasePath
}

func (r *ProviderRepository) List() ([]model.ProviderConfiguration, error) {
	rows, err := r.database.Query(`
SELECT id, provider_id, base_url, model_ids_json, secret_ciphertext, api_key_hint, updated_at
FROM agent_provider_configurations
ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query provider configurations: %w", err)
	}
	defer rows.Close()

	configurations := make([]model.ProviderConfiguration, 0)
	for rows.Next() {
		configuration, err := scanProviderConfiguration(rows)
		if err != nil {
			return nil, err
		}
		configurations = append(configurations, configuration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider configurations: %w", err)
	}
	return configurations, nil
}

func (r *ProviderRepository) Save(input model.SaveProviderConfiguration) (model.ProviderConfiguration, error) {
	transaction, err := r.database.Begin()
	if err != nil {
		return model.ProviderConfiguration{}, fmt.Errorf("begin provider transaction: %w", err)
	}
	defer transaction.Rollback()

	var existingSecret encryptedSecret
	var existingHint string
	err = transaction.QueryRow(`
SELECT secret_nonce, secret_ciphertext, api_key_hint
FROM agent_provider_configurations
WHERE provider_id = ?`, input.ProviderID).Scan(&existingSecret.Nonce, &existingSecret.Ciphertext, &existingHint)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.ProviderConfiguration{}, fmt.Errorf("query existing provider configuration: %w", err)
	}

	secret := existingSecret
	keyHint := existingHint
	if input.APIKey != "" {
		secret, err = r.encrypt(input.APIKey)
		if err != nil {
			return model.ProviderConfiguration{}, err
		}
		keyHint = apiKeyHint(input.APIKey)
	} else if !exists {
		return model.ProviderConfiguration{}, ErrAPIKeyRequired
	}

	modelIDsJSON, err := json.Marshal(input.ModelIDs)
	if err != nil {
		return model.ProviderConfiguration{}, fmt.Errorf("encode provider model ids: %w", err)
	}
	updatedAt := time.Now().UTC()
	_, err = transaction.Exec(`
INSERT INTO agent_provider_configurations (
    id, provider_id, base_url, model_ids_json, secret_nonce, secret_ciphertext, api_key_hint, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider_id) DO UPDATE SET
    base_url = excluded.base_url,
    model_ids_json = excluded.model_ids_json,
    secret_nonce = excluded.secret_nonce,
    secret_ciphertext = excluded.secret_ciphertext,
    api_key_hint = excluded.api_key_hint,
    updated_at = excluded.updated_at`,
		input.ProviderID,
		input.ProviderID,
		input.BaseURL,
		string(modelIDsJSON),
		secret.Nonce,
		secret.Ciphertext,
		keyHint,
		updatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return model.ProviderConfiguration{}, fmt.Errorf("save provider configuration: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return model.ProviderConfiguration{}, fmt.Errorf("commit provider transaction: %w", err)
	}

	return model.ProviderConfiguration{
		ID:               input.ProviderID,
		ProviderID:       input.ProviderID,
		BaseURL:          input.BaseURL,
		ModelIDs:         append([]string(nil), input.ModelIDs...),
		APIKeyConfigured: len(secret.Ciphertext) > 0,
		APIKeyHint:       keyHint,
		UpdatedAt:        updatedAt,
	}, nil
}

func (r *ProviderRepository) Delete(providerID string) error {
	result, err := r.database.Exec("DELETE FROM agent_provider_configurations WHERE provider_id = ?", providerID)
	if err != nil {
		return fmt.Errorf("delete provider configuration: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted provider count: %w", err)
	}
	if deleted == 0 {
		return ErrProviderConfigurationNotFound
	}
	return nil
}

func (r *ProviderRepository) GetAPIKey(providerID string) (string, error) {
	var secret encryptedSecret
	err := r.database.QueryRow(`
SELECT secret_nonce, secret_ciphertext
FROM agent_provider_configurations
WHERE provider_id = ?`, providerID).Scan(&secret.Nonce, &secret.Ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrProviderConfigurationNotFound
	}
	if err != nil {
		return "", fmt.Errorf("query provider secret: %w", err)
	}
	return r.decrypt(secret)
}

func (r *ProviderRepository) ResolveModel(providerID, modelID string) (string, string, error) {
	var baseURL, modelIDsJSON string
	err := r.database.QueryRow(`
SELECT base_url, model_ids_json
FROM agent_provider_configurations
WHERE provider_id = ?`, providerID).Scan(&baseURL, &modelIDsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrProviderConfigurationNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("query provider model: %w", err)
	}
	var modelIDs []string
	if err := json.Unmarshal([]byte(modelIDsJSON), &modelIDs); err != nil {
		return "", "", fmt.Errorf("decode provider model ids: %w", err)
	}
	for _, enabledModelID := range modelIDs {
		if enabledModelID == modelID {
			apiKey, err := r.GetAPIKey(providerID)
			return baseURL, apiKey, err
		}
	}
	return "", "", fmt.Errorf("model %q is not enabled for provider %q", modelID, providerID)
}

func (r *ProviderRepository) initialize() error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	}
	for _, pragma := range pragmas {
		if _, err := r.database.Exec(pragma); err != nil {
			return fmt.Errorf("configure sqlite database: %w", err)
		}
	}
	if err := r.migrateSchema(); err != nil {
		return err
	}
	if err := os.Chmod(r.databasePath, 0o600); err != nil {
		return fmt.Errorf("secure sqlite database: %w", err)
	}
	return nil
}

func (r *ProviderRepository) migrateSchema() error {
	var schemaVersion int
	if err := r.database.QueryRow("PRAGMA user_version").Scan(&schemaVersion); err != nil {
		return fmt.Errorf("read sqlite schema version: %w", err)
	}
	if schemaVersion > currentSchemaVersion {
		return fmt.Errorf("sqlite schema version %d is newer than supported version %d", schemaVersion, currentSchemaVersion)
	}
	if schemaVersion == currentSchemaVersion {
		return nil
	}

	transaction, err := r.database.Begin()
	if err != nil {
		return fmt.Errorf("begin sqlite schema migration: %w", err)
	}
	defer transaction.Rollback()
	if schemaVersion < 1 {
		if _, err := transaction.Exec(providerSchemaSQL); err != nil {
			return fmt.Errorf("create provider schema: %w", err)
		}
	}
	if schemaVersion < 2 {
		if _, err := transaction.Exec(agentSchemaSQL); err != nil {
			return fmt.Errorf("create agent schema: %w", err)
		}
	}
	if schemaVersion < 3 {
		if _, err := transaction.Exec(canvasSchemaSQL); err != nil {
			return fmt.Errorf("create canvas schema: %w", err)
		}
	}
	if _, err := transaction.Exec("PRAGMA user_version = 3"); err != nil {
		return fmt.Errorf("record sqlite schema version: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit sqlite schema migration: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProviderConfiguration(scanner rowScanner) (model.ProviderConfiguration, error) {
	var configuration model.ProviderConfiguration
	var modelIDsJSON string
	var ciphertext []byte
	var updatedAt string
	if err := scanner.Scan(
		&configuration.ID,
		&configuration.ProviderID,
		&configuration.BaseURL,
		&modelIDsJSON,
		&ciphertext,
		&configuration.APIKeyHint,
		&updatedAt,
	); err != nil {
		return model.ProviderConfiguration{}, fmt.Errorf("scan provider configuration: %w", err)
	}
	if err := json.Unmarshal([]byte(modelIDsJSON), &configuration.ModelIDs); err != nil {
		return model.ProviderConfiguration{}, fmt.Errorf("decode provider model ids: %w", err)
	}
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return model.ProviderConfiguration{}, fmt.Errorf("parse provider updated time: %w", err)
	}
	configuration.APIKeyConfigured = len(ciphertext) > 0
	configuration.UpdatedAt = parsedUpdatedAt
	return configuration, nil
}

func (r *ProviderRepository) encrypt(plaintext string) (encryptedSecret, error) {
	key, err := r.loadOrCreateMasterKey()
	if err != nil {
		return encryptedSecret{}, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return encryptedSecret{}, fmt.Errorf("create secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return encryptedSecret{}, fmt.Errorf("create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return encryptedSecret{}, fmt.Errorf("create secret nonce: %w", err)
	}
	return encryptedSecret{
		Nonce:      nonce,
		Ciphertext: gcm.Seal(nil, nonce, []byte(plaintext), nil),
	}, nil
}

func (r *ProviderRepository) decrypt(secret encryptedSecret) (string, error) {
	key, err := r.loadOrCreateMasterKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}
	plaintext, err := gcm.Open(nil, secret.Nonce, secret.Ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt provider secret: %w", err)
	}
	return string(plaintext), nil
}

func (r *ProviderRepository) loadOrCreateMasterKey() ([]byte, error) {
	key, err := os.ReadFile(r.keyPath)
	if err == nil {
		if len(key) != masterKeySize {
			return nil, errors.New("invalid master key length")
		}
		if err := os.Chmod(r.keyPath, 0o600); err != nil {
			return nil, fmt.Errorf("secure master key: %w", err)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read master key: %w", err)
	}

	key = make([]byte, masterKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	file, err := os.OpenFile(r.keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return r.loadOrCreateMasterKey()
	}
	if err != nil {
		return nil, fmt.Errorf("create master key: %w", err)
	}
	if _, err := file.Write(key); err != nil {
		file.Close()
		return nil, fmt.Errorf("write master key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close master key: %w", err)
	}
	return key, nil
}

func apiKeyHint(apiKey string) string {
	if len(apiKey) <= 4 {
		return "••••"
	}
	return "••••" + apiKey[len(apiKey)-4:]
}

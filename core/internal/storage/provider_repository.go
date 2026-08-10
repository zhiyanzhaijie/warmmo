package storage

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
	_ "modernc.org/sqlite/vec"

	"warmnote/core/internal/ai"
)

const (
	databaseFileName     = "warmnote.db"
	masterKeySize        = 32
	currentSchemaVersion = 16
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
    target_node_id TEXT NOT NULL DEFAULT '',
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
	canvasPositionSchemaSQL = `
ALTER TABLE canvas_nodes ADD COLUMN x REAL NOT NULL DEFAULT 0;
ALTER TABLE canvas_nodes ADD COLUMN y REAL NOT NULL DEFAULT 0;`
	candidateLifecycleSchemaSQL = `
ALTER TABLE agent_candidates ADD COLUMN status TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE agent_candidates ADD COLUMN kind TEXT NOT NULL DEFAULT 'chapter-section';
ALTER TABLE agent_candidates ADD COLUMN title TEXT NOT NULL DEFAULT '章节小节候选';
ALTER TABLE agent_candidates ADD COLUMN x REAL NOT NULL DEFAULT 520;
ALTER TABLE agent_candidates ADD COLUMN y REAL NOT NULL DEFAULT 80;
ALTER TABLE agent_candidates ADD COLUMN accepted_node_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_candidates ADD COLUMN decided_at TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_agent_candidates_work_status_created
    ON agent_candidates(work_id, status, created_at DESC);
CREATE TABLE IF NOT EXISTS canvas_edges (
    id TEXT PRIMARY KEY,
    work_id TEXT NOT NULL,
    source_node_id TEXT NOT NULL REFERENCES canvas_nodes(id) ON DELETE CASCADE,
    target_node_id TEXT NOT NULL REFERENCES canvas_nodes(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(work_id, source_node_id, target_node_id, kind)
);
CREATE INDEX IF NOT EXISTS idx_canvas_edges_work_created
    ON canvas_edges(work_id, created_at);`
	canvasHistorySchemaSQL = `
CREATE TABLE IF NOT EXISTS canvas_actions (
    id TEXT PRIMARY KEY,
    work_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    action_type TEXT NOT NULL,
    label TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(work_id, sequence)
);
CREATE INDEX IF NOT EXISTS idx_canvas_actions_work_sequence
    ON canvas_actions(work_id, sequence);
CREATE TABLE IF NOT EXISTS canvas_history_state (
    work_id TEXT PRIMARY KEY,
    current_sequence INTEGER NOT NULL,
    current_action_id TEXT NOT NULL
);`
	workSchemaSQL = `
CREATE TABLE IF NOT EXISTS works (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_works_updated_at ON works(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_canvas_nodes_work_updated
    ON canvas_nodes(work_id, updated_at DESC);
INSERT OR IGNORE INTO works (id, title, created_at, updated_at)
SELECT work_id, '未命名作品', MIN(created_at), MAX(updated_at)
FROM (
    SELECT work_id, created_at, updated_at FROM canvas_nodes
    UNION ALL
    SELECT work_id, created_at, created_at AS updated_at FROM canvas_edges
    UNION ALL
    SELECT work_id, created_at, updated_at FROM agent_runs
    UNION ALL
    SELECT work_id, created_at, COALESCE(NULLIF(decided_at, ''), created_at) AS updated_at FROM agent_candidates
    UNION ALL
    SELECT work_id, created_at, created_at AS updated_at FROM canvas_actions
)
WHERE work_id <> ''
GROUP BY work_id;`
	workMetadataSchemaSQL = `
CREATE TABLE IF NOT EXISTS work_folders (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_work_folders_sort_name
    ON work_folders(sort_order, name);
ALTER TABLE works ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE works ADD COLUMN folder_id TEXT NOT NULL DEFAULT '';
ALTER TABLE works ADD COLUMN status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE works ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;
CREATE INDEX IF NOT EXISTS idx_works_status_folder_updated
    ON works(status, folder_id, updated_at DESC);`
	agentRunResumeSchemaSQL = `
ALTER TABLE agent_runs ADD COLUMN target_node_id TEXT NOT NULL DEFAULT '';`
	chapterSectionNodeKindsSchemaSQL = `
UPDATE canvas_nodes SET kind = 'chapter-section' WHERE kind = 'section-draft';
UPDATE agent_candidates SET kind = 'chapter-section' WHERE kind = 'section-draft';
UPDATE agent_runs SET target = 'chapter-section' WHERE target = 'section-draft';`
	versionSchemaSQL = `
CREATE TABLE IF NOT EXISTS canvas_node_versions (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES canvas_nodes(id) ON DELETE CASCADE,
    work_id TEXT NOT NULL,
    version_number INTEGER NOT NULL,
    parent_version_id TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    source_run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE(node_id, version_number)
);
CREATE INDEX IF NOT EXISTS idx_canvas_node_versions_node_created
    ON canvas_node_versions(node_id, version_number DESC);
INSERT OR IGNORE INTO canvas_node_versions (id,node_id,work_id,version_number,title,content,created_at)
SELECT 'initial:' || id, id, work_id, 1, title, content, created_at FROM canvas_nodes;
UPDATE canvas_nodes SET current_version_id = 'initial:' || id WHERE current_version_id = '';`
	candidateVersionSchemaSQL = `CREATE TABLE agent_candidates_v11 (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
    work_id TEXT NOT NULL,
    skill_id TEXT NOT NULL,
    skill_version TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    kind TEXT NOT NULL DEFAULT 'chapter-section',
    title TEXT NOT NULL DEFAULT '章节小节候选',
    x REAL NOT NULL DEFAULT 520,
    y REAL NOT NULL DEFAULT 80,
    accepted_node_id TEXT NOT NULL DEFAULT '',
    decided_at TEXT NOT NULL DEFAULT '',
    candidate_type TEXT NOT NULL DEFAULT 'node',
    node_id TEXT NOT NULL DEFAULT '',
    base_version_id TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    change_score REAL NOT NULL DEFAULT 0
);
INSERT INTO agent_candidates_v11 (id,run_id,work_id,skill_id,skill_version,content,created_at,status,kind,title,x,y,accepted_node_id,decided_at)
SELECT id,run_id,work_id,skill_id,skill_version,content,created_at,status,kind,title,x,y,accepted_node_id,decided_at FROM agent_candidates;
DROP TABLE agent_candidates;
ALTER TABLE agent_candidates_v11 RENAME TO agent_candidates;
CREATE INDEX idx_agent_candidates_work_status_created ON agent_candidates(work_id,status,created_at DESC);
CREATE INDEX idx_agent_candidates_run ON agent_candidates(run_id);`
	chapterArchiveSchemaSQL = `
CREATE TABLE IF NOT EXISTS chapter_archives (
    id TEXT PRIMARY KEY,
    work_id TEXT NOT NULL,
    chapter_outline_node_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    run_id TEXT NOT NULL,
    outline_version_id TEXT NOT NULL DEFAULT '',
    outline_revision INTEGER NOT NULL,
    outline_title TEXT NOT NULL,
    outline_content TEXT NOT NULL,
    summary TEXT NOT NULL,
    source_digest TEXT NOT NULL,
    is_current INTEGER NOT NULL DEFAULT 1,
    projection_status TEXT NOT NULL DEFAULT 'pending',
    created_at TEXT NOT NULL,
    superseded_at TEXT NOT NULL DEFAULT '',
    UNIQUE(work_id, chapter_outline_node_id, revision)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_chapter_archives_current
    ON chapter_archives(work_id, chapter_outline_node_id) WHERE is_current = 1;
CREATE INDEX IF NOT EXISTS idx_chapter_archives_work_created
    ON chapter_archives(work_id, created_at);
CREATE TABLE IF NOT EXISTS chapter_archive_sections (
    archive_id TEXT NOT NULL REFERENCES chapter_archives(id) ON DELETE CASCADE,
    work_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    section_outline_node_id TEXT NOT NULL,
    chapter_section_node_id TEXT NOT NULL,
    chapter_section_version_id TEXT NOT NULL DEFAULT '',
    node_revision INTEGER NOT NULL,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    PRIMARY KEY(archive_id, chapter_section_node_id),
    UNIQUE(archive_id, ordinal)
);
CREATE INDEX IF NOT EXISTS idx_chapter_archive_sections_work_node
    ON chapter_archive_sections(work_id, chapter_section_node_id);`
	chapterArchiveRetractionSchemaSQL = `
ALTER TABLE chapter_archives ADD COLUMN retracted_at TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_chapter_archives_work_retracted_created
    ON chapter_archives(work_id, retracted_at, created_at);`
	knowledgeVectorSchemaSQL = `
CREATE TABLE IF NOT EXISTS knowledge_vector_documents (
    vector_row_id INTEGER PRIMARY KEY,
    work_id TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    node_id TEXT NOT NULL DEFAULT '',
    version_id TEXT NOT NULL DEFAULT '',
    archive_id TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 0,
    chunk_index INTEGER NOT NULL DEFAULT 0,
    model_id TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    scope TEXT NOT NULL,
    kind TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL,
    evidence_json TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL,
    indexed_at TEXT NOT NULL DEFAULT '',
    UNIQUE(object_type, object_id, chunk_index, model_id, scope)
);
CREATE INDEX IF NOT EXISTS idx_knowledge_vector_documents_work_scope
    ON knowledge_vector_documents(work_id, scope, status);
CREATE INDEX IF NOT EXISTS idx_knowledge_vector_documents_node_scope
    ON knowledge_vector_documents(work_id, node_id, scope, status);
CREATE TABLE IF NOT EXISTS knowledge_index_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_id TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(work_id, object_type, object_id)
);
CREATE INDEX IF NOT EXISTS idx_knowledge_index_jobs_pending
    ON knowledge_index_jobs(status, updated_at, id);
CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_vectors USING vec0(
    embedding float[1024] distance_metric=cosine
);
CREATE TRIGGER IF NOT EXISTS knowledge_enqueue_node_version
AFTER INSERT ON canvas_node_versions
BEGIN
    INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, created_at, updated_at)
    VALUES (NEW.work_id, 'node-version', NEW.id, 'pending', NEW.created_at, NEW.created_at)
    ON CONFLICT(work_id, object_type, object_id) DO UPDATE SET status='pending', last_error='', updated_at=excluded.updated_at;
END;
CREATE TRIGGER IF NOT EXISTS knowledge_enqueue_current_version
AFTER UPDATE OF current_version_id ON canvas_nodes
WHEN NEW.current_version_id <> OLD.current_version_id AND NEW.current_version_id <> ''
BEGIN
    INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, created_at, updated_at)
    VALUES (NEW.work_id, 'node-version', NEW.current_version_id, 'pending', NEW.updated_at, NEW.updated_at)
    ON CONFLICT(work_id, object_type, object_id) DO UPDATE SET status='pending', last_error='', updated_at=excluded.updated_at;
END;
CREATE TRIGGER IF NOT EXISTS knowledge_enqueue_archive
AFTER INSERT ON chapter_archives
BEGIN
    INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, created_at, updated_at)
    VALUES (NEW.work_id, 'archive', NEW.id, 'pending', NEW.created_at, NEW.created_at)
    ON CONFLICT(work_id, object_type, object_id) DO UPDATE SET status='pending', last_error='', updated_at=excluded.updated_at;
END;
CREATE TRIGGER IF NOT EXISTS knowledge_enqueue_archive_section
AFTER INSERT ON chapter_archive_sections
BEGIN
    INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, created_at, updated_at)
    VALUES (NEW.work_id, 'archive-section', NEW.archive_id || ':' || NEW.chapter_section_node_id, 'pending', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
    ON CONFLICT(work_id, object_type, object_id) DO UPDATE SET status='pending', last_error='', updated_at=excluded.updated_at;
END;
INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, created_at, updated_at)
SELECT work_id, 'node-version', current_version_id, 'pending', updated_at, updated_at
FROM canvas_nodes WHERE current_version_id <> ''
ON CONFLICT(work_id, object_type, object_id) DO NOTHING;
INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, created_at, updated_at)
SELECT work_id, 'archive', id, 'pending', created_at, created_at
FROM chapter_archives WHERE is_current = 1 AND retracted_at = ''
ON CONFLICT(work_id, object_type, object_id) DO NOTHING;`
	knowledgeVectorPartitionSchemaSQL = `
CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_vectors_partitioned USING vec0(
    work_id TEXT PARTITION KEY,
    model_id TEXT,
    scope TEXT,
    kind TEXT,
    is_spine BOOLEAN,
    embedding FLOAT[1024] DISTANCE_METRIC=cosine
);
INSERT OR REPLACE INTO knowledge_vectors_partitioned(rowid, work_id, model_id, scope, kind, is_spine, embedding)
SELECT v.rowid,
       d.work_id,
       d.model_id,
       d.scope,
       d.kind,
       CASE WHEN d.object_type IN ('archive', 'archive-section') THEN 1 ELSE 0 END,
       v.embedding
FROM knowledge_vectors v
JOIN knowledge_vector_documents d ON d.vector_row_id = v.rowid
WHERE d.status = 'ready';
DROP TABLE IF EXISTS knowledge_vectors;
DROP TRIGGER IF EXISTS knowledge_enqueue_node_version;
DROP TRIGGER IF EXISTS knowledge_enqueue_current_version;
DROP TRIGGER IF EXISTS knowledge_enqueue_archive;
DROP TRIGGER IF EXISTS knowledge_enqueue_archive_section;
CREATE TRIGGER knowledge_enqueue_node_version
AFTER INSERT ON canvas_node_versions
BEGIN
    INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, attempts, last_error, created_at, updated_at)
    VALUES (NEW.work_id, 'node-version', NEW.id, 'pending', 0, '', NEW.created_at, NEW.created_at)
    ON CONFLICT(work_id, object_type, object_id)
    DO UPDATE SET status='pending', attempts=0, last_error='', updated_at=excluded.updated_at;
END;
CREATE TRIGGER knowledge_enqueue_current_version
AFTER UPDATE OF current_version_id ON canvas_nodes
WHEN NEW.current_version_id <> OLD.current_version_id AND NEW.current_version_id <> ''
BEGIN
    INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, attempts, last_error, created_at, updated_at)
    VALUES (NEW.work_id, 'node-version', NEW.current_version_id, 'pending', 0, '', NEW.updated_at, NEW.updated_at)
    ON CONFLICT(work_id, object_type, object_id)
    DO UPDATE SET status='pending', attempts=0, last_error='', updated_at=excluded.updated_at;
END;
CREATE TRIGGER knowledge_enqueue_archive
AFTER INSERT ON chapter_archives
BEGIN
    INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, attempts, last_error, created_at, updated_at)
    VALUES (NEW.work_id, 'archive', NEW.id, 'pending', 0, '', NEW.created_at, NEW.created_at)
    ON CONFLICT(work_id, object_type, object_id)
    DO UPDATE SET status='pending', attempts=0, last_error='', updated_at=excluded.updated_at;
END;
CREATE TRIGGER knowledge_enqueue_archive_section
AFTER INSERT ON chapter_archive_sections
BEGIN
    INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, attempts, last_error, created_at, updated_at)
    VALUES (NEW.work_id, 'archive-section', NEW.archive_id || ':' || NEW.chapter_section_node_id, 'pending', 0, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
    ON CONFLICT(work_id, object_type, object_id)
    DO UPDATE SET status='pending', attempts=0, last_error='', updated_at=excluded.updated_at;
END;
CREATE TRIGGER knowledge_enqueue_archive_state_change
AFTER UPDATE OF is_current, retracted_at ON chapter_archives
WHEN NEW.is_current <> OLD.is_current OR NEW.retracted_at <> OLD.retracted_at
BEGIN
    INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, attempts, last_error, created_at, updated_at)
    VALUES (NEW.work_id, 'archive', NEW.id, 'pending', 0, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
    ON CONFLICT(work_id, object_type, object_id)
    DO UPDATE SET status='pending', attempts=0, last_error='', updated_at=excluded.updated_at;

    INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, attempts, last_error, created_at, updated_at)
    SELECT s.work_id,
           'archive-section',
           s.archive_id || ':' || s.chapter_section_node_id,
           'pending',
           0,
           '',
           CURRENT_TIMESTAMP,
           CURRENT_TIMESTAMP
    FROM chapter_archive_sections s
    WHERE s.archive_id = NEW.id
    ON CONFLICT(work_id, object_type, object_id)
    DO UPDATE SET status='pending', attempts=0, last_error='', updated_at=excluded.updated_at;
END;`
	knowledgeVectorDimensionSchemaSQL = `
DROP TABLE IF EXISTS knowledge_vectors_partitioned;
DROP TABLE IF EXISTS knowledge_vectors;
DELETE FROM knowledge_vector_documents;
UPDATE knowledge_index_jobs
SET status = 'pending', attempts = 0, last_error = '', updated_at = CURRENT_TIMESTAMP;
CREATE VIRTUAL TABLE knowledge_vectors_partitioned USING vec0(
    work_id TEXT PARTITION KEY,
    model_id TEXT,
    scope TEXT,
    kind TEXT,
    is_spine BOOLEAN,
    embedding FLOAT[1024] DISTANCE_METRIC=cosine
);`
)

var (
	ErrAPIKeyRequired                = ai.ErrAPIKeyRequired
	ErrProviderConfigurationNotFound = ai.ErrProviderConfigurationNotFound
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

func (r *ProviderRepository) List() ([]ai.ProviderConfiguration, error) {
	rows, err := r.database.Query(`
SELECT id, provider_id, base_url, model_ids_json, secret_ciphertext, api_key_hint, updated_at
FROM agent_provider_configurations
ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query provider configurations: %w", err)
	}
	defer rows.Close()

	configurations := make([]ai.ProviderConfiguration, 0)
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

func (r *ProviderRepository) Save(input ai.SaveProviderConfiguration) (ai.ProviderConfiguration, error) {
	transaction, err := r.database.Begin()
	if err != nil {
		return ai.ProviderConfiguration{}, fmt.Errorf("begin provider transaction: %w", err)
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
		return ai.ProviderConfiguration{}, fmt.Errorf("query existing provider configuration: %w", err)
	}

	secret := existingSecret
	keyHint := existingHint
	if input.APIKey != "" {
		secret, err = r.encrypt(input.APIKey)
		if err != nil {
			return ai.ProviderConfiguration{}, err
		}
		keyHint = apiKeyHint(input.APIKey)
	} else if !exists {
		return ai.ProviderConfiguration{}, ErrAPIKeyRequired
	}

	modelIDsJSON, err := json.Marshal(input.ModelIDs)
	if err != nil {
		return ai.ProviderConfiguration{}, fmt.Errorf("encode provider model ids: %w", err)
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
		return ai.ProviderConfiguration{}, fmt.Errorf("save provider configuration: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return ai.ProviderConfiguration{}, fmt.Errorf("commit provider transaction: %w", err)
	}

	return ai.ProviderConfiguration{
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
	if schemaVersion < 4 {
		if _, err := transaction.Exec(canvasPositionSchemaSQL); err != nil {
			return fmt.Errorf("add canvas node positions: %w", err)
		}
	}
	if schemaVersion < 5 {
		if _, err := transaction.Exec(candidateLifecycleSchemaSQL); err != nil {
			return fmt.Errorf("add candidate lifecycle: %w", err)
		}
	}
	if schemaVersion < 6 {
		if _, err := transaction.Exec(canvasHistorySchemaSQL); err != nil {
			return fmt.Errorf("add canvas action history: %w", err)
		}
	}
	if schemaVersion < 7 {
		if _, err := transaction.Exec(workSchemaSQL); err != nil {
			return fmt.Errorf("add work lifecycle: %w", err)
		}
	}
	if schemaVersion < 8 {
		if _, err := transaction.Exec(workMetadataSchemaSQL); err != nil {
			return fmt.Errorf("add work metadata: %w", err)
		}
	}
	if schemaVersion >= 2 && schemaVersion < 9 {
		if _, err := transaction.Exec(agentRunResumeSchemaSQL); err != nil {
			return fmt.Errorf("add resumable agent run input: %w", err)
		}
	}
	if schemaVersion >= 5 && schemaVersion < 10 {
		if _, err := transaction.Exec(chapterSectionNodeKindsSchemaSQL); err != nil {
			return fmt.Errorf("rename chapter section node kinds: %w", err)
		}
	}
	if schemaVersion < 11 {
		hasCurrentVersionID, err := tableHasColumn(transaction, "canvas_nodes", "current_version_id")
		if err != nil {
			return fmt.Errorf("inspect canvas node version column: %w", err)
		}
		if !hasCurrentVersionID {
			if _, err := transaction.Exec("ALTER TABLE canvas_nodes ADD COLUMN current_version_id TEXT NOT NULL DEFAULT ''"); err != nil {
				return fmt.Errorf("add current node version: %w", err)
			}
		}
		if _, err := transaction.Exec(versionSchemaSQL); err != nil {
			return fmt.Errorf("add canvas node versions: %w", err)
		}
		hasCandidateType, err := tableHasColumn(transaction, "agent_candidates", "candidate_type")
		if err != nil {
			return fmt.Errorf("inspect candidate version column: %w", err)
		}
		if !hasCandidateType {
			if _, err := transaction.Exec(candidateVersionSchemaSQL); err != nil {
				return fmt.Errorf("add version candidates: %w", err)
			}
		}
	}
	if schemaVersion < 12 {
		if _, err := transaction.Exec(chapterArchiveSchemaSQL); err != nil {
			return fmt.Errorf("add chapter archives: %w", err)
		}
	}
	if schemaVersion < 13 {
		if _, err := transaction.Exec(chapterArchiveRetractionSchemaSQL); err != nil {
			return fmt.Errorf("add chapter archive retraction: %w", err)
		}
	}
	if schemaVersion < 14 {
		if _, err := transaction.Exec(knowledgeVectorSchemaSQL); err != nil {
			return fmt.Errorf("add knowledge vector schema: %w", err)
		}
	}
	if schemaVersion < 15 {
		if _, err := transaction.Exec(knowledgeVectorPartitionSchemaSQL); err != nil {
			return fmt.Errorf("partition knowledge vector schema: %w", err)
		}
	}
	if schemaVersion < 16 {
		if _, err := transaction.Exec(knowledgeVectorDimensionSchemaSQL); err != nil {
			return fmt.Errorf("upgrade knowledge vector dimension: %w", err)
		}
	}
	if _, err := transaction.Exec("PRAGMA user_version = 16"); err != nil {
		return fmt.Errorf("record sqlite schema version: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit sqlite schema migration: %w", err)
	}
	return nil
}

func tableHasColumn(transaction *sql.Tx, table, column string) (bool, error) {
	rows, err := transaction.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProviderConfiguration(scanner rowScanner) (ai.ProviderConfiguration, error) {
	var configuration ai.ProviderConfiguration
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
		return ai.ProviderConfiguration{}, fmt.Errorf("scan provider configuration: %w", err)
	}
	if err := json.Unmarshal([]byte(modelIDsJSON), &configuration.ModelIDs); err != nil {
		return ai.ProviderConfiguration{}, fmt.Errorf("decode provider model ids: %w", err)
	}
	parsedUpdatedAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return ai.ProviderConfiguration{}, fmt.Errorf("parse provider updated time: %w", err)
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

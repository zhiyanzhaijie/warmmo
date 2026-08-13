package persistence

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

const (
	databaseFileName = "warmmo.db"
	schemaRevision   = "gorm-v1"
)

type Database struct {
	*gorm.DB
	sqlDB         *sql.DB
	path          string
	dataDirectory string
}

func OpenDatabase(dataDirectory string) (*Database, error) {
	if err := os.MkdirAll(dataDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(dataDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("secure data directory: %w", err)
	}

	databasePath := filepath.Join(dataDirectory, databaseFileName)
	reset, err := databaseNeedsReset(databasePath)
	if err != nil {
		return nil, err
	}
	if reset {
		for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("reset development database: %w", err)
			}
		}
	}

	sqlDB, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	db, err := gorm.Open(gormsqlite.Dialector{DriverName: "sqlite", Conn: sqlDB}, &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("open gorm database: %w", err)
	}
	database := &Database{DB: db, sqlDB: sqlDB, path: databasePath, dataDirectory: dataDirectory}
	if err := database.initialize(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return database, nil
}

func databaseNeedsReset(path string) (bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect database file: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return false, fmt.Errorf("inspect database schema: %w", err)
	}
	defer db.Close()
	var value string
	err = db.QueryRow(`SELECT value FROM schema_metadata WHERE key='revision'`).Scan(&value)
	if err == nil {
		return value != schemaRevision, nil
	}
	if isMissingSchemaTable(err) {
		return true, nil
	}
	return false, fmt.Errorf("read database schema revision: %w", err)
}

func isMissingSchemaTable(err error) bool {
	return err != nil && (errors.Is(err, sql.ErrNoRows) || containsSQLiteMissingTable(err.Error()))
}

func containsSQLiteMissingTable(message string) bool {
	return strings.Contains(message, "no such table")
}

func (d *Database) initialize() error {
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if err := d.Exec(pragma).Error; err != nil {
			return fmt.Errorf("configure sqlite database: %w", err)
		}
	}
	if err := d.migrateArtifactTurnIndex(); err != nil {
		return err
	}
	if err := d.AutoMigrate(
		&schemaMetadataModel{}, &providerConfigurationModel{},
		&workFolderModel{}, &workModel{},
		&agentRunModel{}, &agentRunEventModel{}, &agentResponseModel{}, &agentCandidateModel{}, &agentProposalEdgeModel{},
		&agentSessionModel{}, &agentSessionEventModel{}, &agentSessionScopedStateModel{}, &agentConversationModel{}, &agentConversationTurnModel{}, &agentTurnCheckpointModel{}, &agentArtifactModel{}, &agentProductProjectionModel{}, &agentToolCallModel{}, &agentMemoryModel{},
		&canvasNodeModel{}, &canvasNodeVersionModel{}, &canvasEdgeModel{},
		&canvasActionModel{}, &canvasHistoryStateModel{},
		&chapterArchiveModel{}, &chapterArchiveSectionModel{},
		&knowledgeVectorDocumentModel{}, &knowledgeIndexJobModel{},
	); err != nil {
		return fmt.Errorf("migrate gorm schema: %w", err)
	}
	if err := d.normalizeLegacyCanvasKinds(); err != nil {
		return err
	}
	if err := d.initializeVectorSchema(); err != nil {
		return err
	}
	metadata := schemaMetadataModel{Key: "revision", Value: schemaRevision}
	if err := d.Save(&metadata).Error; err != nil {
		return fmt.Errorf("record schema revision: %w", err)
	}
	if err := os.Chmod(d.path, 0o600); err != nil {
		return fmt.Errorf("secure sqlite database: %w", err)
	}
	return nil
}

func (d *Database) normalizeLegacyCanvasKinds() error {
	if err := d.Model(&canvasNodeModel{}).Where("kind = ?", "worldview").Update("kind", "world").Error; err != nil {
		return fmt.Errorf("normalize legacy canvas node kinds: %w", err)
	}
	if err := d.Model(&agentCandidateModel{}).Where("kind = ?", "worldview").Update("kind", "world").Error; err != nil {
		return fmt.Errorf("normalize legacy canvas candidate kinds: %w", err)
	}
	return nil
}

func (d *Database) migrateArtifactTurnIndex() error {
	var indexes []struct {
		Name   string `gorm:"column:name"`
		Unique int    `gorm:"column:unique"`
	}
	if err := d.Raw("PRAGMA index_list('agent_artifacts')").Scan(&indexes).Error; err != nil {
		return fmt.Errorf("inspect agent artifact indexes: %w", err)
	}
	for _, index := range indexes {
		if index.Name != "idx_agent_artifacts_turn_id" || index.Unique == 0 {
			continue
		}
		if err := d.Exec("DROP INDEX IF EXISTS idx_agent_artifacts_turn_id").Error; err != nil {
			return fmt.Errorf("migrate agent artifact turn index: %w", err)
		}
		break
	}
	return nil
}

func (d *Database) initializeVectorSchema() error {
	statements := []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_vectors_partitioned USING vec0(
            work_id TEXT PARTITION KEY,
            model_id TEXT,
            scope TEXT,
            kind TEXT,
            is_spine BOOLEAN,
            embedding FLOAT[1024] DISTANCE_METRIC=cosine
        )`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_chapter_archives_current
            ON chapter_archives(work_id, chapter_outline_node_id) WHERE is_current = 1 AND retracted_at IS NULL`,
		`CREATE TRIGGER IF NOT EXISTS knowledge_enqueue_node_version AFTER INSERT ON canvas_node_versions BEGIN
            INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, attempts, last_error, created_at, updated_at)
            VALUES (NEW.work_id, 'node-version', NEW.id, 'pending', 0, '', NEW.created_at, NEW.created_at)
            ON CONFLICT(work_id, object_type, object_id)
            DO UPDATE SET status='pending', attempts=0, last_error='', updated_at=excluded.updated_at;
        END`,
		`CREATE TRIGGER IF NOT EXISTS knowledge_enqueue_current_version AFTER UPDATE OF current_version_id ON canvas_nodes
        WHEN NEW.current_version_id <> OLD.current_version_id AND NEW.current_version_id <> '' BEGIN
            INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, attempts, last_error, created_at, updated_at)
            VALUES (NEW.work_id, 'node-version', NEW.current_version_id, 'pending', 0, '', NEW.updated_at, NEW.updated_at)
            ON CONFLICT(work_id, object_type, object_id)
            DO UPDATE SET status='pending', attempts=0, last_error='', updated_at=excluded.updated_at;
        END`,
		`CREATE TRIGGER IF NOT EXISTS knowledge_enqueue_archive AFTER INSERT ON chapter_archives BEGIN
            INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, attempts, last_error, created_at, updated_at)
            VALUES (NEW.work_id, 'archive', NEW.id, 'pending', 0, '', NEW.created_at, NEW.created_at)
            ON CONFLICT(work_id, object_type, object_id)
            DO UPDATE SET status='pending', attempts=0, last_error='', updated_at=excluded.updated_at;
        END`,
		`CREATE TRIGGER IF NOT EXISTS knowledge_enqueue_archive_section AFTER INSERT ON chapter_archive_sections BEGIN
            INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, attempts, last_error, created_at, updated_at)
            VALUES (NEW.work_id, 'archive-section', NEW.archive_id || ':' || NEW.chapter_section_node_id, 'pending', 0, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
            ON CONFLICT(work_id, object_type, object_id)
            DO UPDATE SET status='pending', attempts=0, last_error='', updated_at=excluded.updated_at;
        END`,
		`CREATE TRIGGER IF NOT EXISTS knowledge_enqueue_archive_state_change AFTER UPDATE OF is_current, retracted_at ON chapter_archives
        WHEN NEW.is_current <> OLD.is_current OR NEW.retracted_at IS NOT OLD.retracted_at BEGIN
            INSERT INTO knowledge_index_jobs(work_id, object_type, object_id, status, attempts, last_error, created_at, updated_at)
            VALUES (NEW.work_id, 'archive', NEW.id, 'pending', 0, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
            ON CONFLICT(work_id, object_type, object_id)
            DO UPDATE SET status='pending', attempts=0, last_error='', updated_at=excluded.updated_at;
        END`,
	}
	for _, statement := range statements {
		if err := d.Exec(statement).Error; err != nil {
			return fmt.Errorf("initialize vector schema: %w", err)
		}
	}
	return nil
}

func (d *Database) Close() error          { return d.sqlDB.Close() }
func (d *Database) Path() string          { return d.path }
func (d *Database) DataDirectory() string { return d.dataDirectory }

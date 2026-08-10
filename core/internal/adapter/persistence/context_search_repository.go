package persistence

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	appagent "warmmo/core/internal/application/agent"
	"warmmo/core/internal/domain/ai"
	"warmmo/core/internal/domain/canvas"
)

const (
	ContextEmbeddingDimension = ai.CanonicalEmbeddingDimensions
	contextIndexBatchSize     = 8
	contextSearchOversample   = 4
)

// ContextEmbedder is deliberately independent from the chat ai. ModelID
// identifies the embedding endpoint and model so incompatible vectors cannot mix.
type ContextEmbedder interface {
	ModelID() string
	Dimensions() int
	Embed(context.Context, string) ([]float32, error)
}

type ContextIndex struct {
	database *sql.DB
	embedder ContextEmbedder
}

var ErrContextSearchEmbeddingRequired = errors.New("context search requires an embedding provider")

// ContextSearchGateway keeps embedding configuration out of Core startup. It
// resolves the provider only when the context-search tool is actually called.
type ContextSearchGateway struct {
	appContext context.Context
	resolve    func() (*ContextIndex, error)
	mu         sync.Mutex
	index      *ContextIndex
}

func NewContextSearchGateway(appContext context.Context, resolve func() (*ContextIndex, error)) *ContextSearchGateway {
	if appContext == nil {
		appContext = context.Background()
	}
	return &ContextSearchGateway{appContext: appContext, resolve: resolve}
}

func (g *ContextSearchGateway) SearchContext(ctx context.Context, workID string, input appagent.ContextSearchInput) ([]appagent.ContextSearchResult, error) {
	index, err := g.getIndex()
	if err != nil {
		return nil, err
	}
	return index.SearchContext(ctx, workID, input)
}

func (g *ContextSearchGateway) getIndex() (*ContextIndex, error) {
	if g == nil || g.resolve == nil {
		return nil, ErrContextSearchEmbeddingRequired
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.index != nil {
		return g.index, nil
	}
	index, err := g.resolve()
	if err != nil {
		return nil, err
	}
	if index == nil {
		return nil, ErrContextSearchEmbeddingRequired
	}
	g.index = index
	go func() {
		_ = index.Run(g.appContext)
	}()
	return index, nil
}

func NewContextIndex(providerRepository *ProviderRepository, embedder ContextEmbedder) *ContextIndex {
	if providerRepository == nil {
		return &ContextIndex{embedder: embedder}
	}
	return &ContextIndex{database: providerRepository.database, embedder: embedder}
}

func (i *ContextIndex) configured() error {
	if i == nil || i.database == nil || i.embedder == nil {
		return errors.New("context index is not configured")
	}
	if i.embedder.Dimensions() != ContextEmbeddingDimension {
		return fmt.Errorf("context embedder dimensions must be %d", ContextEmbeddingDimension)
	}
	if strings.TrimSpace(i.embedder.ModelID()) == "" {
		return errors.New("context embedder model id is required")
	}
	return nil
}

// Run drains durable index jobs. It is safe to run one worker per process;
// SQLite's job claim transaction prevents duplicate processing.
func (i *ContextIndex) Run(ctx context.Context) error {
	if err := i.configured(); err != nil {
		return err
	}
	if err := i.prepareJobs(ctx); err != nil {
		return fmt.Errorf("prepare context index jobs: %w", err)
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		for count := 0; count < contextIndexBatchSize; count++ {
			processed, err := i.processOne(ctx)
			if err != nil || !processed {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (i *ContextIndex) prepareJobs(ctx context.Context) error {
	if err := i.configured(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := i.database.ExecContext(ctx, `UPDATE knowledge_index_jobs SET status='pending', last_error='', updated_at=? WHERE status='processing'`, now); err != nil {
		return err
	}
	if err := i.retireLegacyArchiveNodeVersions(ctx); err != nil {
		return err
	}
	if _, err := i.database.ExecContext(ctx, `
INSERT INTO knowledge_index_jobs(work_id,object_type,object_id,status,created_at,updated_at)
SELECT n.work_id,'node-version',n.current_version_id,'pending',?,?
FROM canvas_nodes n
WHERE n.current_version_id<>'' AND NOT EXISTS (
    SELECT 1 FROM knowledge_vector_documents d
    WHERE d.work_id=n.work_id AND d.version_id=n.current_version_id AND d.scope='current' AND d.model_id=? AND d.status='ready'
)
ON CONFLICT(work_id,object_type,object_id) DO UPDATE SET status='pending',attempts=0,last_error='',updated_at=excluded.updated_at`, now, now, i.embedder.ModelID()); err != nil {
		return err
	}
	if _, err := i.database.ExecContext(ctx, `
INSERT INTO knowledge_index_jobs(work_id,object_type,object_id,status,created_at,updated_at)
SELECT a.work_id,'archive',a.id,'pending',?,?
FROM chapter_archives a
WHERE a.retracted_at='' AND NOT EXISTS (
    SELECT 1 FROM knowledge_vector_documents d
    WHERE d.work_id=a.work_id AND d.object_type='archive' AND d.object_id=a.id AND d.model_id=? AND d.status='ready'
)
ON CONFLICT(work_id,object_type,object_id) DO UPDATE SET status='pending',attempts=0,last_error='',updated_at=excluded.updated_at`, now, now, i.embedder.ModelID()); err != nil {
		return err
	}
	_, err := i.database.ExecContext(ctx, `
INSERT INTO knowledge_index_jobs(work_id,object_type,object_id,status,created_at,updated_at)
SELECT s.work_id,'archive-section',s.archive_id || ':' || s.chapter_section_node_id,'pending',?,?
FROM chapter_archive_sections s
JOIN chapter_archives a ON a.id=s.archive_id AND a.work_id=s.work_id
WHERE a.retracted_at='' AND NOT EXISTS (
    SELECT 1 FROM knowledge_vector_documents d
    WHERE d.work_id=s.work_id AND d.object_type='archive-section'
      AND d.object_id=s.archive_id || ':' || s.chapter_section_node_id
      AND d.model_id=? AND d.status='ready'
)
ON CONFLICT(work_id,object_type,object_id) DO UPDATE SET status='pending',attempts=0,last_error='',updated_at=excluded.updated_at`, now, now, i.embedder.ModelID())
	return err
}

func (i *ContextIndex) retireLegacyArchiveNodeVersions(ctx context.Context) error {
	rows, err := i.database.QueryContext(ctx, `SELECT vector_row_id FROM knowledge_vector_documents WHERE object_type='node-version' AND scope='archive' AND status='ready'`)
	if err != nil {
		return err
	}
	ids, err := collectVectorRowIDs(rows)
	if err != nil {
		return err
	}
	if _, err = i.database.ExecContext(ctx, `UPDATE knowledge_vector_documents SET status='retired',indexed_at=? WHERE object_type='node-version' AND scope='archive'`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return i.deleteVectors(ctx, ids)
}

func (i *ContextIndex) processOne(ctx context.Context) (bool, error) {
	if err := i.configured(); err != nil {
		return false, err
	}
	job, claimed, err := i.claimJob(ctx)
	if err != nil || !claimed {
		return claimed, err
	}
	if err := i.processJob(ctx, job); err != nil {
		_ = i.finishJob(ctx, job.ID, false, err.Error())
		return true, err
	}
	return true, i.finishJob(ctx, job.ID, true, "")
}

type contextIndexJob struct {
	ID         int64
	WorkID     string
	ObjectType string
	ObjectID   string
}

func (i *ContextIndex) claimJob(ctx context.Context) (contextIndexJob, bool, error) {
	tx, err := i.database.BeginTx(ctx, nil)
	if err != nil {
		return contextIndexJob{}, false, err
	}
	defer tx.Rollback()
	var job contextIndexJob
	err = tx.QueryRowContext(ctx, `
SELECT id, work_id, object_type, object_id
FROM knowledge_index_jobs
WHERE status='pending'
ORDER BY updated_at, id
LIMIT 1`).Scan(&job.ID, &job.WorkID, &job.ObjectType, &job.ObjectID)
	if errors.Is(err, sql.ErrNoRows) {
		return contextIndexJob{}, false, nil
	}
	if err != nil {
		return contextIndexJob{}, false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE knowledge_index_jobs SET status='processing', attempts=attempts+1, updated_at=? WHERE id=? AND status='pending'`, time.Now().UTC().Format(time.RFC3339Nano), job.ID)
	if err != nil {
		return contextIndexJob{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return contextIndexJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return contextIndexJob{}, false, err
	}
	return job, true, nil
}

func (i *ContextIndex) finishJob(ctx context.Context, jobID int64, success bool, message string) error {
	status := "failed"
	if success {
		status = "ready"
	}
	_, err := i.database.ExecContext(ctx, `UPDATE knowledge_index_jobs SET status=CASE WHEN ?='failed' AND attempts<3 THEN 'pending' ELSE ? END, last_error=?, updated_at=? WHERE id=?`, status, status, message, time.Now().UTC().Format(time.RFC3339Nano), jobID)
	return err
}

func (i *ContextIndex) processJob(ctx context.Context, job contextIndexJob) error {
	switch job.ObjectType {
	case "node-version":
		return i.indexNodeVersion(ctx, job.WorkID, job.ObjectID)
	case "archive":
		return i.indexArchive(ctx, job.WorkID, job.ObjectID)
	case "archive-section":
		return i.indexArchiveSection(ctx, job.WorkID, job.ObjectID)
	default:
		return fmt.Errorf("unsupported context index job type %q", job.ObjectType)
	}
}

func (i *ContextIndex) indexNodeVersion(ctx context.Context, workID, versionID string) error {
	var nodeID, kind, title, content, currentVersionID string
	var revision int64
	if err := i.database.QueryRowContext(ctx, `
SELECT v.node_id, n.kind, v.title, v.content, n.revision, n.current_version_id
FROM canvas_node_versions v
JOIN canvas_nodes n ON n.work_id=v.work_id AND n.id=v.node_id
WHERE v.work_id=? AND v.id=?`, workID, versionID).Scan(&nodeID, &kind, &title, &content, &revision, &currentVersionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if currentVersionID != versionID {
		return i.retireDocuments(ctx, workID, "node-version", versionID)
	}
	if err := i.retireOtherCurrentVersions(ctx, workID, nodeID, versionID); err != nil {
		return err
	}
	return i.upsertDocument(ctx, contextDocument{
		WorkID: workID, ObjectType: "node-version", ObjectID: versionID,
		NodeID: nodeID, VersionID: versionID, Revision: revision,
		Kind: kind, Title: title, Content: title + "\n" + content,
		Scope: "current", Source: "canvas.current",
	})
}

func (i *ContextIndex) retireOtherCurrentVersions(ctx context.Context, workID, nodeID, currentVersionID string) error {
	rows, err := i.database.QueryContext(ctx, `SELECT vector_row_id FROM knowledge_vector_documents WHERE work_id=? AND node_id=? AND scope='current' AND version_id<>? AND status='ready'`, workID, nodeID, currentVersionID)
	if err != nil {
		return err
	}
	ids, err := collectVectorRowIDs(rows)
	if err != nil {
		return err
	}
	if _, err = i.database.ExecContext(ctx, `UPDATE knowledge_vector_documents SET status='retired',indexed_at=? WHERE work_id=? AND node_id=? AND scope='current' AND version_id<>?`, time.Now().UTC().Format(time.RFC3339Nano), workID, nodeID, currentVersionID); err != nil {
		return err
	}
	return i.deleteVectors(ctx, ids)
}

func (i *ContextIndex) indexArchive(ctx context.Context, workID, archiveID string) error {
	var nodeID, versionID, title, summary, content string
	var revision int64
	if err := i.database.QueryRowContext(ctx, `
SELECT chapter_outline_node_id, outline_version_id, outline_revision, outline_title, summary, outline_content
FROM chapter_archives
WHERE work_id=? AND id=? AND retracted_at=''`, workID, archiveID).Scan(&nodeID, &versionID, &revision, &title, &summary, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return i.retireArchiveDocuments(ctx, workID, archiveID)
		}
		return err
	}
	return i.upsertDocument(ctx, contextDocument{
		WorkID: workID, ObjectType: "archive", ObjectID: archiveID, ArchiveID: archiveID,
		NodeID: nodeID, VersionID: versionID, Revision: revision, Kind: string(canvas.NodeKindChapterOutline),
		Title: title, Content: title + "\n" + summary + "\n" + content,
		Scope: "archive", Source: "chapter-archive",
	})
}

func (i *ContextIndex) indexArchiveSection(ctx context.Context, workID, objectID string) error {
	archiveID, sectionNodeID, ok := strings.Cut(objectID, ":")
	if !ok || archiveID == "" || sectionNodeID == "" {
		return fmt.Errorf("invalid archive section index id %q", objectID)
	}
	var title, summary, content, versionID string
	var revision int64
	err := i.database.QueryRowContext(ctx, `
SELECT s.title, s.summary, s.content, s.chapter_section_version_id, s.node_revision
FROM chapter_archive_sections s
JOIN chapter_archives a ON a.id=s.archive_id AND a.work_id=s.work_id
WHERE s.work_id=? AND s.archive_id=? AND s.chapter_section_node_id=?
  AND a.retracted_at=''`, workID, archiveID, sectionNodeID).Scan(&title, &summary, &content, &versionID, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return i.retireDocuments(ctx, workID, "archive-section", objectID)
	}
	if err != nil {
		return err
	}
	return i.upsertDocument(ctx, contextDocument{
		WorkID: workID, ObjectType: "archive-section", ObjectID: objectID,
		NodeID: sectionNodeID, VersionID: versionID, ArchiveID: archiveID, Revision: revision,
		Kind:  string(canvas.NodeKindChapterSection),
		Title: title, Content: title + "\n" + summary + "\n" + content,
		Scope: "archive", Source: "chapter-archive",
	})
}

type contextDocument struct {
	WorkID, ObjectType, ObjectID string
	NodeID, VersionID, ArchiveID string
	Revision                     int64
	Kind, Title, Content         string
	Scope, Source                string
}

func (i *ContextIndex) upsertDocument(ctx context.Context, document contextDocument) error {
	digest := sha256.Sum256([]byte(document.Content))
	hash := hex.EncodeToString(digest[:])
	var existingRowID int64
	var existingHash, existingStatus string
	err := i.database.QueryRowContext(ctx, `
SELECT vector_row_id,content_hash,status
FROM knowledge_vector_documents
WHERE work_id=? AND object_type=? AND object_id=? AND chunk_index=0 AND model_id=? AND scope=?`,
		document.WorkID, document.ObjectType, document.ObjectID, i.embedder.ModelID(), document.Scope,
	).Scan(&existingRowID, &existingHash, &existingStatus)
	if err == nil && existingHash == hash && existingStatus == "ready" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	embedding, err := i.embedder.Embed(ctx, document.Content)
	if err != nil {
		return fmt.Errorf("embed context document: %w", err)
	}
	if len(embedding) != ContextEmbeddingDimension {
		return fmt.Errorf("embedding dimension %d does not match %d", len(embedding), ContextEmbeddingDimension)
	}
	vector, err := json.Marshal(embedding)
	if err != nil {
		return err
	}
	evidenceJSON, err := json.Marshal([]string{truncateContextEvidence(document.Content, 800)})
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := i.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rowID int64
	err = tx.QueryRowContext(ctx, `SELECT vector_row_id FROM knowledge_vector_documents WHERE work_id=? AND object_type=? AND object_id=? AND chunk_index=0 AND model_id=? AND scope=?`, document.WorkID, document.ObjectType, document.ObjectID, i.embedder.ModelID(), document.Scope).Scan(&rowID)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if exists {
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_vectors_partitioned WHERE rowid=?`, rowID); err != nil {
			return err
		}
	} else {
		result, err := tx.ExecContext(ctx, `INSERT INTO knowledge_vector_documents(work_id,object_type,object_id,node_id,version_id,archive_id,revision,chunk_index,model_id,content_hash,scope,kind,title,source,evidence_json,status,indexed_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, document.WorkID, document.ObjectType, document.ObjectID, document.NodeID, document.VersionID, document.ArchiveID, document.Revision, 0, i.embedder.ModelID(), hash, document.Scope, document.Kind, document.Title, document.Source, string(evidenceJSON), "pending", "")
		if err != nil {
			return err
		}
		rowID, err = result.LastInsertId()
		if err != nil {
			return err
		}
	}
	isSpine := document.ObjectType == "archive" || document.ObjectType == "archive-section"
	if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_vectors_partitioned(rowid,work_id,model_id,scope,kind,is_spine,embedding) VALUES (?,?,?,?,?,?,?)`, rowID, document.WorkID, i.embedder.ModelID(), document.Scope, document.Kind, isSpine, string(vector)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE knowledge_vector_documents SET work_id=?,node_id=?,version_id=?,archive_id=?,revision=?,content_hash=?,kind=?,title=?,source=?,evidence_json=?,status='ready',indexed_at=? WHERE vector_row_id=?`, document.WorkID, document.NodeID, document.VersionID, document.ArchiveID, document.Revision, hash, document.Kind, document.Title, document.Source, string(evidenceJSON), now, rowID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func truncateContextEvidence(content string, limit int) string {
	content = strings.TrimSpace(content)
	characters := []rune(content)
	if len(characters) <= limit {
		return content
	}
	return strings.TrimSpace(string(characters[:limit])) + "..."
}

func (i *ContextIndex) retireDocuments(ctx context.Context, workID, objectType, objectID string) error {
	rows, err := i.database.QueryContext(ctx, `SELECT vector_row_id FROM knowledge_vector_documents WHERE work_id=? AND object_type=? AND object_id=? AND status='ready'`, workID, objectType, objectID)
	if err != nil {
		return err
	}
	ids, err := collectVectorRowIDs(rows)
	if err != nil {
		return err
	}
	if _, err := i.database.ExecContext(ctx, `UPDATE knowledge_vector_documents SET status='retired', indexed_at=? WHERE work_id=? AND object_type=? AND object_id=?`, time.Now().UTC().Format(time.RFC3339Nano), workID, objectType, objectID); err != nil {
		return err
	}
	return i.deleteVectors(ctx, ids)
}

func (i *ContextIndex) retireArchiveDocuments(ctx context.Context, workID, archiveID string) error {
	rows, err := i.database.QueryContext(ctx, `SELECT vector_row_id FROM knowledge_vector_documents WHERE work_id=? AND archive_id=? AND status='ready'`, workID, archiveID)
	if err != nil {
		return err
	}
	ids, err := collectVectorRowIDs(rows)
	if err != nil {
		return err
	}
	if _, err := i.database.ExecContext(ctx, `UPDATE knowledge_vector_documents SET status='retired',indexed_at=? WHERE work_id=? AND archive_id=?`, time.Now().UTC().Format(time.RFC3339Nano), workID, archiveID); err != nil {
		return err
	}
	return i.deleteVectors(ctx, ids)
}

func collectVectorRowIDs(rows *sql.Rows) ([]int64, error) {
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (i *ContextIndex) deleteVectors(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		if _, err := i.database.ExecContext(ctx, `DELETE FROM knowledge_vectors_partitioned WHERE rowid=?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (i *ContextIndex) SearchContext(ctx context.Context, workID string, input appagent.ContextSearchInput) ([]appagent.ContextSearchResult, error) {
	if err := i.configured(); err != nil {
		return nil, err
	}
	queryEmbedding, err := i.embedder.Embed(ctx, input.Query)
	if err != nil {
		return nil, fmt.Errorf("embed context query: %w", err)
	}
	if len(queryEmbedding) != ContextEmbeddingDimension {
		return nil, fmt.Errorf("query embedding dimension %d does not match %d", len(queryEmbedding), ContextEmbeddingDimension)
	}
	queryVector, err := json.Marshal(queryEmbedding)
	if err != nil {
		return nil, err
	}
	k := input.Limit * contextSearchOversample
	if k < input.Limit {
		k = input.Limit
	}
	if k > 256 {
		k = 256
	}
	statement := `
SELECT d.node_id,d.version_id,d.archive_id,d.revision,d.kind,d.title,d.scope,d.source,d.evidence_json,v.distance
FROM knowledge_vectors_partitioned v
JOIN knowledge_vector_documents d ON d.vector_row_id=v.rowid
WHERE v.embedding MATCH ? AND k=? AND v.work_id=? AND v.model_id=?
  AND d.work_id=? AND d.status='ready' AND d.model_id=?`
	args := []any{string(queryVector), k, workID, i.embedder.ModelID(), workID, i.embedder.ModelID()}
	if input.Scope != "all" {
		statement += ` AND v.scope=? AND d.scope=?`
		args = append(args, input.Scope)
		args = append(args, input.Scope)
	}
	if len(input.Kinds) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(input.Kinds)), ",")
		statement += ` AND v.kind IN (` + placeholders + `)`
		for _, kind := range input.Kinds {
			args = append(args, kind)
		}
	}
	if !input.IncludeSpine {
		statement += ` AND v.is_spine=0`
	}
	statement += ` ORDER BY v.distance`
	rows, err := i.database.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query context vectors: %w", err)
	}
	results := make([]appagent.ContextSearchResult, 0, input.Limit)
	seen := make(map[string]int)
	for rows.Next() {
		var result appagent.ContextSearchResult
		var evidenceJSON string
		var distance float64
		if err := rows.Scan(&result.NodeID, &result.VersionID, &result.ArchiveID, &result.Revision, &result.Kind, &result.Title, &result.Scope, &result.Source, &evidenceJSON, &distance); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &result.Evidence); err != nil {
			rows.Close()
			return nil, fmt.Errorf("decode context search evidence: %w", err)
		}
		result.Score = 1 / (1 + distance)
		key := contextSearchResultKey(result)
		if index, exists := seen[key]; exists {
			results[index].Evidence = append(results[index].Evidence, result.Evidence...)
			continue
		}
		seen[key] = len(results)
		results = append(results, result)
		if len(results) >= input.Limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var graphResults []appagent.ContextSearchResult
	if input.Scope != "archive" {
		graphResults, err = i.searchRelatedNodes(ctx, workID, input)
	}
	if err != nil {
		return nil, err
	}
	for _, result := range graphResults {
		key := contextSearchResultKey(result)
		if index, exists := seen[key]; exists {
			results[index].Evidence = append(results[index].Evidence, result.Evidence...)
			if result.Score > results[index].Score {
				results[index].Score = result.Score
			}
			continue
		}
		results = append(results, result)
		seen[key] = len(results) - 1
	}
	sort.SliceStable(results, func(left, right int) bool {
		return results[left].Score > results[right].Score
	})
	if len(results) > input.Limit {
		results = results[:input.Limit]
	}
	return results, nil
}
func contextSearchResultKey(result appagent.ContextSearchResult) string {
	return strings.Join([]string{result.NodeID, result.VersionID, result.ArchiveID, result.Scope}, "|")
}

func (i *ContextIndex) searchRelatedNodes(ctx context.Context, workID string, input appagent.ContextSearchInput) ([]appagent.ContextSearchResult, error) {
	if len(input.SeedNodeIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(input.SeedNodeIDs)), ",")
	statement := `
WITH RECURSIVE related(node_id,depth) AS (
    SELECT id,0 FROM canvas_nodes WHERE work_id=? AND id IN (` + placeholders + `)
    UNION
    SELECT CASE WHEN e.source_node_id=related.node_id THEN e.target_node_id ELSE e.source_node_id END, related.depth+1
    FROM related JOIN canvas_edges e ON e.work_id=? AND (e.source_node_id=related.node_id OR e.target_node_id=related.node_id)
    WHERE related.depth < ?
)
SELECT n.id,n.current_version_id,n.revision,n.kind,n.title,MIN(related.depth)
FROM related JOIN canvas_nodes n ON n.work_id=? AND n.id=related.node_id
WHERE related.depth > 0 AND n.current_version_id <> ''
GROUP BY n.id,n.current_version_id,n.revision,n.kind,n.title
ORDER BY MIN(related.depth),n.id`
	args := []any{workID}
	for _, id := range input.SeedNodeIDs {
		args = append(args, id)
	}
	args = append(args, workID, input.MaxRelationHops, workID)
	rows, err := i.database.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]appagent.ContextSearchResult, 0, input.Limit)
	for rows.Next() {
		var result appagent.ContextSearchResult
		var depth int
		if err := rows.Scan(&result.NodeID, &result.VersionID, &result.Revision, &result.Kind, &result.Title, &depth); err != nil {
			return nil, err
		}
		if len(input.Kinds) > 0 && !containsString(input.Kinds, result.Kind) {
			continue
		}
		result.Scope = "current"
		result.Source = "graph"
		result.Score = 0.8 / float64(depth+1)
		result.Evidence = []string{fmt.Sprintf("related to seed node within %d graph hop(s)", depth)}
		results = append(results, result)
		if len(results) >= input.Limit {
			break
		}
	}
	return results, rows.Err()
}

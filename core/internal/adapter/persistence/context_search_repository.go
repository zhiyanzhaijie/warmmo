package persistence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

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
	database *gorm.DB
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
	return NewContextIndexWithDatabase(providerRepository.databaseHost, embedder)
}

func NewContextIndexWithDatabase(database *Database, embedder ContextEmbedder) *ContextIndex {
	if database == nil {
		return &ContextIndex{embedder: embedder}
	}
	return &ContextIndex{database: database.DB, embedder: embedder}
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
	now := time.Now().UTC()
	if err := i.database.WithContext(ctx).Model(&knowledgeIndexJobModel{}).Where("status = ?", "processing").Updates(map[string]any{"status": "pending", "last_error": "", "updated_at": now}).Error; err != nil {
		return err
	}
	if err := i.retireLegacyArchiveNodeVersions(ctx); err != nil {
		return err
	}
	if err := i.database.WithContext(ctx).Exec(`
INSERT INTO knowledge_index_jobs(work_id,object_type,object_id,status,created_at,updated_at)
SELECT n.work_id,'node-version',n.current_version_id,'pending',?,?
FROM canvas_nodes n
WHERE n.current_version_id<>'' AND NOT EXISTS (
    SELECT 1 FROM knowledge_vector_documents d
    WHERE d.work_id=n.work_id AND d.version_id=n.current_version_id AND d.scope='current' AND d.model_id=? AND d.status='ready'
)
	ON CONFLICT(work_id,object_type,object_id) DO UPDATE SET status='pending',attempts=0,last_error='',updated_at=excluded.updated_at`, now, now, i.embedder.ModelID()).Error; err != nil {
		return err
	}
	if err := i.database.WithContext(ctx).Exec(`
INSERT INTO knowledge_index_jobs(work_id,object_type,object_id,status,created_at,updated_at)
SELECT a.work_id,'archive',a.id,'pending',?,?
FROM chapter_archives a
	WHERE a.retracted_at IS NULL AND NOT EXISTS (
    SELECT 1 FROM knowledge_vector_documents d
    WHERE d.work_id=a.work_id AND d.object_type='archive' AND d.object_id=a.id AND d.model_id=? AND d.status='ready'
)
	ON CONFLICT(work_id,object_type,object_id) DO UPDATE SET status='pending',attempts=0,last_error='',updated_at=excluded.updated_at`, now, now, i.embedder.ModelID()).Error; err != nil {
		return err
	}
	err := i.database.WithContext(ctx).Exec(`
INSERT INTO knowledge_index_jobs(work_id,object_type,object_id,status,created_at,updated_at)
SELECT s.work_id,'archive-section',s.archive_id || ':' || s.chapter_section_node_id,'pending',?,?
FROM chapter_archive_sections s
JOIN chapter_archives a ON a.id=s.archive_id AND a.work_id=s.work_id
	WHERE a.retracted_at IS NULL AND NOT EXISTS (
    SELECT 1 FROM knowledge_vector_documents d
    WHERE d.work_id=s.work_id AND d.object_type='archive-section'
      AND d.object_id=s.archive_id || ':' || s.chapter_section_node_id
      AND d.model_id=? AND d.status='ready'
)
	ON CONFLICT(work_id,object_type,object_id) DO UPDATE SET status='pending',attempts=0,last_error='',updated_at=excluded.updated_at`, now, now, i.embedder.ModelID()).Error
	return err
}

func (i *ContextIndex) retireLegacyArchiveNodeVersions(ctx context.Context) error {
	var documents []knowledgeVectorDocumentModel
	db := i.database.WithContext(ctx)
	if err := db.Select("vector_row_id").Where("object_type = ? AND scope = ? AND status = ?", "node-version", "archive", "ready").Find(&documents).Error; err != nil {
		return err
	}
	ids := vectorDocumentIDs(documents)
	if err := db.Model(&knowledgeVectorDocumentModel{}).Where("object_type = ? AND scope = ?", "node-version", "archive").Updates(map[string]any{"status": "retired", "indexed_at": time.Now().UTC()}).Error; err != nil {
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
	var job contextIndexJob
	claimed := false
	err := i.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var model knowledgeIndexJobModel
		if err := tx.Where("status = ?", "pending").Order("updated_at, id").First(&model).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		result := tx.Model(&knowledgeIndexJobModel{}).Where("id = ? AND status = ?", model.ID, "pending").Updates(map[string]any{"status": "processing", "attempts": gorm.Expr("attempts + 1"), "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		job = contextIndexJob{ID: model.ID, WorkID: model.WorkID, ObjectType: model.ObjectType, ObjectID: model.ObjectID}
		claimed = true
		return nil
	})
	return job, claimed, err
}

func (i *ContextIndex) finishJob(ctx context.Context, jobID int64, success bool, message string) error {
	status := "failed"
	if success {
		status = "ready"
	}
	updates := map[string]any{"status": status, "last_error": message, "updated_at": time.Now().UTC()}
	if !success {
		var job knowledgeIndexJobModel
		if err := i.database.WithContext(ctx).Select("attempts").First(&job, jobID).Error; err != nil {
			return err
		}
		if job.Attempts < 3 {
			updates["status"] = "pending"
		}
	}
	return i.database.WithContext(ctx).Model(&knowledgeIndexJobModel{}).Where("id = ?", jobID).Updates(updates).Error
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
	db := i.database.WithContext(ctx)
	var version canvasNodeVersionModel
	if err := db.Where("work_id = ? AND id = ?", workID, versionID).First(&version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var node canvasNodeModel
	if err := db.Select("id", "kind", "revision", "current_version_id").Where("work_id = ? AND id = ?", workID, version.NodeID).First(&node).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if node.CurrentVersionID != versionID {
		return i.retireDocuments(ctx, workID, "node-version", versionID)
	}
	if err := i.retireOtherCurrentVersions(ctx, workID, version.NodeID, versionID); err != nil {
		return err
	}
	return i.upsertDocument(ctx, contextDocument{
		WorkID: workID, ObjectType: "node-version", ObjectID: versionID,
		NodeID: version.NodeID, VersionID: versionID, Revision: node.Revision,
		Kind: node.Kind, Title: version.Title, Content: version.Title + "\n" + version.Content,
		Scope: "current", Source: "canvas.current",
	})
}

func (i *ContextIndex) retireOtherCurrentVersions(ctx context.Context, workID, nodeID, currentVersionID string) error {
	db := i.database.WithContext(ctx)
	var documents []knowledgeVectorDocumentModel
	query := db.Where("work_id = ? AND node_id = ? AND scope = ? AND version_id <> ? AND status = ?", workID, nodeID, "current", currentVersionID, "ready")
	if err := query.Select("vector_row_id").Find(&documents).Error; err != nil {
		return err
	}
	if err := query.Updates(map[string]any{"status": "retired", "indexed_at": time.Now().UTC()}).Error; err != nil {
		return err
	}
	return i.deleteVectors(ctx, vectorDocumentIDs(documents))
}

func (i *ContextIndex) indexArchive(ctx context.Context, workID, archiveID string) error {
	var archive chapterArchiveModel
	if err := i.database.WithContext(ctx).Where("work_id = ? AND id = ? AND retracted_at IS NULL", workID, archiveID).First(&archive).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return i.retireArchiveDocuments(ctx, workID, archiveID)
		}
		return err
	}
	return i.upsertDocument(ctx, contextDocument{
		WorkID: workID, ObjectType: "archive", ObjectID: archiveID, ArchiveID: archiveID,
		NodeID: archive.ChapterOutlineNodeID, VersionID: archive.OutlineVersionID, Revision: archive.OutlineRevision, Kind: string(canvas.NodeKindChapterOutline),
		Title: archive.OutlineTitle, Content: archive.OutlineTitle + "\n" + archive.Summary + "\n" + archive.OutlineContent,
		Scope: "archive", Source: "chapter-archive",
	})
}

func (i *ContextIndex) indexArchiveSection(ctx context.Context, workID, objectID string) error {
	archiveID, sectionNodeID, ok := strings.Cut(objectID, ":")
	if !ok || archiveID == "" || sectionNodeID == "" {
		return fmt.Errorf("invalid archive section index id %q", objectID)
	}
	db := i.database.WithContext(ctx)
	var archive chapterArchiveModel
	if err := db.Select("id").Where("id = ? AND work_id = ? AND retracted_at IS NULL", archiveID, workID).First(&archive).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return i.retireDocuments(ctx, workID, "archive-section", objectID)
	} else if err != nil {
		return err
	}
	var section chapterArchiveSectionModel
	if err := db.Where("work_id = ? AND archive_id = ? AND chapter_section_node_id = ?", workID, archiveID, sectionNodeID).First(&section).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return i.retireDocuments(ctx, workID, "archive-section", objectID)
	} else if err != nil {
		return err
	}
	return i.upsertDocument(ctx, contextDocument{
		WorkID: workID, ObjectType: "archive-section", ObjectID: objectID,
		NodeID: sectionNodeID, VersionID: section.ChapterSectionVersionID, ArchiveID: archiveID, Revision: section.NodeRevision,
		Kind:  string(canvas.NodeKindChapterSection),
		Title: section.Title, Content: section.Title + "\n" + section.Summary + "\n" + section.Content,
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
	db := i.database.WithContext(ctx)
	var existing knowledgeVectorDocumentModel
	identity := db.Where("work_id = ? AND object_type = ? AND object_id = ? AND chunk_index = 0 AND model_id = ? AND scope = ?", document.WorkID, document.ObjectType, document.ObjectID, i.embedder.ModelID(), document.Scope)
	err := identity.First(&existing).Error
	if err == nil && existing.ContentHash == hash && existing.Status == "ready" {
		return nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
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
	return db.Transaction(func(tx *gorm.DB) error {
		var stored knowledgeVectorDocumentModel
		err := tx.Where("work_id = ? AND object_type = ? AND object_id = ? AND chunk_index = 0 AND model_id = ? AND scope = ?", document.WorkID, document.ObjectType, document.ObjectID, i.embedder.ModelID(), document.Scope).First(&stored).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			if err := tx.Exec(`DELETE FROM knowledge_vectors_partitioned WHERE rowid = ?`, stored.VectorRowID).Error; err != nil {
				return err
			}
		} else {
			stored = knowledgeVectorDocumentModel{WorkID: document.WorkID, ObjectType: document.ObjectType, ObjectID: document.ObjectID, NodeID: document.NodeID, VersionID: document.VersionID, ArchiveID: document.ArchiveID, Revision: document.Revision, ChunkIndex: 0, ModelID: i.embedder.ModelID(), ContentHash: hash, Scope: document.Scope, Kind: document.Kind, Title: document.Title, Source: document.Source, EvidenceJSON: string(evidenceJSON), Status: "pending"}
			if err := tx.Create(&stored).Error; err != nil {
				return err
			}
		}
		isSpine := document.ObjectType == "archive" || document.ObjectType == "archive-section"
		if err := tx.Exec(`INSERT INTO knowledge_vectors_partitioned(rowid,work_id,model_id,scope,kind,is_spine,embedding) VALUES (?,?,?,?,?,?,?)`, stored.VectorRowID, document.WorkID, i.embedder.ModelID(), document.Scope, document.Kind, isSpine, string(vector)).Error; err != nil {
			return err
		}
		return tx.Model(&knowledgeVectorDocumentModel{}).Where("vector_row_id = ?", stored.VectorRowID).Updates(map[string]any{"work_id": document.WorkID, "node_id": document.NodeID, "version_id": document.VersionID, "archive_id": document.ArchiveID, "revision": document.Revision, "content_hash": hash, "kind": document.Kind, "title": document.Title, "source": document.Source, "evidence_json": string(evidenceJSON), "status": "ready", "indexed_at": time.Now().UTC()}).Error
	})
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
	db := i.database.WithContext(ctx)
	var documents []knowledgeVectorDocumentModel
	query := db.Where("work_id = ? AND object_type = ? AND object_id = ?", workID, objectType, objectID)
	if err := query.Where("status = ?", "ready").Select("vector_row_id").Find(&documents).Error; err != nil {
		return err
	}
	if err := query.Updates(map[string]any{"status": "retired", "indexed_at": time.Now().UTC()}).Error; err != nil {
		return err
	}
	return i.deleteVectors(ctx, vectorDocumentIDs(documents))
}

func (i *ContextIndex) retireArchiveDocuments(ctx context.Context, workID, archiveID string) error {
	db := i.database.WithContext(ctx)
	var documents []knowledgeVectorDocumentModel
	query := db.Where("work_id = ? AND archive_id = ?", workID, archiveID)
	if err := query.Where("status = ?", "ready").Select("vector_row_id").Find(&documents).Error; err != nil {
		return err
	}
	if err := query.Updates(map[string]any{"status": "retired", "indexed_at": time.Now().UTC()}).Error; err != nil {
		return err
	}
	return i.deleteVectors(ctx, vectorDocumentIDs(documents))
}

func vectorDocumentIDs(documents []knowledgeVectorDocumentModel) []int64 {
	ids := make([]int64, len(documents))
	for index, document := range documents {
		ids[index] = document.VectorRowID
	}
	return ids
}

func (i *ContextIndex) deleteVectors(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		if err := i.database.WithContext(ctx).Exec(`DELETE FROM knowledge_vectors_partitioned WHERE rowid = ?`, id).Error; err != nil {
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
	rows, err := i.database.WithContext(ctx).Raw(statement, args...).Rows()
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
	rows, err := i.database.WithContext(ctx).Raw(statement, args...).Rows()
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

package persistence

import "time"

type schemaMetadataModel struct {
	Key   string `gorm:"primaryKey;size:64"`
	Value string `gorm:"not null"`
}

func (schemaMetadataModel) TableName() string { return "schema_metadata" }

type providerConfigurationModel struct {
	ID               string    `gorm:"primaryKey"`
	ProviderID       string    `gorm:"uniqueIndex;not null"`
	BaseURL          string    `gorm:"not null"`
	ModelIDs         []string  `gorm:"column:model_ids_json;serializer:json;not null"`
	SecretNonce      []byte    `gorm:"not null"`
	SecretCiphertext []byte    `gorm:"not null"`
	APIKeyHint       string    `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"index:idx_provider_updated,sort:desc;not null"`
}

func (providerConfigurationModel) TableName() string { return "agent_provider_configurations" }

type workFolderModel struct {
	ID        string      `gorm:"primaryKey"`
	Name      string      `gorm:"uniqueIndex;not null"`
	SortOrder int         `gorm:"index:idx_folder_sort_name,priority:1;not null;default:0"`
	CreatedAt time.Time   `gorm:"not null"`
	UpdatedAt time.Time   `gorm:"not null"`
	Works     []workModel `gorm:"foreignKey:FolderID;references:ID"`
}

func (workFolderModel) TableName() string { return "work_folders" }

type workModel struct {
	ID          string           `gorm:"primaryKey"`
	Title       string           `gorm:"not null"`
	Description string           `gorm:"not null;default:''"`
	FolderID    string           `gorm:"index:idx_work_status_folder_updated,priority:2;not null;default:''"`
	Status      string           `gorm:"index:idx_work_status_folder_updated,priority:1;not null;default:active"`
	Revision    int64            `gorm:"not null;default:1"`
	CreatedAt   time.Time        `gorm:"not null"`
	UpdatedAt   time.Time        `gorm:"index:idx_work_status_folder_updated,priority:3,sort:desc;index:idx_works_updated,sort:desc;not null"`
	Folder      *workFolderModel `gorm:"foreignKey:FolderID;references:ID"`
}

func (workModel) TableName() string { return "works" }

type agentRunModel struct {
	ID             string                `gorm:"primaryKey"`
	WorkID         string                `gorm:"index:idx_agent_run_work_created,priority:1;not null"`
	Status         string                `gorm:"index;not null"`
	Prompt         string                `gorm:"not null"`
	Target         string                `gorm:"not null"`
	TargetNodeID   string                `gorm:"not null;default:''"`
	ProviderID     string                `gorm:"not null"`
	ModelID        string                `gorm:"not null"`
	ContextNodeIDs []string              `gorm:"column:context_node_ids_json;serializer:json;not null"`
	ErrorMessage   string                `gorm:"not null;default:''"`
	CreatedAt      time.Time             `gorm:"index:idx_agent_run_work_created,priority:2,sort:desc;not null"`
	UpdatedAt      time.Time             `gorm:"not null"`
	Events         []agentRunEventModel  `gorm:"foreignKey:RunID;constraint:OnDelete:CASCADE"`
	Responses      []agentResponseModel  `gorm:"foreignKey:RunID;constraint:OnDelete:CASCADE"`
	Candidates     []agentCandidateModel `gorm:"foreignKey:RunID;constraint:OnDelete:CASCADE"`
}

func (agentRunModel) TableName() string { return "agent_runs" }

type agentRunEventModel struct {
	ID        string    `gorm:"primaryKey"`
	RunID     string    `gorm:"uniqueIndex:idx_run_event_sequence,priority:1;index:idx_run_event_order,priority:1;not null"`
	Sequence  int64     `gorm:"uniqueIndex:idx_run_event_sequence,priority:2;index:idx_run_event_order,priority:2;not null"`
	Type      string    `gorm:"not null"`
	DataJSON  string    `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`
}

func (agentRunEventModel) TableName() string { return "agent_run_events" }

type agentResponseModel struct {
	ID              string    `gorm:"primaryKey"`
	RunID           string    `gorm:"uniqueIndex:idx_run_response_approval,priority:1;index:idx_response_run_created,priority:1;not null"`
	ApprovalEventID string    `gorm:"uniqueIndex:idx_run_response_approval,priority:2;not null"`
	Answer          string    `gorm:"not null"`
	CreatedAt       time.Time `gorm:"index:idx_response_run_created,priority:2;not null"`
}

func (agentResponseModel) TableName() string { return "agent_user_responses" }

type agentCandidateModel struct {
	ID             string    `gorm:"primaryKey"`
	RunID          string    `gorm:"index;not null"`
	WorkID         string    `gorm:"index:idx_candidate_work_status_created,priority:1;not null"`
	SkillID        string    `gorm:"not null"`
	SkillVersion   string    `gorm:"not null"`
	Status         string    `gorm:"index:idx_candidate_work_status_created,priority:2;not null;default:pending"`
	Kind           string    `gorm:"not null;default:chapter-section"`
	Title          string    `gorm:"not null"`
	Content        string    `gorm:"not null"`
	X              float64   `gorm:"not null;default:520"`
	Y              float64   `gorm:"not null;default:80"`
	AcceptedNodeID string    `gorm:"not null;default:''"`
	CreatedAt      time.Time `gorm:"index:idx_candidate_work_status_created,priority:3,sort:desc;not null"`
	DecidedAt      *time.Time
	CandidateType  string  `gorm:"not null;default:node"`
	NodeID         string  `gorm:"not null;default:''"`
	BaseVersionID  string  `gorm:"not null;default:''"`
	Reason         string  `gorm:"not null;default:''"`
	ChangeScore    float64 `gorm:"not null;default:0"`
}

func (agentCandidateModel) TableName() string { return "agent_candidates" }

type canvasNodeModel struct {
	ID               string                   `gorm:"primaryKey"`
	WorkID           string                   `gorm:"index:idx_canvas_node_work_created,priority:1;index:idx_canvas_node_work_updated,priority:1;not null"`
	Revision         int64                    `gorm:"not null"`
	Kind             string                   `gorm:"not null"`
	Title            string                   `gorm:"not null"`
	Content          string                   `gorm:"not null"`
	X                float64                  `gorm:"not null;default:0"`
	Y                float64                  `gorm:"not null;default:0"`
	CurrentVersionID string                   `gorm:"not null;default:''"`
	CreatedAt        time.Time                `gorm:"index:idx_canvas_node_work_created,priority:2;not null"`
	UpdatedAt        time.Time                `gorm:"index:idx_canvas_node_work_updated,priority:2,sort:desc;not null"`
	Versions         []canvasNodeVersionModel `gorm:"foreignKey:NodeID;constraint:OnDelete:CASCADE"`
}

func (canvasNodeModel) TableName() string { return "canvas_nodes" }

type canvasNodeVersionModel struct {
	ID              string    `gorm:"primaryKey"`
	NodeID          string    `gorm:"uniqueIndex:idx_node_version_number,priority:1;index:idx_node_version_order,priority:1;not null"`
	WorkID          string    `gorm:"index;not null"`
	VersionNumber   int64     `gorm:"uniqueIndex:idx_node_version_number,priority:2;index:idx_node_version_order,priority:2,sort:desc;not null"`
	ParentVersionID string    `gorm:"not null;default:''"`
	Title           string    `gorm:"not null"`
	Content         string    `gorm:"not null"`
	SourceRunID     string    `gorm:"not null;default:''"`
	CreatedAt       time.Time `gorm:"not null"`
}

func (canvasNodeVersionModel) TableName() string { return "canvas_node_versions" }

type canvasEdgeModel struct {
	ID           string    `gorm:"primaryKey"`
	WorkID       string    `gorm:"uniqueIndex:idx_canvas_edge_unique,priority:1;index:idx_canvas_edge_work_created,priority:1;not null"`
	SourceNodeID string    `gorm:"uniqueIndex:idx_canvas_edge_unique,priority:2;not null"`
	TargetNodeID string    `gorm:"uniqueIndex:idx_canvas_edge_unique,priority:3;not null"`
	Kind         string    `gorm:"uniqueIndex:idx_canvas_edge_unique,priority:4;not null"`
	CreatedAt    time.Time `gorm:"index:idx_canvas_edge_work_created,priority:2;not null"`
}

func (canvasEdgeModel) TableName() string { return "canvas_edges" }

type canvasActionModel struct {
	ID          string    `gorm:"primaryKey"`
	WorkID      string    `gorm:"uniqueIndex:idx_canvas_action_sequence,priority:1;index:idx_canvas_action_order,priority:1;not null"`
	Sequence    int64     `gorm:"uniqueIndex:idx_canvas_action_sequence,priority:2;index:idx_canvas_action_order,priority:2;not null"`
	ActionType  string    `gorm:"not null"`
	Label       string    `gorm:"not null"`
	PayloadJSON string    `gorm:"not null"`
	CreatedAt   time.Time `gorm:"not null"`
}

func (canvasActionModel) TableName() string { return "canvas_actions" }

type canvasHistoryStateModel struct {
	WorkID          string `gorm:"primaryKey"`
	CurrentSequence int64  `gorm:"not null"`
	CurrentActionID string `gorm:"not null"`
}

func (canvasHistoryStateModel) TableName() string { return "canvas_history_state" }

type chapterArchiveModel struct {
	ID                   string    `gorm:"primaryKey"`
	WorkID               string    `gorm:"uniqueIndex:idx_archive_revision,priority:1;index:idx_archive_work_created,priority:1;not null"`
	ChapterOutlineNodeID string    `gorm:"uniqueIndex:idx_archive_revision,priority:2;not null"`
	Revision             int64     `gorm:"uniqueIndex:idx_archive_revision,priority:3;not null"`
	RunID                string    `gorm:"not null"`
	OutlineVersionID     string    `gorm:"not null;default:''"`
	OutlineRevision      int64     `gorm:"not null"`
	OutlineTitle         string    `gorm:"not null"`
	OutlineContent       string    `gorm:"not null"`
	Summary              string    `gorm:"not null"`
	SourceDigest         string    `gorm:"not null"`
	IsCurrent            bool      `gorm:"index;not null;default:true"`
	ProjectionStatus     string    `gorm:"not null;default:pending"`
	CreatedAt            time.Time `gorm:"index:idx_archive_work_created,priority:2;not null"`
	SupersededAt         *time.Time
	RetractedAt          *time.Time                   `gorm:"index:idx_archive_work_retracted_created,priority:2"`
	Sections             []chapterArchiveSectionModel `gorm:"foreignKey:ArchiveID;constraint:OnDelete:CASCADE"`
}

func (chapterArchiveModel) TableName() string { return "chapter_archives" }

type chapterArchiveSectionModel struct {
	ArchiveID               string `gorm:"primaryKey;uniqueIndex:idx_archive_section_ordinal,priority:1;not null"`
	ChapterSectionNodeID    string `gorm:"primaryKey;not null"`
	WorkID                  string `gorm:"index:idx_archive_section_work_node,priority:1;not null"`
	Ordinal                 int    `gorm:"uniqueIndex:idx_archive_section_ordinal,priority:2;not null"`
	SectionOutlineNodeID    string `gorm:"not null"`
	ChapterSectionVersionID string `gorm:"not null;default:''"`
	NodeRevision            int64  `gorm:"not null"`
	Title                   string `gorm:"not null"`
	Summary                 string `gorm:"not null"`
	Content                 string `gorm:"not null"`
	ContentHash             string `gorm:"not null"`
}

func (chapterArchiveSectionModel) TableName() string { return "chapter_archive_sections" }

type knowledgeVectorDocumentModel struct {
	VectorRowID  int64  `gorm:"primaryKey;autoIncrement"`
	WorkID       string `gorm:"index:idx_vector_document_work_scope,priority:1;index:idx_vector_document_node_scope,priority:1;not null"`
	ObjectType   string `gorm:"uniqueIndex:idx_vector_document_identity,priority:1;not null"`
	ObjectID     string `gorm:"uniqueIndex:idx_vector_document_identity,priority:2;not null"`
	NodeID       string `gorm:"index:idx_vector_document_node_scope,priority:2;not null;default:''"`
	VersionID    string `gorm:"not null;default:''"`
	ArchiveID    string `gorm:"index;not null;default:''"`
	Revision     int64  `gorm:"not null;default:0"`
	ChunkIndex   int    `gorm:"uniqueIndex:idx_vector_document_identity,priority:3;not null;default:0"`
	ModelID      string `gorm:"uniqueIndex:idx_vector_document_identity,priority:4;not null"`
	ContentHash  string `gorm:"not null"`
	Scope        string `gorm:"uniqueIndex:idx_vector_document_identity,priority:5;index:idx_vector_document_work_scope,priority:2;index:idx_vector_document_node_scope,priority:3;not null"`
	Kind         string `gorm:"not null;default:''"`
	Title        string `gorm:"not null;default:''"`
	Source       string `gorm:"not null"`
	EvidenceJSON string `gorm:"not null;default:'[]'"`
	Status       string `gorm:"index:idx_vector_document_work_scope,priority:3;index:idx_vector_document_node_scope,priority:4;not null"`
	IndexedAt    *time.Time
}

func (knowledgeVectorDocumentModel) TableName() string { return "knowledge_vector_documents" }

type knowledgeIndexJobModel struct {
	ID         int64     `gorm:"primaryKey;autoIncrement"`
	WorkID     string    `gorm:"uniqueIndex:idx_index_job_identity,priority:1;not null"`
	ObjectType string    `gorm:"uniqueIndex:idx_index_job_identity,priority:2;not null"`
	ObjectID   string    `gorm:"uniqueIndex:idx_index_job_identity,priority:3;not null"`
	Status     string    `gorm:"index:idx_index_job_pending,priority:1;not null;default:pending"`
	Attempts   int       `gorm:"not null;default:0"`
	LastError  string    `gorm:"not null;default:''"`
	CreatedAt  time.Time `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"index:idx_index_job_pending,priority:2;not null"`
}

func (knowledgeIndexJobModel) TableName() string { return "knowledge_index_jobs" }

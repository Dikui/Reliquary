package interfaces

import (
	"context"
	"io"
	"mime/multipart"

	"github.com/hibiken/asynq"
	"reliquary/internal/types"
)

// KnowledgeService defines the interface for knowledge services.
type KnowledgeService interface {
	// CreateKnowledgeFromFile creates knowledge from a file.
	// channel identifies the ingestion channel (e.g. "web", "api", "feishu"); empty defaults to "web".
	CreateKnowledgeFromFile(
		ctx context.Context,
		kbID string,
		file *multipart.FileHeader,
		metadata map[string]string,
		enableMultimodel *bool,
		customFileName string,
		tagIDs []string,
		channel string,
		processOverrides *types.KnowledgeProcessOverrides,
	) (*types.Knowledge, error)
	// CreateKnowledgeFromURL creates knowledge from a URL.
	// When fileName or fileType is provided (or the URL path has a known file extension),
	// the URL is treated as a direct file download instead of a web page crawl.
	// channel identifies the ingestion channel; empty defaults to "web".
	CreateKnowledgeFromURL(
		ctx context.Context,
		kbID string,
		url string,
		fileName string,
		fileType string,
		enableMultimodel *bool,
		title string,
		tagIDs []string,
		channel string,
		processOverrides *types.KnowledgeProcessOverrides,
		sourceInfo ...types.SourceInfo,
	) (*types.Knowledge, error)
	// CreateKnowledgeFromPassage creates knowledge from text passages.
	// channel identifies the ingestion channel; empty defaults to "web".
	CreateKnowledgeFromPassage(ctx context.Context, kbID string, passage []string, channel string) (*types.Knowledge, error)
	// CreateKnowledgeFromPassageSync creates knowledge from text passages and waits until chunks are indexed.
	CreateKnowledgeFromPassageSync(ctx context.Context, kbID string, passage []string, channel string) (*types.Knowledge, error)
	// CreateKnowledgeFromManual creates or saves manual Markdown knowledge content.
	// channel identifies the ingestion channel; empty defaults to "web".
	CreateKnowledgeFromManual(
		ctx context.Context,
		kbID string,
		payload *types.ManualKnowledgePayload,
		channel string,
	) (*types.Knowledge, error)
	// ReviewRSSUpdate stages a changed article and promotes it after review.
	// Approved originals remain as historical evidence; a rejected update does not delete them.
	// rejected is true only when the new original failed quality review, not on execution errors.
	ReviewRSSUpdate(ctx context.Context, oldID string, payload *types.ManualKnowledgePayload) (rejected bool, err error)
	// GetKnowledgeByID retrieves knowledge by ID (uses tenant from context).
	GetKnowledgeByID(ctx context.Context, id string) (*types.Knowledge, error)
	// GetKnowledgeByIDOnly retrieves knowledge by ID without tenant filter (for permission resolution).
	GetKnowledgeByIDOnly(ctx context.Context, id string) (*types.Knowledge, error)

	// GetKnowledgeBatch retrieves a batch of knowledge by IDs.
	GetKnowledgeBatch(ctx context.Context, tenantID uint64, ids []string) ([]*types.Knowledge, error)
	// ListKnowledgeIDsByTagIDs returns document knowledge IDs carrying any of
	// the specified KB-local tags.
	ListKnowledgeIDsByTagIDs(ctx context.Context, tenantID uint64, kbID string, tagIDs []string) ([]string, error)
	// ListKnowledgeByKnowledgeBaseID lists all knowledge under a knowledge base.
	ListKnowledgeByKnowledgeBaseID(ctx context.Context, kbID string) ([]*types.Knowledge, error)
	// ListPagedKnowledgeByKnowledgeBaseID lists all knowledge under a knowledge base
	// with pagination. The filter struct controls optional dimensions (tag, keyword,
	// file type, parse status, source channel, updated time range); pass a zero
	// struct to disable all filters.
	ListPagedKnowledgeByKnowledgeBaseID(
		ctx context.Context,
		kbID string,
		page *types.Pagination,
		filter types.KnowledgeListFilter,
	) (*types.PageResult, error)
	// ListKnowledgeFolderTree returns the folder hierarchy derived from the
	// folder_path of every knowledge entry in a knowledge base, with per-folder
	// document counts. It powers the document sidebar tree.
	ListKnowledgeFolderTree(ctx context.Context, kbID string) (*types.KnowledgeFolderTree, error)
	// MoveKnowledgeToFolder re-files knowledge entries under the given folder
	// path (empty means the knowledge base top level). Folders are derived from
	// the stored paths, so a path that does not exist yet is created implicitly.
	MoveKnowledgeToFolder(
		ctx context.Context,
		kbID string,
		ids []string,
		folderPath string,
	) (int64, error)
	// RenameKnowledgeFolder moves a folder and everything below it to a new path.
	RenameKnowledgeFolder(ctx context.Context, kbID string, from string, to string) (int64, error)
	// DeleteKnowledge deletes knowledge by ID.
	DeleteKnowledge(ctx context.Context, id string) error
	// DeleteKnowledgeList deletes multiple knowledge entries by IDs.
	DeleteKnowledgeList(ctx context.Context, ids []string) error
	// GetKnowledgeFile retrieves the file associated with the knowledge.
	GetKnowledgeFile(ctx context.Context, id string) (io.ReadCloser, string, error)
	ReviewKnowledgeIntake(ctx context.Context, id, action string, sourceInfo types.SourceInfo) (*types.Knowledge, error)
	// UpdateKnowledge updates knowledge information.
	UpdateKnowledge(ctx context.Context, knowledge *types.Knowledge) error
	// RegenerateKnowledgeSummary refreshes the document description and summary retrieval chunk.
	RegenerateKnowledgeSummary(ctx context.Context, knowledgeID string) (*types.Knowledge, error)
	// RequestKnowledgeSummaryRefresh enqueues an async summary refresh.
	RequestKnowledgeSummaryRefresh(ctx context.Context, knowledgeID string) error
	// RegenerateChunkQuestions rebuilds the auxiliary questions for one current chunk revision.
	RegenerateChunkQuestions(ctx context.Context, chunkID string) ([]types.GeneratedQuestion, error)
	// UpdateManualKnowledge updates manual Markdown knowledge content.
	UpdateManualKnowledge(
		ctx context.Context,
		knowledgeID string,
		payload *types.ManualKnowledgePayload,
	) (*types.Knowledge, error)
	// ReparseKnowledge deletes existing document content and re-parses the knowledge asynchronously.
	// When processOverrides is non-nil, it is validated and persisted to the knowledge metadata
	// before re-parsing, letting callers adjust parse config on reparse; nil keeps stored overrides.
	ReparseKnowledge(
		ctx context.Context,
		knowledgeID string,
		processOverrides *types.KnowledgeProcessOverrides,
	) (*types.Knowledge, error)
	// CancelKnowledgeParse marks an in-progress parse as cancelled by the
	// user. The knowledge row and any partially written chunks/index are
	// kept; downstream queued tasks for the same knowledge are best-effort
	// dequeued and active workers are signalled to stop at their next
	// checkpoint. Idempotent — returns the existing row when the knowledge
	// is already cancelled. Returns an error when the knowledge is in a
	// terminal state (completed / failed) or being deleted.
	CancelKnowledgeParse(ctx context.Context, knowledgeID string) (*types.Knowledge, error)
	// CloneKnowledgeBase clones knowledge to another knowledge base.
	CloneKnowledgeBase(ctx context.Context, srcID, dstID string) error
	// UpdateImageInfo updates image information for a knowledge chunk.
	UpdateImageInfo(ctx context.Context, knowledgeID string, chunkID string, imageInfo string) error
	// UpdateKnowledgeTagBatch updates tag for document knowledge items in batch.
	// authorizedKBID restricts all updates to knowledge items belonging to this KB;
	// pass empty string to skip (caller must ensure authorization by other means).
	UpdateKnowledgeTagBatch(ctx context.Context, authorizedKBID string, updates map[string][]string, reason ...string) error
	// SetKnowledgeTags replaces all tags for a single knowledge entry.
	SetKnowledgeTags(ctx context.Context, knowledgeID string, tagIDs []string) error
	// GetKnowledgeTags returns tags for multiple knowledge IDs.
	GetKnowledgeTags(ctx context.Context, knowledgeIDs []string) (map[string][]*types.KnowledgeTag, error)
	// GetRepository gets the knowledge repository
	GetRepository() KnowledgeRepository
	// ProcessManualUpdate handles Asynq manual knowledge update tasks (cleanup + re-indexing)
	ProcessManualUpdate(ctx context.Context, t *asynq.Task) error
	// ProcessDocument handles Asynq document processing tasks
	ProcessDocument(ctx context.Context, t *asynq.Task) error
	// ProcessQuestionGeneration handles Asynq question generation tasks
	ProcessQuestionGeneration(ctx context.Context, t *asynq.Task) error
	// ProcessSummaryGeneration handles Asynq summary generation tasks
	ProcessSummaryGeneration(ctx context.Context, t *asynq.Task) error
	// ProcessKBClone handles Asynq knowledge base clone tasks
	ProcessKBClone(ctx context.Context, t *asynq.Task) error
	// ProcessKnowledgeMove handles Asynq knowledge move tasks
	ProcessKnowledgeMove(ctx context.Context, t *asynq.Task) error
	// ProcessKnowledgeListDelete handles Asynq knowledge list delete tasks
	ProcessKnowledgeListDelete(ctx context.Context, t *asynq.Task) error
	// ProcessKnowledgeListReparse handles Asynq knowledge list reparse tasks
	ProcessKnowledgeListReparse(ctx context.Context, t *asynq.Task) error
	// GetKBCloneProgress retrieves the progress of a knowledge base clone task
	GetKBCloneProgress(ctx context.Context, taskID string) (*types.KBCloneProgress, error)
	// SaveKBCloneProgress saves the progress of a knowledge base clone task
	SaveKBCloneProgress(ctx context.Context, progress *types.KBCloneProgress) error
	// GetKnowledgeMoveProgress retrieves the progress of a knowledge move task
	GetKnowledgeMoveProgress(ctx context.Context, taskID string) (*types.KnowledgeMoveProgress, error)
	// SaveKnowledgeMoveProgress saves the progress of a knowledge move task
	SaveKnowledgeMoveProgress(ctx context.Context, progress *types.KnowledgeMoveProgress) error
	// SearchKnowledge searches knowledge items by keyword across the tenant.
	// fileTypes: optional list of file extensions to filter by (e.g., ["csv", "xlsx"])
	SearchKnowledge(ctx context.Context, keyword string, offset, limit int, fileTypes []string) ([]*types.Knowledge, bool, int64, error)
	SearchKnowledgeForScopes(ctx context.Context, scopes []types.KnowledgeSearchScope, keyword string, offset, limit int, fileTypes []string) ([]*types.Knowledge, bool, int64, error)
}

// KnowledgeRepository defines the interface for knowledge repositories.
type KnowledgeRepository interface {
	GetKnowledgeBatch(ctx context.Context, tenantID uint64, ids []string) ([]*types.Knowledge, error)
	CreateKnowledge(ctx context.Context, knowledge *types.Knowledge) error
	GetKnowledgeByID(ctx context.Context, tenantID uint64, id string) (*types.Knowledge, error)
	// GetKnowledgeByIDOnly returns knowledge by ID without tenant filter (for permission resolution).
	GetKnowledgeByIDOnly(ctx context.Context, id string) (*types.Knowledge, error)
	ListKnowledgeByKnowledgeBaseID(ctx context.Context, tenantID uint64, kbID string) ([]*types.Knowledge, error)
	// ListPagedKnowledgeByKnowledgeBaseID lists all knowledge in a knowledge base
	// with pagination. The filter struct controls optional dimensions (tag, keyword,
	// file type, parse status, source channel, updated time range); pass a zero
	// struct to disable all filters.
	ListPagedKnowledgeByKnowledgeBaseID(ctx context.Context,
		tenantID uint64, kbID string, page *types.Pagination, filter types.KnowledgeListFilter,
	) ([]*types.Knowledge, int64, error)
	UpdateKnowledge(ctx context.Context, knowledge *types.Knowledge) error
	// UpdateKnowledgeBatch updates knowledge items in batch
	UpdateKnowledgeBatch(ctx context.Context, knowledgeList []*types.Knowledge) error
	DeleteKnowledge(ctx context.Context, tenantID uint64, id string) error
	DeleteKnowledgeList(ctx context.Context, tenantID uint64, ids []string) error
	// CheckKnowledgeExists checks if knowledge already exists.
	// For file types, check by fileHash or (fileName+fileSize).
	// For URL types, check by URL.
	// Returns whether it exists, the existing knowledge object (if any), and possible error.
	CheckKnowledgeExists(
		ctx context.Context,
		tenantID uint64,
		kbID string,
		params *types.KnowledgeCheckParams,
	) (bool, *types.Knowledge, error)
	// ListKnowledgeFolderCounts aggregates the number of knowledge entries
	// stored directly in each folder_path of a knowledge base.
	ListKnowledgeFolderCounts(
		ctx context.Context,
		tenantID uint64,
		kbID string,
	) ([]*types.KnowledgeFolderCount, error)
	// UpdateKnowledgeFolderPath files the given entries under folderPath.
	UpdateKnowledgeFolderPath(
		ctx context.Context,
		tenantID uint64,
		kbID string,
		ids []string,
		folderPath string,
	) (int64, error)
	// RenameKnowledgeFolderPath rewrites folder_path for a folder and all of its
	// descendants. Renaming onto an existing path merges the folders.
	RenameKnowledgeFolderPath(
		ctx context.Context,
		tenantID uint64,
		kbID string,
		from string,
		to string,
	) (int64, error)
	// AminusB returns the IDs of knowledge in A that have no counterpart in B,
	// comparing file_hash as a multiset (so duplicate-count differences and
	// NULL/empty hashes are handled correctly, letting a clone converge).
	AminusB(ctx context.Context, Atenant uint64, A string, Btenant uint64, B string) ([]string, error)
	UpdateKnowledgeColumn(ctx context.Context, id string, column string, value interface{}) error
	// UpdateKnowledgeColumns updates multiple columns of a knowledge row in a single
	// statement so callers that flip several related fields (e.g. parse_status +
	// error_message) cannot leave the row in a half-updated state.
	UpdateKnowledgeColumns(ctx context.Context, id string, values map[string]interface{}) error
	// UpdateActiveDeletingKnowledgeColumns updates an active, non-deleted knowledge row
	// only when it is still in the transient deleting state.
	UpdateActiveDeletingKnowledgeColumns(ctx context.Context, id string, values map[string]interface{}) (bool, error)
	// FinalizeSubtask atomically decrements pending_subtasks_count for the
	// given knowledge and promotes parse_status from "finalizing" to
	// "completed" when the count reaches zero. Returns the post-decrement
	// count, whether this caller's UPDATE was the one that promoted the
	// row, and any error.
	FinalizeSubtask(ctx context.Context, id string) (int, bool, error)
	// SetFinalizing atomically transitions a row from "processing" to
	// "finalizing" and writes the initial pending_subtasks_count. Returns
	// whether the transition took place (false when the row's parse_status
	// was no longer "processing", e.g. user cancelled / deleted in flight).
	SetFinalizing(ctx context.Context, id string, expectedSubtasks int) (bool, error)
	// CountKnowledgeByKnowledgeBaseID counts the number of knowledge items in a knowledge base.
	CountKnowledgeByKnowledgeBaseID(ctx context.Context, tenantID uint64, kbID string) (int64, error)
	// CountKnowledgeByStatus counts the number of knowledge items with the specified parse status.
	CountKnowledgeByStatus(ctx context.Context, tenantID uint64, kbID string, parseStatuses []string) (int64, error)
	// SearchKnowledge searches knowledge items by keyword across the tenant.
	// fileTypes: optional list of file extensions to filter by (e.g., ["csv", "xlsx"])
	SearchKnowledge(ctx context.Context, tenantID uint64, keyword string, offset, limit int, fileTypes []string) ([]*types.Knowledge, bool, error)

	// FindByMetadataKey finds a knowledge item by a key-value pair in the metadata JSON column.
	// Used by data source sync to locate existing items by external_id.
	FindByMetadataKey(ctx context.Context, tenantID uint64, kbID string, key string, value string) (*types.Knowledge, error)
	// FindByMetadataKeyPrefix finds knowledge items whose metadata[key] starts
	// with the given prefix. Used to sweep an external node's attachment sub-items.
	FindByMetadataKeyPrefix(ctx context.Context, tenantID uint64, kbID string, key string, prefix string) ([]*types.Knowledge, error)
	// FindByDataSourceExternalID finds a knowledge item owned by one data source
	// and identified by the source's external item ID.
	FindByDataSourceExternalID(
		ctx context.Context, tenantID uint64, kbID, dataSourceID, externalID string,
	) (*types.Knowledge, error)
	// HardDeleteKnowledge physically removes a row after DeleteKnowledge's soft-delete
	// cascade. Sync-internal deletions use this so rows never become tombstones.
	HardDeleteKnowledge(ctx context.Context, tenantID uint64, id string) error
	// HardDeleteKnowledgeList is the batch counterpart of HardDeleteKnowledge.
	HardDeleteKnowledgeList(ctx context.Context, tenantID uint64, ids []string) error
	// SearchKnowledgeInScopes searches knowledge items by keyword within the given (tenant_id, kb_id) scopes (own + shared).
	SearchKnowledgeInScopes(ctx context.Context, scopes []types.KnowledgeSearchScope, keyword string, offset, limit int, fileTypes []string) ([]*types.Knowledge, bool, int64, error)
	// ListIDsByTagIDs returns all knowledge IDs that have any of the specified tag IDs (OR semantics).
	ListIDsByTagIDs(ctx context.Context, tenantID uint64, kbID string, tagIDs []string) ([]string, error)
	// SetKnowledgeTags replaces all tags for a single knowledge entry (deletes old, inserts new).
	SetKnowledgeTags(ctx context.Context, knowledgeID string, tagIDs []string) error
	// SetKnowledgeTagsManually records explicit user corrections with the same transaction as the associations.
	SetKnowledgeTagsManually(ctx context.Context, knowledgeID string, tagIDs []string, actorID, reason string) (added, removed []string, err error)
	// AddKnowledgeTagRelations attaches tags without removing existing ones,
	// after validating that the knowledge and every tag belong to the given
	// tenant and knowledge base. Duplicate deliveries are idempotent.
	AddKnowledgeTagRelations(
		ctx context.Context,
		tenantID uint64,
		kbID, knowledgeID string,
		tagIDs []string,
	) error
	// GetKnowledgeTags returns tags for multiple knowledge IDs.
	GetKnowledgeTags(ctx context.Context, knowledgeIDs []string) (map[string][]*types.KnowledgeTag, error)
	// DeleteKnowledgeTagRelations deletes all tag relations for a knowledge entry.
	DeleteKnowledgeTagRelations(ctx context.Context, knowledgeID string) error
}

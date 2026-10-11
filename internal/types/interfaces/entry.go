package interfaces

import (
	"context"
	"io"
	"mime/multipart"

	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

// EntryService defines the interface for entry services.
type EntryService interface {
	// CreateEntryFromFile creates entry from a file.
	// channel identifies the ingestion channel (e.g. "web", "api", "rss"); empty defaults to "web".
	CreateEntryFromFile(
		ctx context.Context,
		kbID string,
		file *multipart.FileHeader,
		metadata map[string]string,
		enableMultimodel *bool,
		customFileName string,
		channel string,
		processOverrides *types.EntryProcessOverrides,
	) (*types.Entry, error)
	// CreateEntryFromURL creates entry from a URL.
	// When fileName or fileType is provided (or the URL path has a known file extension),
	// the URL is treated as a direct file download instead of a web page crawl.
	// channel identifies the ingestion channel; empty defaults to "web".
	CreateEntryFromURL(
		ctx context.Context,
		kbID string,
		url string,
		fileName string,
		fileType string,
		enableMultimodel *bool,
		title string,
		channel string,
		processOverrides *types.EntryProcessOverrides,
		sourceInfo ...types.SourceInfo,
	) (*types.Entry, error)
	// CreateEntryFromPassage creates entry from text passages.
	// channel identifies the ingestion channel; empty defaults to "web".
	CreateEntryFromPassage(ctx context.Context, kbID string, passage []string, channel string) (*types.Entry, error)
	// CreateEntryFromPassageSync creates entry from text passages and waits until chunks are indexed.
	CreateEntryFromPassageSync(ctx context.Context, kbID string, passage []string, channel string) (*types.Entry, error)
	// CreateEntryFromManual creates or saves manual Markdown entry content.
	// channel identifies the ingestion channel; empty defaults to "web".
	CreateEntryFromManual(
		ctx context.Context,
		kbID string,
		payload *types.ManualEntryPayload,
		channel string,
	) (*types.Entry, error)
	// GetEntryByID retrieves entry by ID (uses space from context).
	GetEntryByID(ctx context.Context, id string) (*types.Entry, error)
	// GetEntryByIDOnly retrieves entry by ID without space filter (for permission resolution).
	GetEntryByIDOnly(ctx context.Context, id string) (*types.Entry, error)

	// GetEntryBatch retrieves a batch of entry by IDs.
	GetEntryBatch(ctx context.Context, spaceID uint64, ids []string) ([]*types.Entry, error)
	// ListEntryByKnowledgeBaseID lists all entry under a knowledge base.
	ListEntryByKnowledgeBaseID(ctx context.Context, kbID string) ([]*types.Entry, error)
	// ListPagedEntryByKnowledgeBaseID lists all entry under a knowledge base
	// with pagination and optional keyword, file type, parse status, source channel
	// and updated time filters. Pass a zero
	// struct to disable all filters.
	ListPagedEntryByKnowledgeBaseID(
		ctx context.Context,
		kbID string,
		page *types.Pagination,
		filter types.EntryListFilter,
	) (*types.PageResult, error)
	// DeleteEntry deletes entry by ID.
	DeleteEntry(ctx context.Context, id string) error
	// DeleteEntryList deletes multiple entry entries by IDs.
	DeleteEntryList(ctx context.Context, ids []string) error
	// GetEntryFile retrieves the file associated with the entry.
	GetEntryFile(ctx context.Context, id string) (io.ReadCloser, string, error)
	// GetEntryBody retrieves the immutable parsed text.
	GetEntryBody(ctx context.Context, id string) (io.ReadCloser, string, error)
	ReviewEntryIntake(ctx context.Context, id, action string, sourceInfo types.SourceInfo) (*types.Entry, error)
	// UpdateEntry updates entry information.
	UpdateEntry(ctx context.Context, entry *types.Entry) error
	// UpdateManualEntry updates manual Markdown entry content.
	UpdateManualEntry(
		ctx context.Context,
		entryID string,
		payload *types.ManualEntryPayload,
	) (*types.Entry, error)
	// ReparseEntry deletes existing document content and re-parses the entry asynchronously.
	// When processOverrides is non-nil, it is validated and persisted to the entry metadata
	// before re-parsing, letting callers adjust parse config on reparse; nil keeps stored overrides.
	ReparseEntry(
		ctx context.Context,
		entryID string,
		processOverrides *types.EntryProcessOverrides,
	) (*types.Entry, error)
	// CancelEntryParse marks an in-progress parse as cancelled by the
	// user. The entry row and any partially written chunks/index are
	// kept; downstream queued tasks for the same entry are best-effort
	// dequeued and active workers are signalled to stop at their next
	// checkpoint. Idempotent — returns the existing row when the entry
	// is already cancelled. Returns an error when the entry is in a
	// terminal state (completed / failed) or being deleted.
	CancelEntryParse(ctx context.Context, entryID string) (*types.Entry, error)
	// GetRepository gets the entry repository
	GetRepository() EntryRepository
	// ProcessManualUpdate handles background manual entry update tasks (cleanup + re-indexing)
	ProcessManualUpdate(ctx context.Context, t *taskqueue.Task) error
	// ProcessDocument handles background document processing tasks
	ProcessDocument(ctx context.Context, t *taskqueue.Task) error
	// ProcessEntryListDelete handles background entry list delete tasks
	ProcessEntryListDelete(ctx context.Context, t *taskqueue.Task) error
	// ProcessEntryListReparse handles background entry list reparse tasks
	ProcessEntryListReparse(ctx context.Context, t *taskqueue.Task) error
	// SearchEntry searches entry items by keyword across the space.
	// fileTypes: optional list of file extensions to filter by (e.g., ["csv", "xlsx"])
	SearchEntry(ctx context.Context, keyword string, offset, limit int, fileTypes []string) ([]*types.Entry, bool, int64, error)
}

// EntryRepository defines the interface for entry repositories.
type EntryRepository interface {
	GetEntryBatch(ctx context.Context, spaceID uint64, ids []string) ([]*types.Entry, error)
	CreateEntry(ctx context.Context, entry *types.Entry) error
	GetEntryByID(ctx context.Context, spaceID uint64, id string) (*types.Entry, error)
	// GetEntryByIDOnly returns entry by ID without space filter (for permission resolution).
	GetEntryByIDOnly(ctx context.Context, id string) (*types.Entry, error)
	ListEntryByKnowledgeBaseID(ctx context.Context, spaceID uint64, kbID string) ([]*types.Entry, error)
	// ListPagedEntryByKnowledgeBaseID lists all entry in a knowledge base
	// with pagination and optional keyword, file type, parse status, source channel
	// and updated time filters. Pass a zero
	// struct to disable all filters.
	ListPagedEntryByKnowledgeBaseID(ctx context.Context,
		spaceID uint64, kbID string, page *types.Pagination, filter types.EntryListFilter,
	) ([]*types.Entry, int64, error)
	UpdateEntry(ctx context.Context, entry *types.Entry) error
	// UpdateEntryBatch updates entry items in batch
	UpdateEntryBatch(ctx context.Context, entryList []*types.Entry) error
	DeleteEntry(ctx context.Context, spaceID uint64, id string) error
	DeleteEntryList(ctx context.Context, spaceID uint64, ids []string) error
	// CheckEntryExists checks if entry already exists.
	// For file types, check by fileHash or (fileName+fileSize).
	// For URL types, check by URL.
	// Returns whether it exists, the existing entry object (if any), and possible error.
	CheckEntryExists(
		ctx context.Context,
		spaceID uint64,
		kbID string,
		params *types.EntryCheckParams,
	) (bool, *types.Entry, error)
	UpdateEntryColumn(ctx context.Context, id string, column string, value interface{}) error
	// UpdateEntryColumns updates multiple columns of a entry row in a single
	// statement so callers that flip several related fields (e.g. parse_status +
	// error_message) cannot leave the row in a half-updated state.
	UpdateEntryColumns(ctx context.Context, id string, values map[string]interface{}) error
	// UpdateActiveDeletingEntryColumns updates an active, non-deleted entry row
	// only when it is still in the transient deleting state.
	UpdateActiveDeletingEntryColumns(ctx context.Context, id string, values map[string]interface{}) (bool, error)
	// FinalizeSubtask atomically decrements pending_subtasks_count for the
	// given entry and promotes parse_status from "finalizing" to
	// "completed" when the count reaches zero. Returns the post-decrement
	// count, whether this caller's UPDATE was the one that promoted the
	// row, and any error.
	FinalizeSubtask(ctx context.Context, id string) (int, bool, error)
	// SetFinalizing atomically transitions a row from "processing" to
	// "finalizing" and writes the initial pending_subtasks_count. Returns
	// whether the transition took place (false when the row's parse_status
	// was no longer "processing", e.g. user cancelled / deleted in flight).
	SetFinalizing(ctx context.Context, id string, expectedSubtasks int) (bool, error)
	// CountEntryByKnowledgeBaseID counts the number of entry items in a knowledge base.
	CountEntryByKnowledgeBaseID(ctx context.Context, spaceID uint64, kbID string) (int64, error)
	// CountEntryByStatus counts the number of entry items with the specified parse status.
	CountEntryByStatus(ctx context.Context, spaceID uint64, kbID string, parseStatuses []string) (int64, error)
	// SearchEntry searches entry items by keyword across the space.
	// fileTypes: optional list of file extensions to filter by (e.g., ["csv", "xlsx"])
	SearchEntry(ctx context.Context, spaceID uint64, keyword string, offset, limit int, fileTypes []string) ([]*types.Entry, bool, error)

	// FindByMetadataKey finds a entry item by a key-value pair in the metadata JSON column.
	// Used by data source sync to locate existing items by external_id.
	FindByMetadataKey(ctx context.Context, spaceID uint64, kbID string, key string, value string) (*types.Entry, error)
	// FindByMetadataKeyPrefix finds entry items whose metadata[key] starts
	// with the given prefix. Used to sweep an external node's attachment sub-items.
	FindByMetadataKeyPrefix(ctx context.Context, spaceID uint64, kbID string, key string, prefix string) ([]*types.Entry, error)
	// FindByDataSourceExternalID finds a entry item owned by one data source
	// and identified by the source's external item ID.
	FindByDataSourceExternalID(
		ctx context.Context, spaceID uint64, kbID, dataSourceID, externalID string,
	) (*types.Entry, error)
	// HardDeleteEntry physically removes a row after DeleteEntry's soft-delete
	// cascade. Sync-internal deletions use this so rows never become tombstones.
	HardDeleteEntry(ctx context.Context, spaceID uint64, id string) error
	// HardDeleteEntryList is the batch counterpart of HardDeleteEntry.
	HardDeleteEntryList(ctx context.Context, spaceID uint64, ids []string) error
	// SearchEntryInScopes searches articles inside explicit space and knowledge base boundaries.
	SearchEntryInScopes(ctx context.Context, scopes []types.SearchScope, keyword string, offset, limit int, fileTypes []string) ([]*types.Entry, bool, int64, error)
}

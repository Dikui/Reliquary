package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"strings"

	"gorm.io/gorm"

	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	werrors "reliquary/internal/errors"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// Error definitions for entry service operations
var (
	// ErrInvalidFileType is returned when an unsupported file type is provided
	ErrInvalidFileType = errors.New("unsupported file type")
	// ErrChunkNotFound is returned when a requested chunk cannot be found.
	// Aliases the repository sentinel so a chunk-not-found from the repo
	// errors.Is-matches at the service and middleware layers (a single
	// identity instead of two string-equal-but-distinct errors).
	ErrChunkNotFound = repository.ErrChunkNotFound
)

// entryService implements the entry service interface
// service 实现知识服务接口
type entryService struct {
	db              *gorm.DB
	config          *config.Config
	retrieveEngine  interfaces.RetrievalService
	repo            interfaces.EntryRepository
	kbService       interfaces.KnowledgeBaseService
	spaceRepo       interfaces.SpaceRepository
	spaceService    interfaces.SpaceService
	chunkService    interfaces.ChunkService
	chunkRepo       interfaces.ChunkRepository
	fileSvc         interfaces.FileService
	resourceCatalog interfaces.ResourceCatalog
	modelService    interfaces.ModelService
	task            interfaces.TaskEnqueuer
	taskInspector   interfaces.TaskInspector
	imageResolver   *docparser.ImageResolver
	ingestDocReader interfaces.DocReader

	// spanTracker records the per-attempt span tree for the parsing
	// pipeline. Best-effort: a nil tracker (test harness) is safely
	// handled because the public surface is the SpanTracker interface,
	// which has a no-op fallback. See entry_span_tracker.go.
	spanTracker SpanTracker
	vault       *VaultService
}

// SetVault attaches the local article library without expanding the public
// ingestion interface or requiring unrelated callers to implement new methods.
func (s *entryService) SetVault(v *VaultService) { s.vault = v }

const (
	manualContentMaxLength = 200000
	manualFileExtension    = ".md"
)

// NewEntryService creates a new entry service instance
func NewEntryService(
	db *gorm.DB,
	config *config.Config,
	repo interfaces.EntryRepository,
	kbService interfaces.KnowledgeBaseService,
	spaceRepo interfaces.SpaceRepository,
	spaceService interfaces.SpaceService,
	chunkService interfaces.ChunkService,
	chunkRepo interfaces.ChunkRepository,
	fileSvc interfaces.FileService,
	resourceCatalog interfaces.ResourceCatalog,
	modelService interfaces.ModelService,
	task interfaces.TaskEnqueuer,
	taskInspector interfaces.TaskInspector,
	retrieveEngine interfaces.RetrievalService,
	imageResolver *docparser.ImageResolver,
	spanTracker SpanTracker,

) (interfaces.EntryService, error) {
	return &entryService{
		db:              db,
		config:          config,
		repo:            repo,
		kbService:       kbService,
		spaceRepo:       spaceRepo,
		spaceService:    spaceService,
		chunkService:    chunkService,
		chunkRepo:       chunkRepo,
		fileSvc:         fileSvc,
		resourceCatalog: resourceCatalog,
		modelService:    modelService,
		task:            task,
		taskInspector:   taskInspector,
		retrieveEngine:  retrieveEngine,
		imageResolver:   imageResolver,
		spanTracker:     spanTracker,
	}, nil
}

// tracker returns a usable SpanTracker — falls back to a no-op when the
// service was constructed without one (test harness, in-memory fixtures w/o repo).
// All pipeline call sites go through this so they never need a nil check.
func (s *entryService) tracker() SpanTracker {
	if s.spanTracker == nil {
		return noopSpanTracker{}
	}
	return s.spanTracker
}

// attemptCtxKey scopes the per-task attempt number to a single execution.
// Set once at the start of ProcessDocument / ProcessManualUpdate so
// every nested tracker call within the same task
// can locate the right attempt without threading it through signatures.
type attemptCtxKey struct{}

// withAttempt returns a child ctx tagged with the given attempt number.
// Pass through every call site that may invoke the tracker.
func withAttempt(ctx context.Context, attempt int) context.Context {
	if attempt <= 0 {
		return ctx
	}
	return context.WithValue(ctx, attemptCtxKey{}, attempt)
}

// attemptFromCtx extracts the attempt number stored by withAttempt;
// returns 0 when missing (legacy paths or tests). Tracker call sites
// treat 0 as "skip recording" since we have no attempt to anchor under.
func attemptFromCtx(ctx context.Context) int {
	if v, ok := ctx.Value(attemptCtxKey{}).(int); ok {
		return v
	}
	return 0
}

func (s *entryService) beginStage(ctx context.Context, kid, name string, input types.JSONMap) {
	a := attemptFromCtx(ctx)
	if a <= 0 {
		return
	}
	s.tracker().BeginStage(ctx, kid, a, name, input)
}

func (s *entryService) endStage(ctx context.Context, kid, name string, output types.JSONMap) {
	a := attemptFromCtx(ctx)
	if a <= 0 {
		return
	}
	span := s.tracker().LookupStage(ctx, kid, a, name)
	if span == nil {
		return
	}
	s.tracker().EndSpan(ctx, span, output)
}

func (s *entryService) failStage(ctx context.Context, kid, name, code, msg string, err error) {
	a := attemptFromCtx(ctx)
	if a <= 0 {
		return
	}
	span := s.tracker().LookupStage(ctx, kid, a, name)
	if span == nil {
		return
	}
	s.tracker().FailSpan(ctx, span, code, msg, err)
}

func (s *entryService) skipStage(ctx context.Context, kid, name, reason string) {
	a := attemptFromCtx(ctx)
	if a <= 0 {
		return
	}
	span := s.tracker().LookupStage(ctx, kid, a, name)
	if span == nil {
		// No begin recorded — synthesize a span row for skipped state.
		// Use BeginStage with no input then SkipSpan to keep schema
		// invariants (started_at / kind set).
		span = s.tracker().BeginStage(ctx, kid, a, name, nil)
	}
	s.tracker().SkipSpan(ctx, span, reason)
}

func (s *entryService) getParserEngineOverridesFromContext(ctx context.Context) map[string]string {
	if v := ctx.Value(types.SpaceInfoContextKey); v != nil {
		if space, ok := v.(*types.Space); ok && space != nil {
			return space.ParserEngineConfig.ToOverridesMap()
		}
	}
	return nil
}

// GetRepository gets the entry repository
// Parameters:
//   - ctx: Context with authentication and request information
//
// Returns:
//   - interfaces.EntryRepository: Entry repository
func (s *entryService) GetRepository() interfaces.EntryRepository {
	return s.repo
}

// isEntryDeleting checks if a entry entry is being deleted.
// This is used to prevent async tasks from conflicting with deletion operations.
func (s *entryService) isEntryDeleting(ctx context.Context, spaceID uint64, entryID string) bool {
	entry, err := s.repo.GetEntryByID(ctx, spaceID, entryID)
	if err != nil {
		// If we can't find the entry, assume it's deleted
		logger.Warnf(ctx, "Failed to check entry deletion status (assuming deleted): %v", err)
		return true
	}
	if entry == nil {
		return true
	}
	return entry.ParseStatus == types.ParseStatusDeleting
}

// isEntryAborted returns (true, status) when the entry has been
// marked as deleting OR cancelled so async pipeline workers should bail
// out. Status is returned so callers can branch on cleanup behavior:
// deleting → existing cleanup of partial chunks/index applies;
// cancelled → keep partially written data per user expectation.
//
// When the row is missing or unreadable we conservatively return
// (true, ParseStatusDeleting): the existing deleting branch already
// handles cleanup-or-no-op semantics safely.
func (s *entryService) isEntryAborted(
	ctx context.Context, spaceID uint64, entryID string,
) (bool, string) {
	entry, err := s.repo.GetEntryByID(ctx, spaceID, entryID)
	if err != nil {
		logger.Warnf(ctx, "Failed to check entry abort status (assuming deleted): %v", err)
		return true, types.ParseStatusDeleting
	}
	if entry == nil {
		return true, types.ParseStatusDeleting
	}
	switch entry.ParseStatus {
	case types.ParseStatusDeleting, types.ParseStatusCancelled:
		return true, entry.ParseStatus
	}
	return false, entry.ParseStatus
}

func kbIDOrEmpty(kb *types.KnowledgeBase) string {
	if kb == nil {
		return ""
	}
	return kb.ID
}

// GetEntryByID retrieves a entry entry by its ID
func (s *entryService) GetEntryByID(ctx context.Context, id string) (*types.Entry, error) {
	spaceID := ctx.Value(types.SpaceIDContextKey).(uint64)

	entry, err := s.repo.GetEntryByID(ctx, spaceID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"entry_id": id,
			"space_id": spaceID,
		})
		return nil, err
	}

	reposts, err := loadEntryRepostSources(s.db, spaceID, []string{entry.ID}, []string{entry.KnowledgeBaseID})
	if err != nil {
		return nil, err
	}
	entry.RepostSources = reposts
	if err := s.loadEntryAssociations(ctx, spaceID, []*types.Entry{entry}); err != nil {
		return nil, err
	}

	logger.Infof(ctx, "Entry retrieved successfully, ID: %s, type: %s", entry.ID, entry.Type)
	return entry, nil
}

// GetEntryByIDOnly retrieves entry by ID without space filter (for permission resolution).
func (s *entryService) GetEntryByIDOnly(ctx context.Context, id string) (*types.Entry, error) {
	return s.repo.GetEntryByIDOnly(ctx, id)
}

// ListEntryByKnowledgeBaseID returns all entry entries in a knowledge base
func (s *entryService) ListEntryByKnowledgeBaseID(ctx context.Context,
	kbID string,
) ([]*types.Entry, error) {
	return s.repo.ListEntryByKnowledgeBaseID(ctx, ctx.Value(types.SpaceIDContextKey).(uint64), kbID)
}

// ListPagedEntryByKnowledgeBaseID returns paginated entry entries in a knowledge base
func (s *entryService) ListPagedEntryByKnowledgeBaseID(ctx context.Context,
	kbID string, page *types.Pagination, filter types.EntryListFilter,
) (*types.PageResult, error) {
	entries, total, err := s.repo.ListPagedEntryByKnowledgeBaseID(ctx,
		ctx.Value(types.SpaceIDContextKey).(uint64), kbID, page, filter)
	if err != nil {
		return nil, err
	}

	if err := s.loadEntryAssociations(ctx, types.MustSpaceIDFromContext(ctx), entries); err != nil {
		return nil, err
	}
	return types.NewPageResult(total, page, entries), nil
}

// GetEntryFile retrieves the physical file associated with a entry entry
func (s *entryService) GetEntryFile(ctx context.Context, id string) (io.ReadCloser, string, error) {
	// Get entry record
	spaceID := ctx.Value(types.SpaceIDContextKey).(uint64)
	entry, err := s.repo.GetEntryByID(ctx, spaceID, id)
	if err != nil {
		return nil, "", err
	}

	if s.vault != nil && entry.VaultOriginalPath != "" {
		file, err := s.vault.ReadOriginal(entry)
		return file, entry.FileName, err
	}

	// Uploaded source bytes take precedence over the immutable parsed body.
	if entry.FilePath != "" {
		kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, entry.KnowledgeBaseID)
		file, err := s.resolveFileServiceForPath(ctx, kb, entry.FilePath).GetFile(ctx, entry.FilePath)
		return file, entry.FileName, err
	}
	return s.getEntryBody(ctx, entry)
}

// GetEntryBody reads the immutable parsed text rather than uploaded bytes.
func (s *entryService) GetEntryBody(ctx context.Context, id string) (io.ReadCloser, string, error) {
	spaceID := ctx.Value(types.SpaceIDContextKey).(uint64)
	entry, err := s.repo.GetEntryByID(ctx, spaceID, id)
	if err != nil {
		return nil, "", err
	}
	return s.getEntryBody(ctx, entry)
}

func (s *entryService) getEntryBody(ctx context.Context, entry *types.Entry) (io.ReadCloser, string, error) {

	if entry.ProcessingMode == "summary_only" {
		return nil, "", errors.New("article stores a summary and original link; parsed body has been removed")
	}
	if s.vault != nil && entry.VaultPath != "" {
		body, err := s.vault.ReadBody(entry)
		if err != nil {
			return nil, "", err
		}
		return io.NopCloser(strings.NewReader(string(body))), sanitizeManualDownloadFilename(entry.Title), nil
	}
	var parsed struct {
		Path string `json:"processed_body_path"`
	}
	if err := json.Unmarshal(entry.Metadata, &parsed); len(entry.Metadata) > 0 && err != nil {
		return nil, "", err
	}
	if parsed.Path != "" {
		kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, entry.KnowledgeBaseID)
		file, err := s.resolveFileServiceForPath(ctx, kb, parsed.Path).GetFile(ctx, parsed.Path)
		return file, sanitizeManualDownloadFilename(entry.Title), err
	}

	// Intake originals are stored independently of the uploaded binary and DB row.
	if intake, intakeErr := entry.IntakeMetadata(); intakeErr != nil {
		return nil, "", intakeErr
	} else if intake != nil {
		if intake.OriginalPath == "" {
			return nil, "", fmt.Errorf("original is not available")
		}
		kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, entry.KnowledgeBaseID)
		file, getErr := s.resolveFileServiceForPath(ctx, kb, intake.OriginalPath).GetFile(ctx, intake.OriginalPath)
		return file, sanitizeManualDownloadFilename(entry.Title), getErr
	}
	// Legacy manual records still keep their body in metadata.
	if entry.IsManual() {
		meta, err := entry.ManualMetadata()
		if err != nil {
			return nil, "", err
		}
		// ManualMetadata returns (nil, nil) when Metadata column is empty; treat as empty content.
		content := ""
		if meta != nil {
			content = meta.Content
		}
		filename := sanitizeManualDownloadFilename(entry.Title)
		return io.NopCloser(strings.NewReader(content)), filename, nil
	}

	// Resolve KB-level file service with FilePath fallback protection
	kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, entry.KnowledgeBaseID)
	file, err := s.resolveFileServiceForPath(ctx, kb, entry.FilePath).GetFile(ctx, entry.FilePath)
	if err != nil {
		return nil, "", err
	}

	return file, entry.FileName, nil
}

func (s *entryService) UpdateEntry(ctx context.Context, entry *types.Entry) error {
	record, err := s.repo.GetEntryByID(ctx, ctx.Value(types.SpaceIDContextKey).(uint64), entry.ID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get entry record: %v", err)
		return err
	}
	previousTitle := record.Title
	if record.SourceKind == "mine" {
		return fmt.Errorf("本人文档不可修改，请删除后重新导入")
	}
	// if need other fields update, please add here
	if entry.Title != "" {
		record.Title = entry.Title
	}
	if entry.DescriptionSpecified {
		record.Description = entry.Description
		if record.Description != "" {
			record.SummaryStatus = types.SummaryStatusCompleted
		} else {
			record.SummaryStatus = types.SummaryStatusNone
		}
	} else if entry.Description != "" {
		record.Description = entry.Description
	}
	if entry.CustomMetadata != nil {
		var custom map[string]interface{}
		if err := json.Unmarshal(entry.CustomMetadata, &custom); err != nil {
			return fmt.Errorf("custom_metadata must be a JSON object: %w", err)
		}
		if len(custom) > 20 {
			return fmt.Errorf("custom_metadata supports at most 20 fields")
		}
		for key, value := range custom {
			if len(strings.TrimSpace(key)) == 0 || len(key) > 64 || len(fmt.Sprint(value)) > 1000 {
				return fmt.Errorf("invalid custom_metadata field %q", key)
			}
			switch value.(type) {
			case string, float64, bool, nil:
			default:
				return fmt.Errorf("custom_metadata field %q must be a string, number, boolean, or null", key)
			}
		}
		existing := make(map[string]interface{})
		if len(record.CustomMetadata) > 0 {
			_ = json.Unmarshal(record.CustomMetadata, &existing)
		}
		record.CustomMetadata = entry.CustomMetadata
	}

	if s.db != nil && record.Title != previousTitle && record.SourceKind != "mine" && record.SourceKind != "report" &&
		record.StructuredSummary != nil && record.ParseStatus == types.ParseStatusCompleted {
		kb, err := s.kbService.GetKnowledgeBaseByID(ctx, record.KnowledgeBaseID)
		if err != nil {
			return err
		}
		// Keep a title change and its vector atomic when the provider fails.
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := repository.NewEntryRepository(tx).UpdateEntry(ctx, record); err != nil {
				return err
			}
			return indexArticleEmbedding(ctx, tx, s.modelService, kb, record)
		}); err != nil {
			return err
		}
	} else if err := s.repo.UpdateEntry(ctx, record); err != nil {
		logger.Errorf(ctx, "Failed to update entry: %v", err)
		return err
	}
	if s.vault != nil && record.SourceKind == "article" && record.VaultPath != "" {
		s.vault.ScheduleRefresh(ctx, record.ID)
	}
	logger.Infof(ctx, "Entry updated successfully, ID: %s", entry.ID)
	return nil
}

// GetEntryBatch retrieves multiple entry entries by their IDs
func (s *entryService) GetEntryBatch(ctx context.Context,
	spaceID uint64, ids []string,
) ([]*types.Entry, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	articles, err := s.repo.GetEntryBatch(ctx, spaceID, ids)
	if err != nil {
		return nil, err
	}
	if err := s.loadEntryAssociations(ctx, spaceID, articles); err != nil {
		return nil, err
	}
	return articles, nil
}

func findEntryByID(list []*types.Entry, id string) *types.Entry {
	for _, entry := range list {
		if entry.ID == id {
			return entry
		}
	}
	return nil
}

func (s *entryService) SearchEntry(ctx context.Context, keyword string, offset, limit int, fileTypes []string) ([]*types.Entry, bool, int64, error) {
	spaceID, ok := ctx.Value(types.SpaceIDContextKey).(uint64)
	if !ok {
		return nil, false, 0, werrors.NewUnauthorizedError("Workspace ID not found in context")
	}

	kb, err := s.kbService.GetKnowledgeBase(ctx)
	if err != nil {
		return nil, false, 0, err
	}
	return s.repo.SearchEntryInScopes(ctx, []types.SearchScope{{SpaceID: spaceID, KBID: kb.ID}}, keyword, offset, limit, fileTypes)
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	redis "github.com/redis/go-redis/v9"
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

// Error definitions for knowledge service operations
var (
	// ErrInvalidFileType is returned when an unsupported file type is provided
	ErrInvalidFileType = errors.New("unsupported file type")
	// ErrInvalidURL is returned when an invalid URL is provided
	ErrInvalidURL = errors.New("invalid URL")
	// ErrChunkNotFound is returned when a requested chunk cannot be found.
	// Aliases the repository sentinel so a chunk-not-found from the repo
	// errors.Is-matches at the service and middleware layers (a single
	// identity instead of two string-equal-but-distinct errors).
	ErrChunkNotFound = repository.ErrChunkNotFound
	// ErrDuplicateFile is returned when trying to add a file that already exists
	ErrDuplicateFile = errors.New("file already exists")
	// ErrDuplicateURL is returned when trying to add a URL that already exists
	ErrDuplicateURL = errors.New("URL already exists")
)

// knowledgeService implements the knowledge service interface
// service 实现知识服务接口
type knowledgeService struct {
	db              *gorm.DB
	config          *config.Config
	retrieveEngine  interfaces.RetrievalService
	repo            interfaces.KnowledgeRepository
	kbService       interfaces.KnowledgeBaseService
	tenantRepo      interfaces.TenantRepository
	tenantService   interfaces.TenantService
	chunkService    interfaces.ChunkService
	chunkRepo       interfaces.ChunkRepository
	fileSvc         interfaces.FileService
	resourceCatalog interfaces.ResourceCatalog
	modelService    interfaces.ModelService
	task            interfaces.TaskEnqueuer
	taskInspector   interfaces.TaskInspector
	redisClient     *redis.Client
	imageResolver   *docparser.ImageResolver

	// spanTracker records the per-attempt span tree for the parsing
	// pipeline. Best-effort: a nil tracker (test harness) is safely
	// handled because the public surface is the SpanTracker interface,
	// which has a no-op fallback. See knowledge_span_tracker.go.
	spanTracker SpanTracker
	vault       *VaultService
}

// SetVault attaches the local article library without expanding the public
// ingestion interface or requiring unrelated callers to implement new methods.
func (s *knowledgeService) SetVault(v *VaultService) { s.vault = v }

const (
	manualContentMaxLength = 200000
	manualFileExtension    = ".md"
)

// NewKnowledgeService creates a new knowledge service instance
func NewKnowledgeService(
	db *gorm.DB,
	config *config.Config,
	repo interfaces.KnowledgeRepository,
	kbService interfaces.KnowledgeBaseService,
	tenantRepo interfaces.TenantRepository,
	tenantService interfaces.TenantService,
	chunkService interfaces.ChunkService,
	chunkRepo interfaces.ChunkRepository,
	fileSvc interfaces.FileService,
	resourceCatalog interfaces.ResourceCatalog,
	modelService interfaces.ModelService,
	task interfaces.TaskEnqueuer,
	taskInspector interfaces.TaskInspector,
	retrieveEngine interfaces.RetrievalService,
	redisClient *redis.Client,
	imageResolver *docparser.ImageResolver,
	spanTracker SpanTracker,

) (interfaces.KnowledgeService, error) {
	return &knowledgeService{
		db:              db,
		config:          config,
		repo:            repo,
		kbService:       kbService,
		tenantRepo:      tenantRepo,
		tenantService:   tenantService,
		chunkService:    chunkService,
		chunkRepo:       chunkRepo,
		fileSvc:         fileSvc,
		resourceCatalog: resourceCatalog,
		modelService:    modelService,
		task:            task,
		taskInspector:   taskInspector,
		retrieveEngine:  retrieveEngine,
		redisClient:     redisClient,
		imageResolver:   imageResolver,
		spanTracker:     spanTracker,
	}, nil
}

// tracker returns a usable SpanTracker — falls back to a no-op when the
// service was constructed without one (test harness, in-memory fixtures w/o repo).
// All pipeline call sites go through this so they never need a nil check.
func (s *knowledgeService) tracker() SpanTracker {
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

func (s *knowledgeService) beginStage(ctx context.Context, kid, name string, input types.JSONMap) {
	a := attemptFromCtx(ctx)
	if a <= 0 {
		return
	}
	s.tracker().BeginStage(ctx, kid, a, name, input)
}

func (s *knowledgeService) endStage(ctx context.Context, kid, name string, output types.JSONMap) {
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

func (s *knowledgeService) failStage(ctx context.Context, kid, name, code, msg string, err error) {
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

func (s *knowledgeService) skipStage(ctx context.Context, kid, name, reason string) {
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

func (s *knowledgeService) getParserEngineOverridesFromContext(ctx context.Context) map[string]string {
	if v := ctx.Value(types.TenantInfoContextKey); v != nil {
		if tenant, ok := v.(*types.Tenant); ok && tenant != nil {
			return tenant.ParserEngineConfig.ToOverridesMap()
		}
	}
	return nil
}

// GetRepository gets the knowledge repository
// Parameters:
//   - ctx: Context with authentication and request information
//
// Returns:
//   - interfaces.KnowledgeRepository: Knowledge repository
func (s *knowledgeService) GetRepository() interfaces.KnowledgeRepository {
	return s.repo
}

// isKnowledgeDeleting checks if a knowledge entry is being deleted.
// This is used to prevent async tasks from conflicting with deletion operations.
func (s *knowledgeService) isKnowledgeDeleting(ctx context.Context, tenantID uint64, knowledgeID string) bool {
	knowledge, err := s.repo.GetKnowledgeByID(ctx, tenantID, knowledgeID)
	if err != nil {
		// If we can't find the knowledge, assume it's deleted
		logger.Warnf(ctx, "Failed to check knowledge deletion status (assuming deleted): %v", err)
		return true
	}
	if knowledge == nil {
		return true
	}
	return knowledge.ParseStatus == types.ParseStatusDeleting
}

// isKnowledgeAborted returns (true, status) when the knowledge has been
// marked as deleting OR cancelled so async pipeline workers should bail
// out. Status is returned so callers can branch on cleanup behavior:
// deleting → existing cleanup of partial chunks/index applies;
// cancelled → keep partially written data per user expectation.
//
// When the row is missing or unreadable we conservatively return
// (true, ParseStatusDeleting): the existing deleting branch already
// handles cleanup-or-no-op semantics safely.
func (s *knowledgeService) isKnowledgeAborted(
	ctx context.Context, tenantID uint64, knowledgeID string,
) (bool, string) {
	knowledge, err := s.repo.GetKnowledgeByID(ctx, tenantID, knowledgeID)
	if err != nil {
		logger.Warnf(ctx, "Failed to check knowledge abort status (assuming deleted): %v", err)
		return true, types.ParseStatusDeleting
	}
	if knowledge == nil {
		return true, types.ParseStatusDeleting
	}
	switch knowledge.ParseStatus {
	case types.ParseStatusDeleting, types.ParseStatusCancelled:
		return true, knowledge.ParseStatus
	}
	return false, knowledge.ParseStatus
}

func kbIDOrEmpty(kb *types.KnowledgeBase) string {
	if kb == nil {
		return ""
	}
	return kb.ID
}

func defaultChannel(ch string) string {
	if ch == "" {
		return types.ChannelWeb
	}
	return ch
}

// GetKnowledgeByID retrieves a knowledge entry by its ID
func (s *knowledgeService) GetKnowledgeByID(ctx context.Context, id string) (*types.Knowledge, error) {
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

	knowledge, err := s.repo.GetKnowledgeByID(ctx, tenantID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_id": id,
			"tenant_id":    tenantID,
		})
		return nil, err
	}

	reposts, err := loadKnowledgeRepostSources(s.db, tenantID, []string{knowledge.ID}, []string{knowledge.KnowledgeBaseID})
	if err != nil {
		return nil, err
	}
	knowledge.RepostSources = reposts
	if err := s.loadKnowledgeAssociations(ctx, tenantID, []*types.Knowledge{knowledge}); err != nil {
		return nil, err
	}

	logger.Infof(ctx, "Knowledge retrieved successfully, ID: %s, type: %s", knowledge.ID, knowledge.Type)
	return knowledge, nil
}

// GetKnowledgeByIDOnly retrieves knowledge by ID without tenant filter (for permission resolution).
func (s *knowledgeService) GetKnowledgeByIDOnly(ctx context.Context, id string) (*types.Knowledge, error) {
	return s.repo.GetKnowledgeByIDOnly(ctx, id)
}

// ListKnowledgeByKnowledgeBaseID returns all knowledge entries in a knowledge base
func (s *knowledgeService) ListKnowledgeByKnowledgeBaseID(ctx context.Context,
	kbID string,
) ([]*types.Knowledge, error) {
	return s.repo.ListKnowledgeByKnowledgeBaseID(ctx, ctx.Value(types.TenantIDContextKey).(uint64), kbID)
}

// ListPagedKnowledgeByKnowledgeBaseID returns paginated knowledge entries in a knowledge base
func (s *knowledgeService) ListPagedKnowledgeByKnowledgeBaseID(ctx context.Context,
	kbID string, page *types.Pagination, filter types.KnowledgeListFilter,
) (*types.PageResult, error) {
	knowledges, total, err := s.repo.ListPagedKnowledgeByKnowledgeBaseID(ctx,
		ctx.Value(types.TenantIDContextKey).(uint64), kbID, page, filter)
	if err != nil {
		return nil, err
	}

	if err := s.loadKnowledgeAssociations(ctx, types.MustTenantIDFromContext(ctx), knowledges); err != nil {
		return nil, err
	}
	return types.NewPageResult(total, page, knowledges), nil
}

// GetKnowledgeFile retrieves the physical file associated with a knowledge entry
func (s *knowledgeService) GetKnowledgeFile(ctx context.Context, id string) (io.ReadCloser, string, error) {
	// Get knowledge record
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	knowledge, err := s.repo.GetKnowledgeByID(ctx, tenantID, id)
	if err != nil {
		return nil, "", err
	}

	if s.vault != nil && knowledge.VaultOriginalPath != "" {
		file, err := s.vault.ReadOriginal(knowledge)
		return file, knowledge.FileName, err
	}

	// Uploaded source bytes take precedence over the immutable parsed body.
	if knowledge.FilePath != "" {
		kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
		file, err := s.resolveFileServiceForPath(ctx, kb, knowledge.FilePath).GetFile(ctx, knowledge.FilePath)
		return file, knowledge.FileName, err
	}
	return s.getKnowledgeBody(ctx, knowledge)
}

// GetKnowledgeBody reads the immutable parsed text rather than uploaded bytes.
func (s *knowledgeService) GetKnowledgeBody(ctx context.Context, id string) (io.ReadCloser, string, error) {
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	knowledge, err := s.repo.GetKnowledgeByID(ctx, tenantID, id)
	if err != nil {
		return nil, "", err
	}
	return s.getKnowledgeBody(ctx, knowledge)
}

func (s *knowledgeService) getKnowledgeBody(ctx context.Context, knowledge *types.Knowledge) (io.ReadCloser, string, error) {

	if knowledge.ProcessingMode == "summary_only" {
		return nil, "", errors.New("article stores a summary and original link; parsed body has been removed")
	}
	if s.vault != nil && knowledge.VaultPath != "" {
		body, err := s.vault.ReadBody(knowledge)
		if err != nil {
			return nil, "", err
		}
		return io.NopCloser(strings.NewReader(string(body))), sanitizeManualDownloadFilename(knowledge.Title), nil
	}
	var parsed struct {
		Path string `json:"processed_body_path"`
	}
	if err := json.Unmarshal(knowledge.Metadata, &parsed); len(knowledge.Metadata) > 0 && err != nil {
		return nil, "", err
	}
	if parsed.Path != "" {
		kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
		file, err := s.resolveFileServiceForPath(ctx, kb, parsed.Path).GetFile(ctx, parsed.Path)
		return file, sanitizeManualDownloadFilename(knowledge.Title), err
	}

	// Intake originals are stored independently of the uploaded binary and DB row.
	if intake, intakeErr := knowledge.IntakeMetadata(); intakeErr != nil {
		return nil, "", intakeErr
	} else if intake != nil {
		if intake.OriginalPath == "" {
			return nil, "", fmt.Errorf("original is not available")
		}
		kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
		file, getErr := s.resolveFileServiceForPath(ctx, kb, intake.OriginalPath).GetFile(ctx, intake.OriginalPath)
		return file, sanitizeManualDownloadFilename(knowledge.Title), getErr
	}
	// Legacy manual records still keep their body in metadata.
	if knowledge.IsManual() {
		meta, err := knowledge.ManualMetadata()
		if err != nil {
			return nil, "", err
		}
		// ManualMetadata returns (nil, nil) when Metadata column is empty; treat as empty content.
		content := ""
		if meta != nil {
			content = meta.Content
		}
		filename := sanitizeManualDownloadFilename(knowledge.Title)
		return io.NopCloser(strings.NewReader(content)), filename, nil
	}

	// Resolve KB-level file service with FilePath fallback protection
	kb, _ := s.kbService.GetKnowledgeBaseByID(ctx, knowledge.KnowledgeBaseID)
	file, err := s.resolveFileServiceForPath(ctx, kb, knowledge.FilePath).GetFile(ctx, knowledge.FilePath)
	if err != nil {
		return nil, "", err
	}

	return file, knowledge.FileName, nil
}

func (s *knowledgeService) UpdateKnowledge(ctx context.Context, knowledge *types.Knowledge) error {
	record, err := s.repo.GetKnowledgeByID(ctx, ctx.Value(types.TenantIDContextKey).(uint64), knowledge.ID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get knowledge record: %v", err)
		return err
	}
	// if need other fields update, please add here
	if knowledge.Title != "" {
		record.Title = knowledge.Title
	}
	if knowledge.DescriptionSpecified {
		record.Description = knowledge.Description
		if record.Description != "" {
			record.SummaryStatus = types.SummaryStatusCompleted
		} else {
			record.SummaryStatus = types.SummaryStatusNone
		}
	} else if knowledge.Description != "" {
		record.Description = knowledge.Description
	}
	if knowledge.CustomMetadata != nil {
		var custom map[string]interface{}
		if err := json.Unmarshal(knowledge.CustomMetadata, &custom); err != nil {
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
		record.CustomMetadata = knowledge.CustomMetadata
	}

	// Update knowledge record in the repository
	if err := s.repo.UpdateKnowledge(ctx, record); err != nil {
		logger.Errorf(ctx, "Failed to update knowledge: %v", err)
		return err
	}
	if s.vault != nil && record.SourceKind == "article" && record.VaultPath != "" {
		s.vault.ScheduleRefresh(ctx, record.ID)
	}
	logger.Infof(ctx, "Knowledge updated successfully, ID: %s", knowledge.ID)
	return nil
}

// GetKnowledgeBatch retrieves multiple knowledge entries by their IDs
func (s *knowledgeService) GetKnowledgeBatch(ctx context.Context,
	tenantID uint64, ids []string,
) ([]*types.Knowledge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	articles, err := s.repo.GetKnowledgeBatch(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	if err := s.loadKnowledgeAssociations(ctx, tenantID, articles); err != nil {
		return nil, err
	}
	return articles, nil
}

func findKnowledgeByID(list []*types.Knowledge, id string) *types.Knowledge {
	for _, knowledge := range list {
		if knowledge.ID == id {
			return knowledge
		}
	}
	return nil
}

func (s *knowledgeService) SearchKnowledge(ctx context.Context, keyword string, offset, limit int, fileTypes []string) ([]*types.Knowledge, bool, int64, error) {
	tenantID, ok := ctx.Value(types.TenantIDContextKey).(uint64)
	if !ok {
		return nil, false, 0, werrors.NewUnauthorizedError("Workspace ID not found in context")
	}

	kb, err := s.kbService.GetKnowledgeBase(ctx)
	if err != nil {
		return nil, false, 0, err
	}
	return s.repo.SearchKnowledgeInScopes(ctx, []types.KnowledgeSearchScope{{TenantID: tenantID, KBID: kb.ID}}, keyword, offset, limit, fileTypes)
}

// Package container implements dependency injection container setup
// Provides centralized configuration for services, repositories, and handlers
// This package is responsible for wiring up all dependencies and ensuring proper lifecycle management
package container

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/panjf2000/ants/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/dig"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"reliquary/internal/agent/approval"
	"reliquary/internal/application/repository"
	postgresRepo "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service"
	chatpipeline "reliquary/internal/application/service/chat_pipeline"
	"reliquary/internal/application/service/file"
	"reliquary/internal/application/service/memory"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/common"
	"reliquary/internal/config"
	"reliquary/internal/database"
	"reliquary/internal/datasource"
	"reliquary/internal/datasource/connector/feishu/core"
	"reliquary/internal/datasource/connector/feishu/drive"
	"reliquary/internal/datasource/connector/feishu/wiki"
	gitlabConnector "reliquary/internal/datasource/connector/gitlab"
	grokXConnector "reliquary/internal/datasource/connector/grokx"
	imaConnector "reliquary/internal/datasource/connector/ima"
	notionConnector "reliquary/internal/datasource/connector/notion"
	rssConnector "reliquary/internal/datasource/connector/rss"
	yuqueConnector "reliquary/internal/datasource/connector/yuque"
	"reliquary/internal/event"
	"reliquary/internal/handler"
	"reliquary/internal/handler/session"
	imPkg "reliquary/internal/im"
	"reliquary/internal/im/feishu"
	"reliquary/internal/infrastructure/docparser"
	infra_web_search "reliquary/internal/infrastructure/web_search"
	"reliquary/internal/logger"
	"reliquary/internal/mcp"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/models/limiter"
	"reliquary/internal/models/utils/ollama"
	"reliquary/internal/router"
	"reliquary/internal/stream"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

// BuildContainer constructs the dependency injection container
// Registers all components, services, repositories and handlers needed by the application
// Creates a fully configured application container with proper dependency resolution
// Parameters:
//   - container: Base dig container to add dependencies to
//
// Returns:
//   - Configured container with all application dependencies registered
func BuildContainer(container *dig.Container) *dig.Container {
	ctx := context.Background()
	logger.Debugf(ctx, "[Container] Starting container initialization...")

	// Register resource cleaner for proper cleanup of resources
	must(container.Provide(NewResourceCleaner, dig.As(new(interfaces.ResourceCleaner))))

	// Core infrastructure configuration
	logger.Debugf(ctx, "[Container] Registering core infrastructure...")
	must(container.Provide(config.LoadConfig))
	must(container.Provide(initDatabase))
	must(container.Provide(initFileService))
	must(container.Provide(initRedisClient))
	must(container.Provide(initAntsPool))

	// Register goroutine pool cleanup handler
	must(container.Invoke(registerPoolCleanup))

	// Initialize retrieval engine registry for search capabilities
	logger.Debugf(ctx, "[Container] Registering retrieval engine registry...")
	must(container.Provide(initRetrieveEngineRegistry))

	// External service clients
	logger.Debugf(ctx, "[Container] Registering external service clients...")
	must(container.Provide(initDocReaderClient))
	must(container.Provide(docparser.NewImageResolver))
	must(container.Provide(initOllamaService))
	must(container.Provide(stream.NewStreamManager))
	logger.Debugf(ctx, "[Container] Initializing DuckDB...")
	must(container.Provide(NewDuckDB))
	logger.Debugf(ctx, "[Container] DuckDB registered")

	// Data repositories layer
	logger.Debugf(ctx, "[Container] Registering repositories...")
	must(container.Provide(repository.NewTenantRepository))
	must(container.Provide(repository.NewTenantAPIKeyRepository))
	must(container.Provide(repository.NewTenantMemberRepository))
	must(container.Provide(repository.NewTenantInvitationRepository))
	must(container.Provide(repository.NewAuditLogRepository))
	must(container.Provide(repository.NewKnowledgeBaseRepository))
	must(container.Provide(repository.NewKnowledgeRepository))
	must(container.Provide(repository.NewKnowledgeSpanRepository))
	must(container.Provide(repository.NewChunkRepository))
	must(container.Provide(repository.NewKnowledgeTagRepository))
	must(container.Provide(repository.NewSessionRepository))
	must(container.Provide(repository.NewMessageRepository))
	must(container.Provide(repository.NewMessageSuggestionRepository))
	must(container.Provide(repository.NewModelRepository))
	must(container.Provide(repository.NewUserRepository))
	must(container.Provide(repository.NewAuthTokenRepository))
	must(container.Provide(repository.NewSystemSettingRepository))
	must(container.Provide(repository.NewMCPServiceRepository))
	must(container.Provide(repository.NewMCPToolApprovalRepository))
	must(container.Provide(repository.NewMCPOAuthRepository))
	must(container.Provide(repository.NewTenantSandboxConfigRepository))
	must(container.Provide(repository.NewTenantSkillRepository))
	must(container.Provide(repository.NewCustomAgentRepository))
	must(container.Provide(repository.NewUserResourceFavoriteRepository))
	must(container.Provide(service.NewWebSearchStateService))
	must(container.Provide(repository.NewDataSourceRepository))
	must(container.Provide(repository.NewSyncLogRepository))
	must(container.Provide(repository.NewWikiPageRepository))
	must(container.Provide(repository.NewMemoryRepository))
	must(container.Provide(repository.NewTaskPendingOpsRepository))
	must(container.Provide(repository.NewTaskDeadLetterRepository))

	// MCP manager for managing MCP client connections
	logger.Debugf(ctx, "[Container] Registering MCP manager...")
	must(container.Provide(mcp.NewMCPManager))
	must(container.Provide(mcp.NewOAuthManager))

	// Sandbox manager fallback is disabled; executable backends are resolved
	// from named workspace configurations.
	logger.Debugf(ctx, "[Container] Registering sandbox manager...")
	must(container.Provide(newSandboxManager))
	// Per-tenant sandbox backends: the resolver builds a manager per request
	// from the tenant's own configuration, falling back to the singleton above
	// for tenants that configured nothing.
	must(container.Provide(service.NewTenantSandboxConfigLoader))
	must(container.Provide(newTenantSandboxResolver))

	// Business service layer
	logger.Debugf(ctx, "[Container] Registering business services...")
	must(container.Provide(service.NewTenantService))
	must(container.Provide(service.NewTenantAPIKeyService))
	must(container.Provide(service.NewTenantMemberService))
	must(container.Provide(service.NewTenantInvitationService))
	must(container.Provide(service.NewAuditLogService))
	must(container.Provide(service.NewAuditLogRetentionRunner))
	must(container.Provide(service.NewKnowledgeBaseService))
	must(container.Provide(service.NewKnowledgeService))
	must(container.Provide(service.NewSpanTracker))
	must(container.Provide(service.NewChunkService))
	must(container.Provide(service.NewKnowledgeTagService))
	must(container.Provide(service.NewKnowledgeAnomalyService))
	must(container.Provide(service.NewKnowledgeStrategyService))
	must(container.Provide(service.NewKnowledgeContextService))
	must(container.Provide(service.NewKnowledgeContextWorker))
	must(container.Provide(service.NewWorkflowSkillExecutor))
	must(container.Provide(service.NewKnowledgeWorkflowService))
	must(container.Provide(func(s *service.KnowledgeContextService) interfaces.KnowledgeContextReader { return s }))
	must(container.Provide(embedding.NewBatchEmbedder))
	must(container.Provide(service.NewModelService))
	must(container.Provide(service.NewDatasetService))
	must(container.Provide(service.NewEvaluationService))
	must(container.Provide(service.NewUserService))
	must(container.Provide(service.NewSystemSettingService))
	must(container.Provide(func(
		repo repository.TenantSandboxConfigRepository,
		agents interfaces.CustomAgentRepository,
		skills repository.TenantSkillRepository,
		files interfaces.StorageBackendResolver,
	) *service.TenantSandboxConfigService {
		return service.NewTenantSandboxConfigService(repo, agents, buildGlobalSandboxConfig(), skills, files)
	}))
	must(container.Provide(func(s *service.TenantSandboxConfigService) service.WorkspaceSandboxPolicy {
		return s
	}))

	// Extract services - register individual extracters with names
	must(container.Provide(service.NewDataTableSummaryService, dig.Name("dataTableSummary")))
	must(container.Provide(service.NewImageMultimodalService, dig.Name("imageMultimodal")))
	must(container.Provide(service.NewKnowledgePostProcessService, dig.Name("knowledgePostProcess")))
	must(container.Provide(service.NewKnowledgeAutoTagService, dig.Name("knowledgeAutoTag")))

	must(container.Provide(service.NewMessageService))
	must(container.Provide(service.NewMessageSuggestionService))
	must(container.Provide(service.NewMCPServiceService))
	must(container.Provide(service.NewMCPToolApprovalService))
	must(container.Provide(service.NewCustomAgentService))
	must(container.Provide(service.NewUserResourceFavoriteService))
	must(container.Provide(service.NewWikiPageService))
	must(container.Provide(service.NewWikiIngestService, dig.Name("wikiIngest")))
	must(container.Provide(service.NewWikiLintService))

	// Web search service (needed by AgentService)
	logger.Debugf(ctx, "[Container] Registering web search registry and providers...")
	must(container.Provide(infra_web_search.NewRegistry))
	must(container.Invoke(registerWebSearchProviders))
	must(container.Provide(repository.NewWebSearchProviderRepository))
	must(container.Provide(repository.NewVectorStoreRepository))
	must(container.Provide(repository.NewStorageBackendRepository))
	must(container.Provide(repository.NewResourceRepository))
	must(container.Provide(repository.NewTemporaryDocumentRepository))
	must(container.Provide(service.NewResourceCatalog))
	// TenantStoreOwnership adapter used by the retriever factory functions
	// to verify that a resolved VectorStore belongs to the caller's tenant.
	must(container.Provide(retriever.NewVectorStoreRepoOwnership))
	must(container.Provide(service.NewWebSearchService))
	must(container.Provide(service.NewWebSearchProviderService))
	must(container.Provide(NewEngineFactory))
	// StoreRegistry: same instance as RetrieveEngineRegistry, exposed as StoreRegistry interface.
	// NewRetrieveEngineRegistry always returns *retriever.RetrieveEngineRegistry which implements both.
	must(container.Provide(func(r interfaces.RetrieveEngineRegistry) (interfaces.StoreRegistry, error) {
		sr, ok := r.(*retriever.RetrieveEngineRegistry)
		if !ok {
			return nil, fmt.Errorf("registry does not implement StoreRegistry")
		}
		return sr, nil
	}))
	must(container.Provide(service.NewVectorStoreService))
	must(container.Provide(service.NewStorageBackendServiceWithResources))
	must(container.Provide(func(s *service.StorageBackendService) interfaces.StorageBackendService { return s }))
	must(container.Provide(func(s *service.StorageBackendService) interfaces.StorageBackendResolver { return s }))

	// Agent service layer (requires event bus, web search service)
	// SessionService is passed as parameter to CreateAgentEngine method when creating AgentService
	logger.Debugf(ctx, "[Container] Registering event bus and agent service...")
	must(container.Provide(event.NewEventBus))
	must(container.Provide(service.NewSessionSandboxPinner))
	must(container.Provide(func(cfg *config.Config, s interfaces.MCPToolApprovalService, rdb *redis.Client) *approval.Gate {
		return approval.NewGate(cfg, &approval.Adapter{Svc: s}, rdb)
	}))
	// Expose Gate as MCPApproval interface so AgentService and others can depend on the abstraction.
	must(container.Provide(func(g *approval.Gate) approval.MCPApproval { return g }))
	must(container.Provide(service.NewAgentService))

	// Session service (depends on agent service)
	// SessionService is created after AgentService and passes itself to AgentService.CreateAgentEngine when needed
	logger.Debugf(ctx, "[Container] Registering memory service...")
	must(container.Provide(memory.NewMemoryService))

	logger.Debugf(ctx, "[Container] Registering session service...")
	must(container.Provide(service.NewSessionService))
	must(container.Provide(service.NewTenantSkillService))
	// The member-facing half of env vars is its own service because its
	// authority is different in kind: it derives the identity from the context
	// and touches only that identity's rows.
	must(container.Provide(service.NewUserEnvService))

	// ArtifactCollector drains skill-generated files from the sandbox on
	// each agent turn (see spec at
	// docs/superpowers/specs/2026-07-10-skill-artifact-download-design.md).
	// The factory returns nil when the sandbox backend does not support
	// per-session file inspection; downstream code guards on nil.
	must(container.Provide(service.NewArtifactCollectorFromSandboxManager))

	logger.Debugf(ctx, "[Container] Registering task enqueuer...")
	must(container.Provide(router.NewAsyncqClient, dig.As(new(interfaces.TaskEnqueuer))))
	// Dedicated pools guarantee capacity for each task stage.
	must(container.Provide(router.NewCoreAsynqServer, dig.Name("coreAsynqServer")))
	must(container.Provide(router.NewPostProcessAsynqServer, dig.Name("postProcessAsynqServer")))
	must(container.Provide(router.NewEnrichmentAsynqServer, dig.Name("enrichmentAsynqServer")))
	must(container.Provide(router.NewMaintenanceAsynqServer, dig.Name("maintenanceAsynqServer")))
	must(container.Provide(router.NewSharedAsynqServer, dig.Name("sharedAsynqServer")))
	must(container.Provide(router.NewWikiAsynqServer, dig.Name("wikiAsynqServer")))
	must(container.Provide(router.NewAsynqInspector))
	must(container.Provide(router.NewAsynqTaskInspector))
	must(container.Invoke(registerModelConcurrencyLimiter))
	must(container.Provide(service.NewTemporaryDocumentService))
	must(container.Invoke(startTemporaryDocumentCleanup))

	// Chat pipeline components for processing chat requests
	logger.Debugf(ctx, "[Container] Registering chat pipeline plugins...")

	// Data source sync framework
	logger.Debugf(ctx, "[Container] Registering data source sync framework...")
	must(container.Provide(initConnectorRegistry))
	must(container.Provide(datasource.NewScheduler))
	must(container.Provide(service.NewDataSourceService))
	must(container.Invoke(startDataSourceScheduler))
	logger.Debugf(ctx, "[Container] Data source sync framework registered")
	must(container.Invoke(startAuditLogRetention))
	logger.Debugf(ctx, "[Container] Audit log retention runner registered")
	must(container.Provide(service.NewHousekeepingService))
	must(container.Invoke(startHousekeepingService))
	logger.Debugf(ctx, "[Container] Knowledge housekeeping runner registered")
	must(container.Provide(chatpipeline.NewEventManager))
	must(container.Invoke(chatpipeline.NewPluginSearch))
	must(container.Invoke(chatpipeline.NewPluginRerank))
	must(container.Invoke(chatpipeline.NewPluginWebFetch))
	must(container.Invoke(chatpipeline.NewPluginMerge))
	must(container.Invoke(chatpipeline.NewPluginDataAnalysis))
	must(container.Invoke(chatpipeline.NewPluginIntoChatMessage))
	must(container.Invoke(chatpipeline.NewPluginChatCompletion))
	must(container.Invoke(chatpipeline.NewPluginChatCompletionStream))
	must(container.Invoke(chatpipeline.NewPluginFilterTopK))
	must(container.Invoke(chatpipeline.NewPluginQueryUnderstand))
	must(container.Invoke(chatpipeline.NewPluginLoadHistory))
	must(container.Invoke(chatpipeline.NewPluginMemoryRecall))
	must(container.Invoke(chatpipeline.NewPluginSearchParallel))
	must(container.Invoke(chatpipeline.NewPluginWikiBoost))
	must(container.Invoke(chatpipeline.NewPluginMemoryAffinity))
	logger.Debugf(ctx, "[Container] Chat pipeline plugins registered")

	// TenantSkillService is provided next to SessionService (handlers need
	// it), but Invoke constructs the whole chain. SessionService needs
	// *chatpipeline.EventManager, which only exists after the pipeline
	// block above — starting the reaper any earlier panics.
	must(container.Invoke(startTenantSkillReaper))
	logger.Debugf(ctx, "[Container] Tenant skill reaper registered")

	// HTTP handlers layer
	logger.Debugf(ctx, "[Container] Registering HTTP handlers...")
	must(container.Provide(handler.NewTenantHandler))
	must(container.Provide(handler.NewTenantMemberHandler))
	must(container.Provide(handler.NewTenantInvitationHandler))
	must(container.Provide(handler.NewAuditLogHandler))
	must(container.Provide(handler.NewKnowledgeBaseHandler))
	must(container.Provide(handler.NewKnowledgeHandler))
	must(container.Provide(handler.NewKnowledgeAnomalyHandler))
	must(container.Provide(handler.NewKnowledgeStrategyHandler))
	must(container.Provide(handler.NewKnowledgeContextHandler))
	must(container.Provide(handler.NewKnowledgeWorkflowHandler))
	must(container.Provide(handler.NewChunkHandler))
	must(container.Provide(handler.NewTagHandler))
	must(container.Provide(session.NewHandler))
	must(container.Provide(handler.NewMessageHandler))
	must(container.Provide(handler.NewMessageSuggestionHandler))
	must(container.Provide(handler.NewModelHandler))
	must(container.Provide(handler.NewSandboxConfigHandler))
	must(container.Provide(func(
		s *service.TenantSkillService, streams interfaces.StreamManager,
	) *handler.SandboxSkillHandler {
		return handler.NewSandboxSkillHandler(s, streams)
	}))
	must(container.Provide(handler.NewMeEnvVarHandler))
	must(container.Provide(handler.NewEvaluationHandler))
	must(container.Provide(handler.NewInitializationHandler))
	must(container.Provide(handler.NewAuthHandler))
	must(container.Provide(handler.NewSystemHandler))
	must(container.Provide(handler.NewMCPServiceHandler))
	must(container.Provide(handler.NewMCPCredentialsHandler))
	must(container.Provide(handler.NewMCPOAuthHandler))
	must(container.Provide(handler.NewModelCredentialsHandler))
	must(container.Provide(handler.NewWebSearchProviderCredentialsHandler))
	must(container.Provide(handler.NewDataSourceCredentialsHandler))
	must(container.Provide(handler.NewWebSearchHandler))
	must(container.Provide(handler.NewWebSearchProviderHandler))
	must(container.Provide(handler.NewVectorStoreHandler))
	must(container.Provide(handler.NewStorageBackendHandler))
	must(container.Provide(handler.NewCustomAgentHandler))
	must(container.Provide(handler.NewUserResourceFavoriteHandler))
	must(container.Provide(service.NewSkillService))
	must(container.Provide(func(s *service.TenantSkillService) *handler.SkillHandler {
		return handler.NewSkillHandler(s, s)
	}))
	must(container.Provide(handler.NewMemoryHandler))

	// Data source handler
	must(container.Provide(handler.NewDataSourceHandler))
	// Wiki page handler
	must(container.Provide(handler.NewWikiPageHandler))
	// IM integration
	logger.Debugf(ctx, "[Container] Registering IM integration...")
	must(container.Provide(imPkg.NewService))
	must(container.Invoke(registerIMService))
	must(container.Provide(handler.NewIMHandler))
	logger.Debugf(ctx, "[Container] HTTP handlers registered")

	// Wire the chat package's local image resolver so multimodal chat can read
	// local:// images that live under a tenant's configured storage PathPrefix
	// (which is not encoded in the local:// URL).
	must(container.Invoke(registerChatLocalImageResolver))

	// Router configuration
	logger.Debugf(ctx, "[Container] Registering router and starting task server...")
	must(container.Provide(router.NewRouter))
	must(container.Invoke(router.RunAsynqServer))
	// Wiki operation rows are durable, while their wake-up triggers may be
	// lost across a process restart if persistence succeeded immediately before
	// trigger enqueue failed. Re-arm
	// them only after the matching handlers are ready.
	must(container.Invoke(recoverPendingWikiTasks))
	must(container.Invoke(startKnowledgeWorkflowScheduler))

	logger.Infof(ctx, "[Container] Container initialization completed successfully")
	return container
}

// registerChatLocalImageResolver wires the chat package's LocalImageResolver
// hook. Stored local:// URLs are relative to the resolved storage base dir and
// do NOT encode the owning tenant's configured PathPrefix, so resolving them to
// disk bytes requires rebuilding the FileService from that tenant's storage
// config. The owning tenant is parsed from the URL's first path segment, which
// correctly handles cross-tenant shared resources (e.g. shared KB images).
func registerChatLocalImageResolver(
	tenantRepo interfaces.TenantRepository,
	storageResolver interfaces.StorageBackendResolver,
	resourceCatalog interfaces.ResourceCatalog,
) {
	chat.LocalImageResolver = func(storageURL string) ([]byte, bool) {
		ctx := context.Background()
		physicalPath, resource, err := resourceCatalog.ResolvePath(ctx, storageURL)
		if err != nil {
			return nil, false
		}
		tenantID := secutils.ParseTenantIDFromStoragePath(physicalPath)
		if resource != nil {
			tenantID = resource.TenantID
		}
		if tenantID == 0 {
			return nil, false
		}
		tenant, err := tenantRepo.GetTenantByID(ctx, tenantID)
		if err != nil || tenant == nil {
			return nil, false
		}
		baseDir := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
		backendID, inner, scoped := types.ParseStorageBackendPath(physicalPath)
		if resource != nil && resource.StorageBackendID != "" {
			backendID = resource.StorageBackendID
		}
		providerPath := physicalPath
		if scoped {
			providerPath = inner
		}
		provider := types.ParseProviderScheme(providerPath)
		if provider == "" {
			provider = "local"
		}
		fileSvc, _, err := storageResolver.ResolveFileService(ctx, tenant, backendID, provider, baseDir)
		if err != nil {
			return nil, false
		}
		rc, err := fileSvc.GetFile(ctx, physicalPath)
		if err != nil {
			return nil, false
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, false
		}
		return data, true
	}
}

// must is a helper function for error handling
// Panics if the error is not nil, useful for configuration steps that must succeed
// Parameters:
//   - err: Error to check
func must(err error) {
	if err != nil {
		panic(err)
	}
}

// defaultModelMaxConcurrency caps per-model background requests.
const defaultModelMaxConcurrency = 32

// resolveModelMaxConcurrency reads the per-model background concurrency limit
// from system settings / env, defaulting to defaultModelMaxConcurrency when
// unset. A configured value of 0 (or negative) is honoured and disables the
// governor — that is the supported way to turn throttling off via config/env.
func resolveModelMaxConcurrency(ss interfaces.SystemSettingService) int {
	if ss == nil {
		return defaultModelMaxConcurrency
	}
	return int(ss.GetInt(context.Background(), "model.max_concurrency",
		"RELIQUARY_MODEL_MAX_CONCURRENCY", int64(defaultModelMaxConcurrency)))
}

// registerModelConcurrencyLimiter builds the Redis-backed per-model background
// concurrency governor (chat + vlm) and installs it.
func registerModelConcurrencyLimiter(rdb *redis.Client, ss interfaces.SystemSettingService) {
	limit := resolveModelMaxConcurrency(ss)
	limiter.SetGovernor(limiter.NewRedisLimiter(rdb), limit)
	if limit <= 0 {
		logger.Infof(context.Background(),
			"[ModelLimiter] background concurrency governor DISABLED (model.max_concurrency<=0)")
		return
	}
	logger.Infof(context.Background(),
		"[ModelLimiter] background model concurrency governed per-model, limit=%d (distributed via redis)", limit)
}

func initRedisClient() (*redis.Client, error) {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		return nil, fmt.Errorf("REDIS_ADDR is required")
	}
	db, err := strconv.Atoi(os.Getenv("REDIS_DB"))
	if err != nil {
		db = 0
	}

	client := redis.NewClient(&redis.Options{
		Addr:      redisAddr,
		Username:  os.Getenv("REDIS_USERNAME"),
		Password:  os.Getenv("REDIS_PASSWORD"),
		DB:        db,
		TLSConfig: common.RedisTLSConfig(),
	})

	_, err = client.Ping(context.Background()).Result()
	if err != nil {
		return nil, fmt.Errorf("连接Redis失败: %w", err)
	}

	return client, nil
}

// initDatabase initializes database connection
// Creates and configures database connection based on environment configuration
// Supports multiple database backends (PostgreSQL)
// Parameters:
//   - cfg: Application configuration
//
// Returns:
//   - Configured database connection
//   - Error if connection fails
func initDatabase(_ *config.Config) (*gorm.DB, error) {
	driver := strings.TrimSpace(os.Getenv("DB_DRIVER"))
	if driver != "" && driver != "postgres" {
		return nil, fmt.Errorf("unsupported database driver: %s", driver)
	}
	gormDSN := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"),
	)
	db, err := gorm.Open(postgres.Open(gormDSN), &gorm.Config{
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, err
	}
	if os.Getenv("AUTO_MIGRATE") != "false" {
		migrateDSN := fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=disable&options=-c%%20app.skip_embedding=false",
			os.Getenv("DB_USER"), url.QueryEscape(os.Getenv("DB_PASSWORD")),
			os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_NAME"),
		)
		opts := database.MigrationOptions{AutoRecoverDirty: os.Getenv("AUTO_RECOVER_DIRTY") != "false"}
		if err := database.RunMigrationsWithOptions(migrateDSN, opts); err != nil {
			return nil, fmt.Errorf("database migration failed: %w", err)
		}
		syncSequences(db)
		if err := service.EnsureSingleTeamSpace(context.Background(), db); err != nil {
			return nil, err
		}
		resetPendingTasks(db)
		if err := types.LoadBuiltinModelsConfig(context.Background(), db, config.ConfigDir()); err != nil {
			logger.Warnf(context.Background(), "Load builtin models config failed: %v", err)
		}
	} else {
		logger.Infof(context.Background(), "Auto-migration is disabled (AUTO_MIGRATE=false)")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(10 * time.Minute)
	return db, nil
}

// syncSequences ensures PostgreSQL sequences for auto-increment columns (seq_id)
// are at least as high as the current MAX value in each table. This is needed
// because older code assigned seq_id via application-level MAX()+1, which could
// advance values past the DB sequence counter and cause duplicate key errors.
func syncSequences(db *gorm.DB) {
	if db.Dialector.Name() != "postgres" {
		return
	}
	pairs := [][2]string{
		{"chunks", "chunks_seq_id_seq"},
		{"knowledge_tags", "knowledge_tags_seq_id_seq"},
	}
	for _, p := range pairs {
		table, seq := p[0], p[1]
		sql := fmt.Sprintf(
			`SELECT setval('%s', GREATEST(nextval('%s'), (SELECT COALESCE(MAX(seq_id), 0) FROM %s)))`,
			seq, seq, table,
		)
		if err := db.Exec(sql).Error; err != nil {
			logger.Warnf(context.Background(), "Failed to sync sequence %s: %v", seq, err)
		} else {
			logger.Infof(context.Background(), "Synced sequence %s with table %s", seq, table)
		}
	}
}

// initFileService initializes file storage service
// Creates the appropriate file storage service based on configuration
// Supports local filesystem and S3-compatible storage
// Parameters:
//   - cfg: Application configuration
//
// Returns:
//   - Configured file service implementation
//   - Error if initialization fails
func initFileService(cfg *config.Config, catalog interfaces.ResourceCatalog) (interfaces.FileService, error) {
	inner, err := initRawFileService(cfg)
	if err != nil {
		return nil, err
	}
	return file.NewResourceCatalogFileService(inner, catalog), nil
}

func initRawFileService(_ *config.Config) (interfaces.FileService, error) {
	backend := types.StorageBackendFromEnvironment(0)
	cfg := backend.ToStorageEngineConfig()
	svc, _, err := file.NewFileServiceFromStorageConfig(backend.Provider, cfg, os.Getenv("LOCAL_STORAGE_BASE_DIR"))
	return svc, err
}

// initRetrieveEngineRegistry registers the PostgreSQL hybrid search engine.
func initRetrieveEngineRegistry(
	db *gorm.DB, cfg *config.Config,
	storeRepo interfaces.VectorStoreRepository, engineFactory interfaces.EngineFactory,
) (interfaces.RetrieveEngineRegistry, error) {
	registry := retriever.NewRetrieveEngineRegistry(storeRepo, engineFactory).(*retriever.RetrieveEngineRegistry)
	engine := retriever.NewKVHybridRetrieveEngine(
		postgresRepo.NewPostgresRetrieveEngineRepository(db), types.PostgresRetrieverEngineType,
	)
	if err := registry.Register(engine); err != nil {
		return nil, err
	}
	var stores []types.VectorStore
	if err := db.Find(&stores).Error; err != nil {
		logger.Warnf(context.Background(), "Failed to load PostgreSQL vector stores: %v", err)
		return registry, nil
	}
	for _, store := range stores {
		svc, err := engineFactory(context.Background(), store)
		if err != nil {
			logger.Warnf(context.Background(), "Failed to load vector store %s: %v", store.ID, err)
			continue
		}
		registry.RegisterWithStoreID(store.ID, svc)
	}
	return registry, nil
}

// initAntsPool initializes the goroutine pool
// Creates a managed goroutine pool for concurrent task execution
// Parameters:
//   - cfg: Application configuration
//
// Returns:
//   - Configured goroutine pool
//   - Error if initialization fails
func initAntsPool(cfg *config.Config) (*ants.Pool, error) {
	// Default to 5 if not specified in config
	poolSize := os.Getenv("CONCURRENCY_POOL_SIZE")
	if poolSize == "" {
		poolSize = "5"
	}
	poolSizeInt, err := strconv.Atoi(poolSize)
	if err != nil {
		return nil, err
	}
	// Set up the pool with pre-allocation for better performance
	return ants.NewPool(poolSizeInt, ants.WithPreAlloc(true))
}

// registerPoolCleanup registers the goroutine pool for cleanup
// Ensures proper cleanup of the goroutine pool when application shuts down
// Parameters:
//   - pool: Goroutine pool
//   - cleaner: Resource cleaner
func registerPoolCleanup(pool *ants.Pool, cleaner interfaces.ResourceCleaner) {
	cleaner.RegisterWithName("AntsPool", func() error {
		pool.Release()
		return nil
	})
}

// initDocReaderClient initializes the DocumentReader client (lightweight API).
func initDocReaderClient(cfg *config.Config) (interfaces.DocumentReader, error) {
	addr := strings.TrimSpace(os.Getenv("DOCREADER_ADDR"))
	transport := strings.TrimSpace(os.Getenv("DOCREADER_TRANSPORT"))
	if transport == "" {
		transport = "grpc"
	}
	if addr == "" {
		logger.Infof(context.Background(), "[DocConverter] No DOCREADER_ADDR configured, starting disconnected")
	}
	transport = strings.ToLower(transport)
	switch transport {
	case "http", "https":
		if addr != "" && !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
			addr = "http://" + addr
		}
		return docparser.NewHTTPDocumentReader(addr)
	default:
		return docparser.NewGRPCDocumentReader(addr)
	}
}

// initOllamaService initializes the Ollama service client
// Creates a client for interacting with Ollama API for model inference
// Parameters:
//   - None
//
// Returns:
//   - Configured Ollama service client
//   - Error if initialization fails
func initOllamaService() (*ollama.OllamaService, error) {
	// Get Ollama service from existing factory function
	return ollama.GetOllamaService()
}

func NewDuckDB() (*sql.DB, error) {
	sqlDB, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("failed to open duckdb: %w", err)
	}

	// Try to install and load required extensions unless explicitly disabled.
	//   - spatial: used for st_read_meta() to enumerate layer (sheet) names from .xlsx/.xls
	//   - excel:   used for read_xlsx() which gives proper type inference per sheet
	//
	// INSTALL hits extensions.duckdb.org (public internet). In locked-down
	// runtimes with no egress, set DUCKDB_SKIP_EXTENSION_LOAD=1 to avoid a
	// startup hang; xlsx/xls ingest may fail later without these extensions.
	if strings.EqualFold(os.Getenv("DUCKDB_SKIP_EXTENSION_LOAD"), "true") ||
		os.Getenv("DUCKDB_SKIP_EXTENSION_LOAD") == "1" {
		logger.Infof(context.Background(),
			"[DuckDB] Skipping spatial/excel extension install/load "+
				"(DUCKDB_SKIP_EXTENSION_LOAD is set; xlsx ingest may fail without them)")
	} else {
		bgCtx := context.Background()
		for _, ext := range []string{"spatial", "excel"} {
			if _, err := sqlDB.ExecContext(bgCtx, fmt.Sprintf("INSTALL %s;", ext)); err != nil {
				logger.Warnf(bgCtx, "[DuckDB] Failed to install %s extension: %v", ext, err)
			}
			if _, err := sqlDB.ExecContext(bgCtx, fmt.Sprintf("LOAD %s;", ext)); err != nil {
				logger.Warnf(bgCtx, "[DuckDB] Failed to load %s extension: %v", ext, err)
			}
		}
	}

	return sqlDB, nil
}

// registerWebSearchProviders registers all web search provider types to the registry.
// Each provider type is registered with its factory function that accepts parameters.
// Provider instances are created on-demand when tenants configure them.
func registerWebSearchProviders(registry *infra_web_search.Registry) {
	registry.Register("duckduckgo", infra_web_search.NewDuckDuckGoProvider)
	registry.Register("google", infra_web_search.NewGoogleProvider)
	registry.Register("bing", infra_web_search.NewBingProvider)
	registry.Register("tavily", infra_web_search.NewTavilyProvider)
	registry.Register("ollama", infra_web_search.NewOllamaProvider)
	registry.Register("baidu", infra_web_search.NewBaiduProvider)
	registry.Register("searxng", infra_web_search.NewSearxngProvider)
	registry.Register("keenable", infra_web_search.NewKeenableProvider)
	registry.Register("zhipu", infra_web_search.NewZhipuProvider)
	registry.Register("exa", infra_web_search.NewExaProvider)
	registry.Register("metaso", infra_web_search.NewMetasoProvider)
}

// registerIMService registers adapter factories, loads enabled channels, and
// wires the process-lifetime shutdown hook. Each platform's factory lives in
// its own subpackage to keep this file focused on wiring.
func registerIMService(imService *imPkg.Service, cleaner interfaces.ResourceCleaner) {
	imService.RegisterAdapterFactory("feishu", feishu.NewFactory(feishu.RegionFeishu))
	// Lark is Feishu's international cloud: same adapter, different host/tenant.
	imService.RegisterAdapterFactory("lark", feishu.NewFactory(feishu.RegionLark))

	// Load and start all enabled channels from database
	if err := imService.LoadAndStartChannels(); err != nil {
		logger.Warnf(context.Background(), "[IM] Failed to load channels from database: %v", err)
	}

	cleaner.RegisterWithName("IMService", func() error {
		imService.Stop()
		return nil
	})
}

// initConnectorRegistry creates and populates the connector registry with all available connectors.
// Aggregates registration errors via errors.Join so a misconfigured or duplicated connector fails
// container initialization loudly instead of silently disabling the feature at runtime.
func initConnectorRegistry() (*datasource.ConnectorRegistry, error) {
	registry := datasource.NewConnectorRegistry()

	var errs error
	if err := registry.Register(wiki.NewConnector(core.RegionFeishu)); err != nil {
		errs = errors.Join(errs, fmt.Errorf("register feishu connector: %w", err))
	}
	// Lark is Feishu's international cloud: same connector, different host/tenant.
	if err := registry.Register(wiki.NewConnector(core.RegionLark)); err != nil {
		errs = errors.Join(errs, fmt.Errorf("register lark connector: %w", err))
	}
	// Feishu/Lark Drive (云盘) mode: different connector type so the registry
	// dispatches to the Drive connector. Shares core.Client/Region/export logic
	// with the wiki connector. See 飞书云盘数据源设计.md / ADR-0001.
	if err := registry.Register(drive.NewDriveConnector(core.RegionFeishuDrive)); err != nil {
		errs = errors.Join(errs, fmt.Errorf("register feishu_drive connector: %w", err))
	}
	if err := registry.Register(drive.NewDriveConnector(core.RegionLarkDrive)); err != nil {
		errs = errors.Join(errs, fmt.Errorf("register lark_drive connector: %w", err))
	}
	if err := registry.Register(notionConnector.NewConnector()); err != nil {
		errs = errors.Join(errs, fmt.Errorf("register notion connector: %w", err))
	}
	if err := registry.Register(yuqueConnector.NewConnector()); err != nil {
		errs = errors.Join(errs, fmt.Errorf("register yuque connector: %w", err))
	}
	if err := registry.Register(imaConnector.NewConnector()); err != nil {
		errs = errors.Join(errs, fmt.Errorf("register ima connector: %w", err))
	}
	if err := registry.Register(rssConnector.NewConnector()); err != nil {
		errs = errors.Join(errs, fmt.Errorf("register rss connector: %w", err))
	}
	if err := registry.Register(grokXConnector.NewConnector()); err != nil {
		errs = errors.Join(errs, fmt.Errorf("register grok_x connector: %w", err))
	}
	if err := registry.Register(gitlabConnector.NewConnector()); err != nil {
		errs = errors.Join(errs, fmt.Errorf("register gitlab connector: %w", err))
	}

	// Future connectors will be registered here:
	// if err := registry.Register(confluenceConnector.NewConnector()); err != nil { ... }
	// if err := registry.Register(githubConnector.NewConnector()); err != nil { ... }

	if errs != nil {
		return nil, errs
	}
	return registry, nil
}

// startDataSourceScheduler starts the data source cron scheduler and registers cleanup.
func startDataSourceScheduler(scheduler *datasource.Scheduler, cleaner interfaces.ResourceCleaner) {
	if err := scheduler.Start(context.Background()); err != nil {
		logger.Warnf(context.Background(), "[Container] data source scheduler start failed: %v", err)
	}

	cleaner.RegisterWithName("DataSourceScheduler", func() error {
		scheduler.Stop()
		return nil
	})
}

// startHousekeepingService starts the knowledge housekeeping cron and registers
// cleanup. This is the safety net that recovers any knowledge stuck in
// "processing" past a configurable threshold (see HousekeepingService for
// rationale). Best-effort: a startup error is logged but does NOT abort the
// container — the rest of the system stays usable.
func startHousekeepingService(svc *service.HousekeepingService, cleaner interfaces.ResourceCleaner) {
	if svc == nil {
		return
	}
	if err := svc.Start(context.Background()); err != nil {
		logger.Warnf(context.Background(), "[Container] housekeeping start failed: %v", err)
	}
	cleaner.RegisterWithName("KnowledgeHousekeeping", func() error {
		svc.Stop()
		return nil
	})
}

// startTenantSkillReaper starts the stuck-install / orphan-snapshot cron and
// registers cleanup. Best-effort: a startup error is logged but does NOT abort
// the container — the rest of the system stays usable.
func startTenantSkillReaper(svc *service.TenantSkillService, cleaner interfaces.ResourceCleaner) {
	if svc == nil {
		return
	}
	if err := svc.Start(context.Background()); err != nil {
		logger.Warnf(context.Background(), "[Container] tenant skill reaper start failed: %v", err)
	}
	cleaner.RegisterWithName("TenantSkillReaper", func() error {
		svc.Stop()
		return nil
	})
}

// startTemporaryDocumentCleanup removes expired session attachments and their
// extracted images. The durable expiry timestamp is the source of truth; the
// ticker only controls how quickly storage is reclaimed.
func startTemporaryDocumentCleanup(svc interfaces.TemporaryDocumentService, cleaner interfaces.ResourceCleaner) {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := svc.CleanupExpired(context.Background()); err != nil {
					logger.Warnf(context.Background(), "[TemporaryDocument] cleanup failed: %v", err)
				}
			case <-stop:
				return
			}
		}
	}()
	cleaner.RegisterWithName("TemporaryDocumentCleanup", func() error {
		close(stop)
		return nil
	})
}

// startAuditLogRetention spins up the daily audit_logs purge sweep
// and registers shutdown cleanup. Mirrors the data-source-scheduler
// pattern: container init kicks the goroutine, ResourceCleaner stops
// it during graceful shutdown so a SIGTERM during a sweep doesn't
// orphan the goroutine.
//
// retention_days <= 0 is the configured way to disable retention;
// the runner short-circuits Start() on that path so we don't need
// to gate the wiring here.
func startAuditLogRetention(
	runner *service.AuditLogRetentionRunner, cleaner interfaces.ResourceCleaner,
) {
	runner.Start(context.Background())
	cleaner.RegisterWithName("AuditLogRetentionRunner", func() error {
		runner.Stop()
		return nil
	})
}

func startKnowledgeWorkflowScheduler(svc *service.KnowledgeWorkflowService, cleaner interfaces.ResourceCleaner) {
	if err := svc.Start(context.Background()); err != nil {
		logger.Warnf(context.Background(), "[Container] knowledge workflow scheduler: %v", err)
	}
	cleaner.RegisterWithName("KnowledgeWorkflowScheduler", func() error { svc.Stop(); return nil })
}

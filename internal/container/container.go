// Package container implements dependency injection container setup
// Provides centralized configuration for services, repositories, and handlers
// This package is responsible for wiring up all dependencies and ensuring proper lifecycle management
package container

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/application/service/file"
	"reliquary/internal/chatgpt"
	"reliquary/internal/config"
	"reliquary/internal/database"
	"reliquary/internal/handler"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/logger"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/models/limiter"
	"reliquary/internal/queue"
	"reliquary/internal/router"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils/ollama"
	"strconv"
	"strings"
	"time"

	"github.com/panjf2000/ants/v2"

	"go.uber.org/dig"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
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
	must(container.Provide(newIngestDiagnostics))
	must(container.Provide(initDatabase))
	must(container.Provide(initFileService))
	must(container.Provide(initAntsPool))

	// Register goroutine pool cleanup handler
	must(container.Invoke(registerPoolCleanup))

	// Query PostgreSQL for retrieval and indexing.
	must(container.Provide(NewPostgresRetrievalService))

	// External service clients
	logger.Debugf(ctx, "[Container] Registering external service clients...")
	must(container.Provide(docparser.NewImageResolver))
	must(container.Provide(initOllamaService))

	// Data repositories layer
	logger.Debugf(ctx, "[Container] Registering repositories...")
	must(container.Provide(repository.NewSpaceRepository))
	must(container.Provide(repository.NewAPIKeyRepository))
	must(container.Provide(repository.NewLibraryRepository))
	must(container.Provide(repository.NewEntryRepository))
	must(container.Provide(repository.NewEntrySpanRepository))
	must(container.Provide(repository.NewChunkRepository))
	must(container.Provide(repository.NewModelRepository))
	must(container.Provide(repository.NewUserRepository))
	must(container.Provide(repository.NewSystemSettingRepository))
	must(container.Provide(repository.NewTaskDeadLetterRepository))

	// Business service layer
	logger.Debugf(ctx, "[Container] Registering business services...")
	must(container.Provide(service.NewSpaceService))
	must(container.Provide(service.NewAPIKeyService))
	must(container.Provide(service.NewLibraryService))
	must(container.Provide(service.NewEntryService))
	must(container.Provide(service.NewSearchService))
	must(container.Provide(service.NewEvaluationService))
	must(container.Provide(service.NewSearchRecorder))
	must(container.Provide(handler.NewEvaluationHandler))
	must(container.Invoke(startSearchRecordCleanup))
	must(container.Provide(service.NewSpanTracker))
	must(container.Provide(service.NewChunkService))
	must(container.Provide(service.NewEntityService))
	must(container.Provide(service.NewFollowService))
	must(container.Provide(service.NewOperationLogService))
	must(container.Provide(service.NewEmbeddingRebuilder))
	must(container.Provide(embedding.NewBatchEmbedder))
	must(container.Provide(func(db *gorm.DB) *chatgpt.Manager { return chatgpt.NewManager(db) }))
	must(container.Provide(service.NewChatGPTPolicyService))
	must(container.Provide(service.NewModelServiceWithChatGPT))
	must(container.Provide(service.NewUserService))
	must(container.Provide(service.NewSystemSettingService))
	must(container.Provide(service.NewVaultService))
	must(container.Provide(newVaultDocumentService))
	// Extract services - register individual extracters with names

	must(container.Provide(repository.NewResourceRepository))
	must(container.Provide(service.NewResourceCatalog))
	logger.Debugf(ctx, "[Container] Registering task enqueuer...")
	must(container.Provide(queue.NewClient))
	must(container.Provide(func(client *queue.Client) interfaces.TaskEnqueuer { return client }))
	must(container.Provide(router.NewPostgresTaskInspector))
	must(container.Invoke(registerModelConcurrencyLimiter))

	must(container.Provide(service.NewHousekeepingService))
	must(container.Invoke(startHousekeepingService))
	logger.Debugf(ctx, "[Container] Entry housekeeping runner registered")

	// HTTP handlers layer
	logger.Debugf(ctx, "[Container] Registering HTTP handlers...")
	must(container.Provide(handler.NewSpaceHandler))
	must(container.Provide(handler.NewLibraryHandler))
	must(container.Provide(handler.NewEntryHandler))
	must(container.Provide(handler.NewIngestDiagnosticsHandler))
	must(container.Provide(handler.NewVaultHandler))
	must(container.Provide(handler.NewMaintenanceHandler))
	must(container.Provide(handler.NewSearchHandler))
	must(container.Provide(handler.NewEntityHandler))
	must(container.Provide(handler.NewEmbeddingRebuildHandler))
	must(container.Provide(handler.NewFollowHandler))
	must(container.Provide(handler.NewOperationLogHandler))
	must(container.Provide(newMCPHandler))
	must(container.Provide(handler.NewChunkHandler))
	must(container.Provide(handler.NewModelHandlerWithMaintenance))
	must(container.Provide(handler.NewChatGPTHandler))
	must(container.Provide(handler.NewChatGPTPolicyHandler))
	must(container.Provide(handler.NewInitializationHandler))
	must(container.Provide(handler.NewAuthHandler))
	must(container.Provide(handler.NewSystemHandler))
	must(container.Provide(handler.NewModelCredentialsHandler))

	logger.Debugf(ctx, "[Container] HTTP handlers registered")

	// Wire the model package's local image resolver so image inputs can read
	// local:// images that live under a space's configured storage PathPrefix
	// (which is not encoded in the local:// URL).
	must(container.Invoke(registerModelLocalImageResolver))
	must(container.Invoke(registerVaultIntegration))
	must(container.Invoke(registerIngestDiagnostics))

	// Router configuration
	logger.Debugf(ctx, "[Container] Registering router and starting task server...")
	must(container.Provide(router.NewRouter))
	must(container.Invoke(router.RunTaskServer))

	logger.Infof(ctx, "[Container] Container initialization completed successfully")
	return container
}

// registerModelLocalImageResolver loads catalog-backed local images for model calls.
func registerModelLocalImageResolver(files interfaces.FileService) {
	chat.LocalImageResolver = func(storageURL string) ([]byte, bool) {
		rc, err := files.GetFile(context.Background(), storageURL)
		if err != nil {
			return nil, false
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		return data, err == nil
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

// registerModelConcurrencyLimiter builds the process-local per-model background
// concurrency governor for text generation and installs it.
func registerModelConcurrencyLimiter(ss interfaces.SystemSettingService) {
	limit := resolveModelMaxConcurrency(ss)
	limiter.SetGovernor(limiter.NewLocalLimiter(), limit)
	if limit <= 0 {
		logger.Infof(context.Background(),
			"[ModelLimiter] background concurrency governor DISABLED (model.max_concurrency<=0)")
		return
	}
	logger.Infof(context.Background(),
		"[ModelLimiter] background model concurrency governed per-model, limit=%d (in process)", limit)
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
		// Bound values can contain submitted content. Keep SQL diagnostics without copying those values into shared logs.
		Logger: gormlogger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), gormlogger.Config{
			SlowThreshold: 200 * time.Millisecond, LogLevel: gormlogger.Warn, ParameterizedQueries: true,
		}),
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
		if err := initializeRuntimeData(context.Background(), db, config.ConfigDir()); err != nil {
			return nil, err
		}
	} else {
		logger.Infof(context.Background(), "Auto-migration is disabled (AUTO_MIGRATE=false)")
	}
	if err := service.EnsurePersonalSpace(context.Background(), db); err != nil {
		return nil, err
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

// initFileService registers uploaded files in the resource catalog.
func initFileService(cfg *config.Config, catalog interfaces.ResourceCatalog) (interfaces.FileService, error) {
	inner, err := initRawFileService(cfg)
	if err != nil {
		return nil, err
	}
	return file.NewResourceCatalogFileService(inner, catalog), nil
}

func initRawFileService(_ *config.Config) (interfaces.FileService, error) {
	baseDir := strings.TrimSpace(os.Getenv("LOCAL_STORAGE_BASE_DIR"))
	if baseDir == "" {
		baseDir = "/data/files"
	}
	return file.NewLocalFileService(baseDir, os.Getenv("APP_EXTERNAL_URL")), nil
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

func startHousekeepingService(svc *service.HousekeepingService, cleaner interfaces.ResourceCleaner) {
	if svc == nil {
		return
	}
	if err := svc.Start(context.Background()); err != nil {
		logger.Warnf(context.Background(), "[Container] housekeeping start failed: %v", err)
	}
	cleaner.RegisterWithName("EntryHousekeeping", func() error {
		svc.Stop()
		return nil
	})
}

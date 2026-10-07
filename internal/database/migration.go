package database

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"reliquary/internal/logger"
)

var (
	migrationStateMu        sync.RWMutex
	currentMigrationVersion uint
	currentMigrationDirty   bool
	migrationVersionSet     bool
	currentMigrationError   string
)

// CachedMigrationVersion returns the migration version captured at startup.
// Returns (version, dirty, ok). ok is false if the version was never captured.
//
// Note: when migrations fail mid-way, the cache may still be populated via a
// best-effort m.Version() call inside RunMigrationsWithOptions so the system
// info endpoint can surface the partial state. Check CachedMigrationError() to
// distinguish a clean version reading from a recorded-after-failure one.
func CachedMigrationVersion() (uint, bool, bool) {
	migrationStateMu.RLock()
	defer migrationStateMu.RUnlock()
	return currentMigrationVersion, currentMigrationDirty, migrationVersionSet
}

// CachedMigrationError returns the error message captured when the most recent
// migration attempt failed at startup. Empty string means migrations either
// succeeded or were never run.
func CachedMigrationError() string {
	migrationStateMu.RLock()
	defer migrationStateMu.RUnlock()
	return currentMigrationError
}

// setMigrationState records the latest known migration state. Unlike the old
// sync.Once-based setter, this is intentionally idempotent-overwrite so the
// failure path (which runs after Up() errored) can replace the pre-migration
// snapshot taken from the initial m.Version() call.
func setMigrationState(version uint, dirty bool, errMsg string, versionKnown bool) {
	migrationStateMu.Lock()
	defer migrationStateMu.Unlock()
	if versionKnown {
		currentMigrationVersion = version
		currentMigrationDirty = dirty
		migrationVersionSet = true
	}
	currentMigrationError = errMsg
}

// captureMigrationFailure best-effort queries m for the current version so the
// system info endpoint can show "N (failed)" instead of vanishing the row, and
// stores the human-readable error message. Always returns the original error.
func captureMigrationFailure(m *migrate.Migrate, err error) error {
	versionKnown := false
	var ver uint
	var dirty bool
	if m != nil {
		v, d, vErr := m.Version()
		if vErr == nil {
			versionKnown = true
			ver, dirty = v, d
		}
	}
	setMigrationState(ver, dirty, err.Error(), versionKnown)
	return err
}

// RunMigrations executes all pending database migrations
// This should be called during application startup
func RunMigrations(dsn string) error {
	return RunMigrationsWithOptions(dsn, MigrationOptions{AutoRecoverDirty: false})
}

// MigrationOptions configures migration behavior
type MigrationOptions struct {
	// AutoRecoverDirty when true, automatically attempts to recover from dirty state
	// by forcing to the previous version and retrying the migration
	AutoRecoverDirty bool
}

// RunMigrationsWithOptions executes all pending database migrations with custom options
func RunMigrationsWithOptions(dsn string, opts MigrationOptions) error {
	ctx := context.Background()

	logger.Infof(ctx, "Starting database migration...")

	migrationSource, err := source.Open("file://migrations/versioned")
	if err != nil {
		wrapped := fmt.Errorf("failed to open migration source: %w", err)
		setMigrationState(0, false, wrapped.Error(), false)
		return wrapped
	}
	m, err := migrate.NewWithSourceInstance("file", migrationSource, dsn)
	if err != nil {
		migrationSource.Close()
		logger.Errorf(ctx, "Failed to create migrate instance: %v", err)
		wrapped := fmt.Errorf("failed to create migrate instance: %w", err)
		setMigrationState(0, false, wrapped.Error(), false)
		return wrapped
	}
	defer m.Close()

	// Check current version and dirty state before migration
	oldVersion, oldDirty, versionErr := m.Version()
	if versionErr != nil && versionErr != migrate.ErrNilVersion {
		logger.Errorf(ctx, "Failed to get migration version: %v", versionErr)
		return captureMigrationFailure(m, fmt.Errorf("failed to get migration version: %w", versionErr))
	}

	if versionErr == migrate.ErrNilVersion {
		logger.Infof(ctx, "Database has no migration history, will start from version 0")
	} else {
		logger.Infof(ctx, "Current migration version: %d, dirty: %v", oldVersion, oldDirty)
		if err := validateMigrationVersion(migrationSource, oldVersion); err != nil {
			return captureMigrationFailure(m, err)
		}
	}

	// If database is in dirty state, try to recover or return error
	if oldDirty {
		logger.Warnf(ctx, "Database is in dirty state at version %d", oldVersion)
		if opts.AutoRecoverDirty {
			logger.Infof(ctx, "AutoRecoverDirty is enabled, attempting recovery...")
			if err := recoverFromDirtyState(ctx, m, migrationSource, oldVersion); err != nil {
				return captureMigrationFailure(m, err)
			}
			// Update oldVersion after recovery
			oldVersion, _, _ = m.Version()
		} else {
			return captureMigrationFailure(m, dirtyMigrationError(migrationSource, oldVersion))
		}
	}

	// Run all pending migrations
	logger.Infof(ctx, "Running pending migrations...")
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		logger.Errorf(ctx, "Migration failed: %v", err)
		// Check if error is due to dirty state (in case it became dirty during migration)
		currentVersion, currentDirty, versionCheckErr := m.Version()
		if versionCheckErr == nil && currentDirty {
			logger.Warnf(ctx, "Migration caused dirty state at version %d", currentVersion)
			if opts.AutoRecoverDirty {
				logger.Infof(ctx, "Attempting to recover from dirty state...")
				// Try to recover and retry
				if recoverErr := recoverFromDirtyState(ctx, m, migrationSource, currentVersion); recoverErr != nil {
					return captureMigrationFailure(m, recoverErr)
				}
				// Retry migration after recovery
				logger.Infof(ctx, "Retrying migration after recovery...")
				if retryErr := m.Up(); retryErr != nil && retryErr != migrate.ErrNoChange {
					logger.Errorf(ctx, "Migration failed after recovery attempt: %v", retryErr)
					return captureMigrationFailure(m, fmt.Errorf("migration failed after recovery attempt: %w", retryErr))
				}
			} else {
				return captureMigrationFailure(m, fmt.Errorf("migration failed: %w; %w", err,
					dirtyMigrationError(migrationSource, currentVersion)))
			}
		} else {
			return captureMigrationFailure(m, fmt.Errorf("failed to run migrations: %w", err))
		}
	}

	// Get current version after migration
	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return captureMigrationFailure(m, fmt.Errorf("failed to get migration version: %w", err))
	}

	setMigrationState(version, dirty, "", true)

	if oldVersion != version {
		logger.Infof(ctx, "Database migrated from version %d to %d", oldVersion, version)
	} else {
		logger.Infof(ctx, "Database is up to date (version: %d)", version)
	}

	if dirty {
		logger.Warnf(ctx, "Database is in dirty state! Manual intervention may be required.")
	}

	return nil
}

func validateMigrationVersion(migrationSource source.Driver, version uint) error {
	reader, _, err := migrationSource.ReadUp(version)
	if os.IsNotExist(err) {
		return fmt.Errorf("database migration version %d is not present in the current migration history. "+
			"Databases created before the 2026-10-07 baseline must be recreated; in-place upgrades are not supported", version)
	}
	if err != nil {
		return fmt.Errorf("failed to read migration version %d: %w", version, err)
	}
	return reader.Close()
}

func previousMigrationVersion(migrationSource source.Driver, version uint) (int, error) {
	previous, err := migrationSource.Prev(version)
	if os.IsNotExist(err) {
		return -1, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to find migration before version %d: %w", version, err)
	}
	return int(previous), nil
}

func dirtyMigrationError(migrationSource source.Driver, version uint) error {
	forceVersion, err := previousMigrationVersion(migrationSource, version)
	if err != nil {
		return err
	}
	return fmt.Errorf("database is in dirty state at version %d. To retry:\n"+
		"1. Remove any partially created objects or recreate the database; force does not undo SQL changes\n"+
		"2. Reset the migration history: ./scripts/migrate.sh force %d\n"+
		"   Or if using make: make migrate-force version=%d\n"+
		"3. Restart the application to retry the migration\n"+
		"AutoRecoverDirty can reset the migration history automatically after partial changes are resolved",
		version, forceVersion, forceVersion)
}

// recoverFromDirtyState resets to the previous available version, or -1 before
// the initial migration. It does not roll back partially applied SQL changes.
func recoverFromDirtyState(ctx context.Context, m *migrate.Migrate, migrationSource source.Driver, dirtyVersion uint) error {
	if err := validateMigrationVersion(migrationSource, dirtyVersion); err != nil {
		return err
	}
	forceVersion, err := previousMigrationVersion(migrationSource, dirtyVersion)
	if err != nil {
		return err
	}

	logger.Warnf(ctx, "Database is in dirty state at version %d, attempting auto-recovery by forcing to version %d",
		dirtyVersion, forceVersion)
	logger.Warnf(ctx, "Partial migration changes must be resolved before retrying; force does not undo SQL changes")

	// Force to previous version to clear dirty state
	if err := m.Force(forceVersion); err != nil {
		return fmt.Errorf("failed to force migration version during recovery: %w", err)
	}

	logger.Infof(ctx, "Successfully forced migration to version %d, migration will be retried", forceVersion)
	return nil
}

// GetMigrationVersion returns the current migration version
func GetMigrationVersion() (uint, bool, error) {
	dbURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_NAME"),
	)

	migrationsPath := "file://migrations/versioned"

	m, err := migrate.New(migrationsPath, dbURL)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create migrate instance: %w", err)
	}
	defer m.Close()

	version, dirty, err := m.Version()
	if err != nil {
		return 0, false, err
	}

	return version, dirty, nil
}

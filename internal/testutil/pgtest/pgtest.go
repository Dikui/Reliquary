// Package pgtest isolates PostgreSQL integration tests from the configured database.
package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const DSNEnv = "RELIQUARY_TEST_PG_DSN"

var packageDatabase struct {
	sync.Mutex
	admin    *sql.DB
	template string
	dsn      *url.URL
}

// Run releases the package's migrated template after all its tests complete.
// Packages using Open or OpenFixture call os.Exit(pgtest.Run(m)) in TestMain.
func Run(m *testing.M) int {
	code := m.Run()
	packageDatabase.Lock()
	defer packageDatabase.Unlock()
	if packageDatabase.admin != nil {
		if err := dropDatabase(packageDatabase.admin, packageDatabase.template); err != nil {
			fmt.Fprintln(os.Stderr, "pgtest: clean package database:", err)
			code = 1
		}
		if err := packageDatabase.admin.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "pgtest: close administration connection:", err)
			code = 1
		}
	}
	return code
}

// Open clones the fully migrated package database for one isolated test.
// An unset DSN skips the test; connection or migration errors fail it.
func Open(t testing.TB) *gorm.DB {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(DSNEnv))
	if raw == "" {
		t.Skip("PostgreSQL integration test requires " + DSNEnv)
	}
	packageDatabase.Lock()
	defer packageDatabase.Unlock()
	if packageDatabase.admin == nil {
		if err := initialize(raw); err != nil {
			t.Fatalf("pgtest: initialize migrated package database: %v", err)
		}
	}
	name := databaseName()
	if _, err := packageDatabase.admin.Exec("CREATE DATABASE " + name + " TEMPLATE " + packageDatabase.template); err != nil {
		t.Fatalf("pgtest: create isolated database: %v", err)
	}
	t.Cleanup(func() {
		if err := dropDatabase(packageDatabase.admin, name); err != nil {
			t.Errorf("pgtest: clean isolated database: %v", err)
		}
	})
	dsn := *packageDatabase.dsn
	dsn.Path = "/" + name
	db, err := connect(dsn.String())
	if err != nil {
		t.Fatalf("pgtest: connect isolated database: %v", err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatalf("pgtest: obtain connection pool: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Errorf("pgtest: close connection pool: %v", err)
		}
	})
	return db
}

// OpenFixture gives SQL-focused unit tests a separate schema for their minimal
// PostgreSQL tables. The package template still executes the full migrations.
// Open tests the migrated production schema without this additional schema.
func OpenFixture(t testing.TB) *gorm.DB {
	t.Helper()
	db := Open(t)
	if err := db.Exec("CREATE SCHEMA fixture").Error; err != nil {
		t.Fatalf("pgtest: create fixture schema: %v", err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatalf("pgtest: obtain fixture connection pool: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("pgtest: close migrated connection: %v", err)
	}
	var name string
	// The connection URL is held by the PostgreSQL dialector.
	dsn, err := url.Parse(db.Dialector.(*postgres.Dialector).DSN)
	if err != nil {
		t.Fatalf("pgtest: parse fixture connection URL: %v", err)
	}
	name = strings.TrimPrefix(dsn.Path, "/")
	query := dsn.Query()
	query.Set("search_path", "fixture,public")
	dsn.RawQuery = query.Encode()
	fixture, err := connect(dsn.String())
	if err != nil {
		t.Fatalf("pgtest: connect fixture schema in %s: %v", name, err)
	}
	fixturePool, err := fixture.DB()
	if err != nil {
		t.Fatalf("pgtest: obtain fixture pool: %v", err)
	}
	t.Cleanup(func() {
		if err := fixturePool.Close(); err != nil {
			t.Errorf("pgtest: close fixture pool: %v", err)
		}
	})
	return fixture
}

func initialize(raw string) error {
	dsn, err := url.Parse(raw)
	if err != nil || (dsn.Scheme != "postgres" && dsn.Scheme != "postgresql") || dsn.Host == "" {
		return fmt.Errorf("%s must be a PostgreSQL connection URL", DSNEnv)
	}
	adminDB, err := connect(dsn.String())
	if err != nil {
		return err
	}
	admin, err := adminDB.DB()
	if err != nil {
		return err
	}
	name := databaseName()
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		admin.Close()
		return fmt.Errorf("create package database (CREATEDB is required): %w", err)
	}
	success := false
	defer func() {
		if !success {
			dropDatabase(admin, name)
			admin.Close()
		}
	}()
	target := *dsn
	target.Path = "/" + name
	_, source, _, _ := runtime.Caller(0)
	migrations := filepath.Join(filepath.Dir(source), "..", "..", "..", "migrations", "versioned")
	m, err := migrate.New("file://"+filepath.ToSlash(migrations), target.String())
	if err != nil {
		return fmt.Errorf("open migrations: %w", err)
	}
	migrationErr := m.Up()
	sourceErr, databaseErr := m.Close()
	if migrationErr != nil && migrationErr != migrate.ErrNoChange {
		return fmt.Errorf("execute versioned up migrations: %w", migrationErr)
	}
	if sourceErr != nil || databaseErr != nil {
		return fmt.Errorf("close migrations: source=%v database=%v", sourceErr, databaseErr)
	}
	packageDatabase.admin, packageDatabase.template, packageDatabase.dsn = admin, name, dsn
	success = true
	return nil
}

func connect(dsn string) (*gorm.DB, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	pool := stdlib.OpenDB(*config, stdlib.OptionAfterConnect(func(_ context.Context, conn *pgx.Conn) error {
		conn.TypeMap().RegisterType(&pgtype.Type{
			Name: "timestamptz", OID: pgtype.TimestamptzOID,
			Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC},
		})
		return nil
	}))
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool, DSN: dsn}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		pool.Close()
	}
	return db, err
}

func databaseName() string {
	return "reliquary_pgtest_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func dropDatabase(admin *sql.DB, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := admin.ExecContext(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
	return err
}

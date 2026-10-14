package pgtest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/golang-migrate/migrate/v4"

	"gorm.io/driver/postgres"
)

func TestMain(m *testing.M) {
	os.Exit(Run(m))
}

func TestOpenRunsVersionedMigrations(t *testing.T) {
	db := Open(t)
	var version uint
	var dirty bool
	if err := db.Raw("SELECT version, dirty FROM schema_migrations").Row().Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	if dirty || version != LatestMigrationVersion(t) {
		t.Fatalf("expected clean latest migration, got version=%d dirty=%v", version, dirty)
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM pg_extension WHERE extname IN ('vector', 'pg_search', 'pg_trgm')").Scan(&count).Error; err != nil || count != 3 {
		t.Fatalf("required PostgreSQL extensions: count=%d error=%v", count, err)
	}
	if err := db.Exec(`INSERT INTO chunks
		(space_id, library_id, entry_id, content, chunk_index, start_at, end_at)
		VALUES (1, 'pgtest-library', 'pgtest-entry', 'PostgreSQL integration tests', 0, 0, 28)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO embeddings (source_id, source_type, dimension, embedding)
		VALUES ('pgtest-source', 1, 3, '[1,2,3]'::halfvec)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM chunks WHERE seq_id @@@ 'content:PostgreSQL'").Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("pg_search query: count=%d error=%v", count, err)
	}
	var distance float64
	if err := db.Raw("SELECT embedding <-> '[1,2,3]'::halfvec FROM embeddings WHERE source_id = 'pgtest-source'").Scan(&distance).Error; err != nil || distance != 0 {
		t.Fatalf("pgvector query: distance=%v error=%v", distance, err)
	}
}

func TestBaselineUpDownUp(t *testing.T) {
	db := Open(t)
	_, source, _, _ := runtime.Caller(0)
	migrations := filepath.Join(filepath.Dir(source), "..", "..", "..", "migrations", "versioned")
	m, err := migrate.New("file://"+filepath.ToSlash(migrations), db.Dialector.(*postgres.Dialector).DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sourceErr, databaseErr := m.Close(); sourceErr != nil || databaseErr != nil {
			t.Errorf("close migrations: source=%v database=%v", sourceErr, databaseErr)
		}
	})
	if err := m.Down(); err != nil {
		t.Fatalf("baseline down: %v", err)
	}
	var table *string
	if err := db.Raw("SELECT to_regclass('public.chunks')::text").Scan(&table).Error; err != nil || table != nil {
		t.Fatalf("baseline down left chunks: table=%v error=%v", table, err)
	}
	if err := m.Up(); err != nil {
		t.Fatalf("baseline second up: %v", err)
	}
	var count int64
	if err := db.Raw(`SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'embeddings' AND column_name = 'content'`).Scan(&count).Error; err != nil || count != 0 {
		t.Fatalf("embeddings still has content: count=%d error=%v", count, err)
	}
	if err := db.Raw("SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'chunks_search_idx'").Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("chunks BM25 missing after second up: count=%d error=%v", count, err)
	}
}

func TestOpenIsolatesTestDatabases(t *testing.T) {
	first := Open(t)
	second := Open(t)
	var firstName, secondName string
	if err := first.Raw("SELECT current_database()").Scan(&firstName).Error; err != nil {
		t.Fatal(err)
	}
	if err := second.Raw("SELECT current_database()").Scan(&secondName).Error; err != nil {
		t.Fatal(err)
	}
	if firstName == secondName {
		t.Fatal("tests share a database")
	}
	if err := first.Exec("CREATE TABLE pgtest_isolation (id INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := second.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'pgtest_isolation'").Scan(&count).Error; err != nil || count != 0 {
		t.Fatalf("fixture leaked across databases: count=%d error=%v", count, err)
	}
}

func TestOpenFixtureUsesNativePostgresSchema(t *testing.T) {
	db := OpenFixture(t)
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil || schema != "fixture" {
		t.Fatalf("fixture schema: schema=%q error=%v", schema, err)
	}
	if err := db.Exec("CREATE TABLE sql_features (payload JSONB, title TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO sql_features VALUES ('["document"]'::jsonb, 'Postgres')`).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sql_features WHERE payload = ?::jsonb AND title ILIKE ?", `["document"]`, "%POSTGRES%").Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("native PostgreSQL query: count=%d error=%v", count, err)
	}
}

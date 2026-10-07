package pgtest

import (
	"os"
	"testing"
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
	if dirty || version < 98 {
		t.Fatalf("expected complete migrations, got version=%d dirty=%v", version, dirty)
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM pg_extension WHERE extname IN ('vector', 'pg_search', 'pg_trgm')").Scan(&count).Error; err != nil || count != 3 {
		t.Fatalf("required PostgreSQL extensions: count=%d error=%v", count, err)
	}
	if err := db.Exec(`INSERT INTO embeddings (source_id, source_type, content, dimension, embedding)
		VALUES ('pgtest-source', 1, 'PostgreSQL integration tests', 3, '[1,2,3]'::halfvec)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM embeddings WHERE id @@@ 'content:PostgreSQL'").Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("pg_search query: count=%d error=%v", count, err)
	}
	var distance float64
	if err := db.Raw("SELECT embedding <-> '[1,2,3]'::halfvec FROM embeddings WHERE source_id = 'pgtest-source'").Scan(&distance).Error; err != nil || distance != 0 {
		t.Fatalf("pgvector query: distance=%v error=%v", distance, err)
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
	if err := db.Exec(`INSERT INTO sql_features VALUES ('["wiki"]'::jsonb, 'Postgres')`).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sql_features WHERE payload = ?::jsonb AND title ILIKE ?", `["wiki"]`, "%POSTGRES%").Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("native PostgreSQL query: count=%d error=%v", count, err)
	}
}

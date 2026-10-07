package tools

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"reliquary/internal/testutil/pgtest"
)

func TestGrepChunksFindsOlderRegexMatches(t *testing.T) {
	db := pgtest.OpenFixture(t)
	for _, schema := range []string{
		`CREATE TABLE knowledges (id TEXT PRIMARY KEY, title TEXT, deleted_at TIMESTAMPTZ)`,
		`CREATE TABLE chunks (id TEXT PRIMARY KEY, content TEXT, chunk_index INTEGER, knowledge_id TEXT, knowledge_base_id TEXT, chunk_type TEXT, metadata JSONB, created_at TIMESTAMPTZ, is_enabled BOOLEAN, deleted_at TIMESTAMPTZ, tenant_id BIGINT)`,
	} {
		if err := db.Exec(schema).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO knowledges (id,title) VALUES ('doc','ordinary'),('title-doc','QA-CONFLICT title')`).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 501; i++ {
		if err := db.Exec(`INSERT INTO chunks (id,content,chunk_index,knowledge_id,knowledge_base_id,created_at,is_enabled,tenant_id) VALUES (?, 'unrelated', 0, 'doc', 'kb', '2026-09-25', TRUE, 1)`, fmt.Sprintf("new-%03d", i)).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct{ id, content, doc string }{{"body-hit", "QA-CONFLICT body", "doc"}, {"title-hit", "unrelated", "title-doc"}} {
		if err := db.Exec(`INSERT INTO chunks (id,content,chunk_index,knowledge_id,knowledge_base_id,created_at,is_enabled,tenant_id) VALUES (?, ?, 0, ?, 'kb', '2026-09-24', TRUE, 1)`, row.id, row.content, row.doc).Error; err != nil {
			t.Fatal(err)
		}
	}
	tool := NewGrepChunksTool(db, nil)
	hits, err := tool.searchChunks(context.Background(), []string{"QA-CONFLICT"}, []*regexp.Regexp{regexp.MustCompile("(?i)QA-CONFLICT")}, []string{"kb"}, nil, nil, map[string]uint64{"kb": 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].ID != "title-hit" || hits[1].ID != "body-hit" {
		t.Fatalf("expected title and older body match after 501 nonmatches, got %+v", hits)
	}
}

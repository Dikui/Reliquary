package types

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The historical migrations and offline adapters must spell the old schema
// names to read old backups and to undo the naming migration.
var libraryNamingExceptions = map[string]string{
	"internal/maintenance/queue_migration.go":                      "Historical backup projection converts the archived scope across migration 000009.",
	"migrations/versioned/000009_redis_pending_ops.up.sql":         "Converts historical dead-letter scope during migration.",
	"migrations/versioned/000009_redis_pending_ops.down.sql":       "Restores historical dead-letter scope for temporary down tests.",
	"internal/maintenance/format.go":                               "Replica formats 1 through 3 are read and hashed in their original naming vocabulary.",
	"internal/maintenance/schema_compat.go":                        "Offline maintenance temporarily renames old schemas and restores them in the same transaction.",
	"migrations/versioned/000000_init.down.sql":                    "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000000_init.up.sql":                      "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000001_maintenance.down.sql":             "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000001_maintenance.up.sql":               "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000002_space_naming.down.sql":            "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000002_space_naming.up.sql":              "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000003_entry_naming.down.sql":            "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000003_entry_naming.up.sql":              "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000004_ingest_only.down.sql":             "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000004_ingest_only.up.sql":               "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000005_remove_collection.down.sql":       "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000005_remove_collection.up.sql":         "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000006_user_documents_imported.down.sql": "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000006_user_documents_imported.up.sql":   "Frozen history used by offline backup restoration and version-specific fixtures.",
	"migrations/versioned/000007_library_naming.down.sql":          "Naming migration must spell both schema generations for reversible DDL.",
	"migrations/versioned/000007_library_naming.up.sql":            "Naming migration must spell both schema generations for reversible DDL.",
}

// Only these exact compatibility literals are allowed in current Go sources.
// New identifiers and unrelated literals in the same files remain checked.
var libraryNamingAllowedLiterals = map[string][]string{
	"internal/maintenance/queue_migration.go": {`"knowledge_base"`},
	"internal/maintenance/format.go":          {`"knowledge_bases"`, `"knowledge_base_id"`},
	"internal/maintenance/schema_compat.go": {
		`"SELECT to_regclass('public.knowledge_bases') IS NOT NULL"`,
		`"SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='data_sources' AND column_name='knowledge_base_id')"`,
		`"ALTER TABLE public.data_sources RENAME COLUMN knowledge_base_id TO library_id"`,
		`"ALTER TABLE public.data_sources RENAME COLUMN library_id TO knowledge_base_id"`,
	},
}

// Exact English locale lines may be exempted only with a documented reason.
// No collection-label exceptions are currently needed. Knowledge graph,
// knowledge retrieval/search/processing/query and personal knowledge describe
// content or operations, not a Library, so they are outside this copy check.
var libraryEnglishCopyExceptions = map[string]string{}

func TestLibraryNamingGuard(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	retired := regexp.MustCompile(`(?i)knowledgebase|knowledge_base|knowledge-base|knowledgesettings`)
	retiredEnglishCopy := regexp.MustCompile(`(?i)\bknowledge\s+(?:settings|bases?)\b`)
	for _, dir := range []string{"internal", "cmd", "migrations", "frontend/src"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			name := entry.Name()
			if strings.HasSuffix(name, "_test.go") || strings.Contains(name, ".test.") || strings.Contains(name, ".spec.") {
				return nil
			}
			switch filepath.Ext(name) {
			case ".go", ".ts", ".tsx", ".vue", ".js", ".jsx", ".mjs", ".css", ".less", ".scss", ".html", ".sql", ".json", ".yaml", ".yml":
			default:
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if retired.MatchString(rel) {
				t.Errorf("%s: retired library filename", rel)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			// Versions 0 to 6 are frozen, individually enumerated below. Version 7
			// necessarily names both sides of the rename and supports full rollback.
			if reason := libraryNamingExceptions[rel]; reason != "" && strings.HasPrefix(rel, "migrations/") {
				return nil
			}
			for i, line := range strings.Split(string(data), "\n") {
				if libraryNamingExceptions[rel] != "" {
					for _, literal := range libraryNamingAllowedLiterals[rel] {
						line = strings.ReplaceAll(line, literal, "")
					}
				}
				if retired.MatchString(line) {
					t.Errorf("%s:%d: retired library name: %s", rel, i+1, line)
				}
				if strings.HasPrefix(rel, "frontend/src/i18n/locales/zh") && strings.Contains(line, "知识库") {
					t.Errorf("%s:%d: retired collection label", rel, i+1)
				}
				if rel == "frontend/src/i18n/locales/en-US.ts" && retiredEnglishCopy.MatchString(line) && libraryEnglishCopyExceptions[strings.TrimSpace(line)] == "" {
					t.Errorf("%s:%d: retired English collection label: %s", rel, i+1, line)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

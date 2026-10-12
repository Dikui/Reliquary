package maintenance

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// schemaRules matches preservation columns to the installed migration version.
// Offline maintenance may temporarily adapt old names, but must not introduce
// later fields or alter its persisted structure and migration history.
func schemaRules(ctx context.Context, db *gorm.DB) (map[string]Rule, error) {
	var present bool
	if err := db.WithContext(ctx).Raw("SELECT to_regclass('public.schema_migrations') IS NOT NULL").Scan(&present).Error; err != nil {
		return nil, err
	}
	if !present {
		return Rules, nil
	}
	var migration struct {
		Version int64
		Dirty   bool
	}
	if err := db.WithContext(ctx).Table("public.schema_migrations").Select("version,dirty").Take(&migration).Error; err != nil {
		return nil, err
	}
	if migration.Dirty {
		return nil, fmt.Errorf("maintenance requires a clean migration version")
	}
	if migration.Version >= 4 {
		return Rules, nil
	}
	legacy := make(map[string]Rule, len(Rules))
	for table, rule := range Rules {
		legacy[table] = rule
	}
	delete(legacy, "ingest_assets")
	delete(legacy, "ingest_discards")
	remove := func(group string, names ...string) string {
		omit := map[string]bool{}
		for _, name := range names {
			omit[name] = true
		}
		kept := []string{}
		for _, name := range strings.Fields(group) {
			if !omit[name] {
				kept = append(kept, name)
			}
		}
		return strings.Join(kept, " ")
	}
	entries := legacy["entries"]
	entries.Header = remove(entries.Header, "acquisition")
	entries.Sidecar = remove(entries.Sidecar, "incomplete", "external_completion_attempted", "ingest_api_key_id", "ingest_api_key_name", "last_ingest_api_key_id", "last_ingest_api_key_name", "missing_materials", "ingest_attachments", "content_type_hint", "related_article_id")
	legacy["entries"] = entries
	logs := legacy["operation_logs"]
	logs.Sidecar = remove(logs.Sidecar, "api_key_id", "api_key_name")
	legacy["operation_logs"] = logs
	return legacy, nil
}

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
	if migration.Version >= 5 {
		return Rules, nil
	}
	legacy := make(map[string]Rule, len(Rules))
	for table, rule := range Rules {
		legacy[table] = rule
	}
	for table, rule := range collectionRules {
		legacy[table] = rule
	}
	if migration.Version == 4 {
		return legacy, nil
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

// These tables only belong to installations before collection was removed.
// They remain explicit for exporting and restoring an older complete backup.
var collectionRules = map[string]Rule{
	"data_sources": {Configuration: "id space_id knowledge_base_id name type default_content_type config sync_schedule sync_mode status sync_deletions last_sync_at last_sync_cursor last_sync_result error_message sync_log_retention_days created_at updated_at deleted_at"},
	"sync_logs":    {Sidecar: "id data_source_id space_id status started_at finished_at items_total items_created items_updated items_deleted items_skipped items_failed error_message result created_at updated_at"},
}

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
func schemaRules(ctx context.Context, db *gorm.DB) (result map[string]Rule, resultErr error) {
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
	defer func() {
		if resultErr != nil {
			return
		}
		copy := make(map[string]Rule, len(result))
		for table, rule := range result {
			copy[table] = rule
		}
		for table, registration := range PersistentTables {
			if migration.Version < registration.Since {
				delete(copy, table)
			}
		}
		for table, registration := range RegeneratedTables {
			if migration.Version < registration.Since {
				delete(copy, table)
			}
		}
		retired := retirementAt(migration.Version)
		for _, table := range retired.tables {
			delete(copy, table)
		}
		for table, columns := range retired.columns {
			if rule, ok := copy[table]; ok {
				for _, column := range columns {
					rule.Header = strings.ReplaceAll(" "+rule.Header+" ", " "+column+" ", " ")
					rule.Sidecar = strings.ReplaceAll(" "+rule.Sidecar+" ", " "+column+" ", " ")
					rule.Configuration = strings.ReplaceAll(" "+rule.Configuration+" ", " "+column+" ", " ")
					rule.Regenerated = strings.ReplaceAll(" "+rule.Regenerated+" ", " "+column+" ", " ")
				}
				copy[table] = rule
			}
		}
		if migration.Version < 19 {
			rule := copy["graph_pending_sync"]
			rule.Regenerated = strings.TrimSpace(strings.ReplaceAll(rule.Regenerated, "completed_at", ""))
			if rule.Regenerated != "" {
				copy["graph_pending_sync"] = rule
			}
		}
		if migration.Version < 21 {
			delete(copy, "search_records")
		}
		if migration.Version < 22 {
			delete(copy, "model_calls")
		}
		if migration.Version < 29 {
			rule, ok := copy["model_calls"]
			if ok {
				rule.Sidecar = strings.TrimSpace(strings.ReplaceAll(rule.Sidecar, " text_input_tokens image_input_tokens image_input_price", ""))
				copy["model_calls"] = rule
			}
		}
		if migration.Version < 24 {
			delete(copy, "model_call_contents")
			delete(copy, "model_call_content_deletions")
			rule := copy["model_calls"]
			rule.Sidecar = strings.TrimSpace(strings.ReplaceAll(rule.Sidecar, " processing_attempt span_id", ""))
			if rule.Sidecar != "" {
				copy["model_calls"] = rule
			}
		}
		if migration.Version < 23 {
			for table, rule := range copy {
				copy[table] = legacyEntryRule(table, rule)
			}
		}
		result = copy
	}()
	if migration.Dirty {
		return nil, fmt.Errorf("maintenance requires a clean migration version")
	}
	if migration.Version >= 15 {
		return Rules, nil
	}
	current := make(map[string]Rule, len(Rules))
	for table, rule := range Rules {
		current[table] = rule
	}
	if migration.Version < 15 {
		delete(current, "follow_feed_library_states")
		topic := current["topics"]
		topic.Sidecar = strings.ReplaceAll(topic.Sidecar, "library_ids ", "")
		current["topics"] = topic
	}
	if migration.Version >= 14 {
		return current, nil
	}
	for table, registration := range RegeneratedTables {
		if migration.Version < registration.Since {
			delete(current, table)
		}
	}
	chunks := current["chunks"]
	chunks.Regenerated = strings.TrimSpace(strings.ReplaceAll(chunks.Regenerated, "source_locations", ""))
	current["chunks"] = chunks
	runs := current["embedding_rebuild_runs"]
	runs.Sidecar = strings.TrimSpace(strings.ReplaceAll(runs.Sidecar, "candidate switched_at", ""))
	current["embedding_rebuild_runs"] = runs
	if migration.Version >= 12 {
		return current, nil
	}
	discard := current["ingest_discards"]
	discard.Sidecar = "space_id normalized_url article_id discarded_at reason"
	current["ingest_discards"] = discard
	if migration.Version >= 11 {
		return current, nil
	}
	entry := current["entries"]
	for _, field := range []string{"source_ref", "source_body_hash", "identifier", "graph"} {
		entry.Sidecar = strings.ReplaceAll(" "+entry.Sidecar+" ", " "+field+" ", " ")
	}
	entry.Sidecar = strings.TrimSpace(entry.Sidecar)
	current["entries"] = entry
	if migration.Version >= 10 {
		return current, nil
	}
	delete(current, "library_content_types")
	delete(current, "library_relation_types")
	library := current["libraries"]
	for _, field := range []string{"system_role", "template", "directory", "review_criteria", "source_evidence_rule", "feed_enabled"} {
		library.Configuration = strings.ReplaceAll(" "+library.Configuration+" ", " "+field+" ", " ")
	}
	library.Configuration = strings.TrimSpace(library.Configuration)
	current["libraries"] = library
	space := current["spaces"]
	space.Configuration = strings.TrimSpace(strings.ReplaceAll(" "+space.Configuration+" ", " embedding_model_id ", " "))
	current["spaces"] = space
	if migration.Version >= 9 {
		return current, nil
	}
	delete(current, "task_pending_ops")
	current["task_jobs"] = legacyQueueRule
	if migration.Version >= 8 {
		return current, nil
	}
	legacy := make(map[string]Rule, len(Rules))
	for table, rule := range current {
		legacy[table] = rule
	}
	entriesBefore := legacy["entries"]
	entriesBefore.Sidecar += " content_type_uncertain processing_review_status processing_review_content_type processing_review_reason processing_mode vault_original_path"
	if migration.Version >= 4 {
		entriesBefore.Sidecar += " ingest_attachments"
	}
	legacy["entries"] = entriesBefore
	spaces := legacy["spaces"]
	spaces.Configuration += " parser_engine_config"
	legacy["spaces"] = spaces
	if migration.Version >= 5 {
		return legacy, nil
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
	entries.Sidecar = remove(entries.Sidecar, "incomplete", "external_completion_attempted", "ingest_api_key_id", "ingest_api_key_name", "last_ingest_api_key_id", "last_ingest_api_key_name", "missing_materials", "ingest_attachments", "content_type_hint", "related_entry_id")
	legacy["entries"] = entries
	logs := legacy["operation_logs"]
	logs.Sidecar = remove(logs.Sidecar, "api_key_id", "api_key_name")
	legacy["operation_logs"] = logs
	return legacy, nil
}

// These tables only belong to installations before collection was removed.
// They remain explicit for exporting and restoring an older complete backup.
var collectionRules = map[string]Rule{
	"data_sources": {Configuration: "id space_id library_id name type default_content_type config sync_schedule sync_mode status sync_deletions last_sync_at last_sync_cursor last_sync_result error_message sync_log_retention_days created_at updated_at deleted_at"},
	"sync_logs":    {Sidecar: "id data_source_id space_id status started_at finished_at items_total items_created items_updated items_deleted items_skipped items_failed error_message result created_at updated_at"},
}

var legacyQueueRule = Rule{Sidecar: "id task_type queue payload headers state max_retry retried fail_count timeout_ns retention_ns deadline group_name created_at updated_at process_at started_at completed_at last_failed_at last_error lease_until claim_token worker_pool cancel_requested"}

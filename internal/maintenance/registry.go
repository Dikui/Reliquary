package maintenance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PersistentTable is the single registration for new portable SQL tables.
// Existing tables retain their established paths and column classifications.
// SQL migrations remain responsible for constraints and schema changes.
type PersistentTable struct {
	Columns Rule     `json:"columns"`
	File    string   `json:"file"`
	After   []string `json:"after"`
	Since   int64    `json:"since_schema_version"`
}

const RegistryVersion = 1

var PersistentTables = map[string]PersistentTable{
	"story_bridge_evidence": {
		Columns: Rule{Sidecar: "decision_id space_id source_fact_id source_created_at"},
		File:    "data/story_bridge_evidence.json", After: []string{"grouping_decisions", "facts"}, Since: 44,
	},
	"story_links": {
		Columns: Rule{Sidecar: "space_id story_id other_story_id"},
		File:    "data/story_links.json", After: []string{"stories"}, Since: 44,
	},
	"grouping_decisions": {
		Columns: Rule{Sidecar: "id space_id entry_id candidate_fact_id similarity model_id result confidence " +
			"review_result review_confidence applied_result reason created_at"},
		File: "data/grouping_decisions.json", After: []string{"entries", "facts"}, Since: 41,
	},
	"entity_candidates": {
		Columns: Rule{Sidecar: "id space_id name normalized_name entity_type status entity_id created_at updated_at"},
		File:    "data/entity_candidates.json", After: []string{"spaces", "entities"}, Since: 40,
	},
	"entity_candidate_occurrences": {
		Columns: Rule{Sidecar: "space_id candidate_id fact_id endpoint created_at"},
		File:    "data/entity_candidate_occurrences.json", After: []string{"entity_candidates", "facts"}, Since: 40,
	},
	"stories": {
		Columns: Rule{Sidecar: "id space_id title first_fact_at last_fact_at manual_corrected merged_into_id"},
		File:    "data/stories.json", After: []string{"spaces"}, Since: 39,
	},
	"facts": {
		Columns: Rule{Sidecar: "id space_id story_id title subject action object conditions occurred_at created_at " +
			"subject_entity_id object_entity_id manual_corrected merged_into_id recall_date"},
		File: "data/facts.json", After: []string{"stories", "entities"}, Since: 39,
	},
	"entry_facts": {
		Columns: Rule{Sidecar: "space_id entry_id fact_id role quote manual created_at"},
		File:    "data/entry_facts.json", After: []string{"entries", "facts"}, Since: 39,
	},
	"domains": {
		Columns: Rule{Configuration: "id space_id name normalized_name definition created_at updated_at"},
		File:    "config/domains.json",
		After:   []string{"spaces"},
		Since:   37,
	},
	"entry_domains": {
		Columns: Rule{Sidecar: "space_id entry_id domain_id created_at"},
		File:    "data/entry_domains.json",
		After:   []string{"entries", "domains"},
		Since:   37,
	},
	"domain_seed_imports": {
		Columns: Rule{Sidecar: "space_id imported_at"},
		File:    "data/domain_seed_imports.json",
		After:   []string{"spaces", "domains"},
		Since:   37,
	},
	"entity_seed_imports": {
		Columns: Rule{Sidecar: "space_id imported_at"},
		File:    "data/entity_seed_imports.json",
		After: []string{
			"spaces",
			"entities",
		},
		Since: 36,
	},

	"content_check_settings": {
		Columns: Rule{Configuration: "space_id enabled model_id weekday time next_run_at scan_after"},
		File:    "config/content_check_settings.json",
		After: []string{
			"spaces",
			"models",
		},
		Since: 32,
	},

	"content_check_runs": {
		Columns: Rule{Sidecar: "id space_id model_id previous_id status window_from window_to targets " +
			"completed results error created_at finished_at"},

		File:  "data/content_check_runs.json",
		After: []string{"content_check_settings"},
		Since: 32,
	},

	"relations": {
		Columns: Rule{Sidecar: "id space_id library_id subject_id relation_type object_id version " +
			"created_at updated_at"},
		File: "data/relations.json",
		After: []string{
			"spaces",
			"libraries",
			"entities",
			"library_relation_types",
		},
		Since: 18,
	},

	"relation_sources":       {Columns: Rule{Sidecar: "relation_id entry_id evidence run_id body_hash"}, File: "data/relation_sources.json", After: []string{"relations", "entries"}, Since: 18},
	"relation_suppressions":  {Columns: Rule{Sidecar: "space_id library_id subject_id relation_type object_id actor created_at"}, File: "data/relation_suppressions.json", After: []string{"spaces", "libraries", "entities", "library_relation_types"}, Since: 18},
	"relation_type_merges":   {Columns: Rule{Configuration: "library_id from_type to_type created_at"}, File: "data/relation_type_merges.json", After: []string{"library_relation_types"}, Since: 18},
	"evaluation_sets":        {Columns: Rule{Sidecar: "id space_id name description created_at updated_at"}, File: "data/evaluation_sets.json", After: []string{"spaces"}, Since: 21},
	"evaluation_questions":   {Columns: Rule{Sidecar: "id space_id set_id method request source status source_record_id suggested_results answer_deleted revision created_at updated_at"}, File: "data/evaluation_questions.json", After: []string{"evaluation_sets"}, Since: 21},
	"evaluation_answers":     {Columns: Rule{Sidecar: "question_id entry_id grade"}, File: "data/evaluation_answers.json", After: []string{"evaluation_questions"}, Since: 21},
	"evaluation_runs":        {Columns: Rule{Sidecar: "id space_id set_id set_snapshot options snapshot status summary error created_at finished_at"}, File: "data/evaluation_runs.json", After: []string{"spaces"}, Since: 21},
	"evaluation_run_results": {Columns: Rule{Sidecar: "run_id question_id question snapshot results metrics hits error"}, File: "data/evaluation_run_results.json", After: []string{"evaluation_runs"}, Since: 21},
	"entry_processing_outputs": {
		Columns: Rule{Sidecar: "entry_id space_id library_id run_id body_hash content_type relations images updated_at"},
		File:    "data/entry_processing_outputs.json", After: []string{"spaces", "libraries", "entries"}, Since: 17,
	},
	"follow_feed_library_states": {
		Columns: Rule{Sidecar: "space_id library_id last_viewed_at"},
		File:    "data/follow_feed_library_states.json", After: []string{"libraries"}, Since: 15,
	},
	"entry_segment_runs": {
		Columns: Rule{Sidecar: "id space_id entry_id phase plan_hash body_hash plan result status created_at updated_at"},
		File:    "data/entry_segment_runs.json", After: []string{"spaces", "entries"}, Since: 16,
	},
	"embedding_rebuild_runs": {
		Columns: Rule{Sidecar: "id space_id status model_id model_name dimension profile_hash total_count completed_count failed_count error attempt_id lease_until created_at updated_at started_at finished_at candidate switched_at"},
		File:    "data/embedding_rebuild_runs.json", After: []string{"spaces", "models"}, Since: 0,
	},
	"embedding_rebuild_items": {
		Columns: Rule{Sidecar: "id run_id kind source_id attachment_path status attempts error created_at updated_at"},
		File:    "data/embedding_rebuild_items.json", After: []string{"embedding_rebuild_runs"}, Since: 0,
	},
	"libraries": {
		Columns: Rule{Configuration: "system_role template directory review_criteria source_evidence_rule feed_enabled model_tiers id name description space_id chunking_config embedding_model_id summary_model_id created_at updated_at deleted_at type indexing_strategy creator_id"},
		File:    "config/libraries.json", After: []string{"spaces", "users"}, Since: 0,
	},
	"library_content_types": {
		Columns: Rule{Configuration: "library_id name position enabled extract_graph"},
		File:    "data/library_content_types.json", After: []string{"libraries"}, Since: 10,
	},
	"library_relation_types": {
		Columns: Rule{Configuration: "library_id name position enabled"},
		File:    "data/library_relation_types.json", After: []string{"libraries"}, Since: 10,
	},
}

// BackupOnlyTables must be explicitly classified when introduced. They are
// preserved by PostgreSQL backups, never by portable replicas.
var BackupOnlyTables = map[string]Rule{
	"entry_batch_items":            {Sidecar: "batch_id entry_id space_id title library_id status message warning task_id attempt created_at updated_at"},
	"model_call_content_deletions": {Sidecar: "space_id entry_id"},
	"model_call_contents":          {Sidecar: "id request_gzip response_gzip request_bytes response_bytes response_type http_status complete"},
	"search_records":               {Sidecar: "id space_id created_at source method query libraries request results duration_ms fallback_fulltext reranked evaluation_run_id error"},
	"model_calls":                  {Sidecar: "id space_id requested_at model_id model_name provider purpose entry_id input_tokens output_tokens input_price output_price currency cost cost_status duration_ms success error_category attempt retry_number processing_attempt span_id text_input_tokens image_input_tokens image_input_price"},
}

// Migration 000028 adds an empty backup-only table. Project only that default;
// unexpected rows and changes to snapshots from version 28 onward still fail.
func projectEntryBatchManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 28 || actual.SchemaVersion < 28 {
		return expected, actual
	}
	if _, present := expected.Tables["entry_batch_items"]; present {
		return expected, actual
	}
	if _, present := actual.Tables["entry_batch_items"]; !present {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables)+1)
	for name, table := range expected.Tables {
		left.Tables[name] = table
	}
	columns := []string{}
	for column := range BackupOnlyTables["entry_batch_items"].Columns() {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	left.Tables["entry_batch_items"] = TableManifest{Columns: columns, SHA256: sum([]byte("[]"))}
	return &left, actual
}

// Version 32 adds empty self-check tables. Preserve strict row and file checks
// while allowing only those empty defaults when comparing an older replica.
func projectContentCheckManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 32 || actual.SchemaVersion < 32 {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables)+2)
	left.Files = make(map[string][]File, len(expected.Files))
	for name, table := range expected.Tables {
		left.Tables[name] = table
	}
	for group, files := range expected.Files {
		left.Files[group] = append([]File{}, files...)
	}
	for _, name := range []string{"content_check_settings", "content_check_runs"} {
		if _, present := left.Tables[name]; present {
			continue
		}
		if _, present := actual.Tables[name]; !present {
			continue
		}
		columns := []string{}
		for column := range PersistentTables[name].Columns.Columns() {
			columns = append(columns, column)
		}
		sort.Strings(columns)
		left.Tables[name] = TableManifest{Columns: columns, SHA256: sum([]byte("[]")), Data: []map[string]any{}}
		for _, file := range actual.Files["vault"] {
			if file.Path == ".reliquary/"+PersistentTables[name].File {
				left.Files["vault"] = append(left.Files["vault"], File{Path: file.Path, SHA256: sum([]byte("[]\n")), Size: 3})
			}
		}
	}
	return &left, actual
}

// RegeneratedTable registers derived tables without putting their rows in a
// portable replica. Their names still participate in strict schema verification.
type RegeneratedTable struct {
	Columns Rule
	Since   int64
}

var RegeneratedTables = map[string]RegeneratedTable{
	"fact_embeddings": {
		Columns: Rule{Regenerated: "space_id fact_id model_id dimension content_hash embedding"}, Since: 41,
	},
	"graph_pending_sync":        {Columns: Rule{Regenerated: "id space_id entity_id relation_id relation_version deleted created_at completed_at"}, Since: 18},
	"embedding_rebuild_vectors": {Columns: Rule{Regenerated: "run_id kind source_id attachment_path entry_id library_id model_id dimension embedding content_hash"}, Since: 14},
	"embedding_vector_archives": {Columns: Rule{Regenerated: "run_id space_id payload created_at"}, Since: 14},
}

func init() {
	for table, registration := range RegeneratedTables {
		Rules[table] = registration.Columns
	}
	for table, registration := range PersistentTables {
		Rules[table] = registration.Columns
	}
	for table, columns := range BackupOnlyTables {
		Rules[table] = columns
	}
}

func registeredPath(table string) (string, bool) {
	registration, ok := PersistentTables[table]
	return registration.File, ok
}

func registryBytes(snapshot *Snapshot) ([]byte, error) { return registryBytesWithRuns(snapshot, true) }

func registryBytesWithRuns(snapshot *Snapshot, includeRuns bool) ([]byte, error) {
	registrations := map[string]PersistentTable{}
	for table, registration := range PersistentTables {
		if !includeRuns && (table == "embedding_rebuild_runs" || table == "embedding_rebuild_items") {
			continue
		}
		if _, ok := snapshot.Tables[table]; ok {
			registrations[table] = registration
		}
	}
	data, err := json.MarshalIndent(struct {
		Version int                        `json:"format_version"`
		Tables  map[string]PersistentTable `json:"tables"`
	}{RegistryVersion, registrations}, "", "  ")
	return append(data, '\n'), err
}

// registeredImportOrder resolves new tables automatically around the existing
// dependency order. Missing dependencies and cycles fail before any SQL writes.
func registeredImportOrder(base []string, tables map[string][]map[string]any) ([]string, error) {
	pending := map[string]PersistentTable{}
	for table, registration := range PersistentTables {
		if _, ok := tables[table]; ok {
			pending[table] = registration
		}
	}
	seen := map[string]bool{}
	result := []string{}
	drain := func() {
		for {
			ready := []string{}
			for table, registration := range pending {
				ok := true
				for _, dependency := range registration.After {
					if !seen[dependency] {
						ok = false
					}
				}
				if ok {
					ready = append(ready, table)
				}
			}
			if len(ready) == 0 {
				return
			}
			sort.Strings(ready)
			for _, table := range ready {
				result = append(result, table)
				seen[table] = true
				delete(pending, table)
			}
		}
	}
	drain()
	for _, table := range base {
		if _, registered := PersistentTables[table]; registered {
			continue
		}
		result = append(result, table)
		seen[table] = true
		drain()
	}
	if len(pending) > 0 {
		names := []string{}
		for table := range pending {
			names = append(names, table)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("persistent table dependency missing or cyclic: %s", strings.Join(names, ", "))
	}
	return result, nil
}

// RequireCurrentVaultReplica keeps the public file rebuild separate from the
// historical SQL backup readers, whose version mappings remain available.
func RequireCurrentVaultReplica(root string) error {
	data, err := os.ReadFile(filepath.Join(root, ".reliquary", "manifest.json"))
	if err != nil {
		return err
	}
	var manifest ReplicaManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if manifest.RegistryVersion != RegistryVersion {
		return fmt.Errorf("legacy file-library replica has no supported registry; restore with its matching release and re-ingest into the current per-library format")
	}
	snapshot, err := Load(root)
	if err != nil {
		return err
	}
	directories := map[string]string{}
	for _, library := range snapshot.Tables["libraries"] {
		directory, _ := library["directory"].(string)
		if directory == "" {
			return fmt.Errorf("legacy file-library replica has no library directories; use its matching release")
		}
		directories[fmt.Sprint(library["id"])] = directory
	}
	for _, entry := range snapshot.Tables["entries"] {
		path, _ := entry["vault_path"].(string)
		if path != "" && filepath.Dir(filepath.FromSlash(path)) != directories[fmt.Sprint(entry["library_id"])] {
			return fmt.Errorf("legacy file-library replica path %s does not match its library directory", path)
		}
	}
	return nil
}

func registryManifestFileMatches(file File, manifest *Manifest) bool {
	snapshot := &Snapshot{Tables: map[string][]map[string]any{}}
	for table := range manifest.Tables {
		snapshot.Tables[table] = nil
	}
	data, err := registryBytes(snapshot)
	if err == nil && file.SHA256 == sum(data) && file.Size == int64(len(data)) {
		return true
	}
	// Registries written before the candidate fields had only library entries.
	for _, column := range manifest.Tables["embedding_rebuild_runs"].Columns {
		if column == "candidate" {
			return false
		}
	}
	data, err = registryBytesWithRuns(snapshot, false)
	return err == nil && file.SHA256 == sum(data) && file.Size == int64(len(data))
}

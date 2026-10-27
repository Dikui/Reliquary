package maintenance

import (
	"encoding/json"
	"path"
	"slices"
	"strings"
)

// Frozen schemas remain readable for migration checks and older backups.
var retiredTables = []string{
	"relations",
	"relation_sources",
	"relation_suppressions",
	"relation_type_merges",
	"library_relation_types",
	"graph_pending_sync",
}

var retiredColumns = map[string]string{
	"entries":                  "graph",
	"library_content_types":    "extract_graph",
	"entry_processing_outputs": "relations",
}

type retirement struct {
	tables        []string
	columns       map[string][]string
	tasks         []string
	resetEntities bool
}

func retirementAt(version int64) retirement {
	r := retirement{columns: map[string][]string{}}
	if version >= 33 {
		r.tables = append(r.tables, retiredTables...)
		for table, column := range retiredColumns {
			r.columns[table] = []string{column}
		}
		r.tasks = append(r.tasks, "graph:sync", "graph:check", "graph:rebuild")
	}
	if version >= 34 {
		r.tables = append(
			r.tables,
			"library_content_types",
			"topics",
			"entry_topics",
			"topic_backfill_jobs",
			"topic_backfill_batches",
			"content_check_settings",
			"content_check_runs",
		)
		r.columns["entries"] = append(
			r.columns["entries"],
			"content_type",
			"content_type_corrected",
			"content_type_hint",
		)
		r.columns["entry_processing_outputs"] = append(r.columns["entry_processing_outputs"], "content_type")
		r.columns["follows"] = []string{"topic_id"}
		r.tasks = append(r.tasks, "topic:backfill", "content:check")
	}
	if version >= 36 {
		r.tables = append(r.tables, "entity_merge_suggestions", "entity_embedding_rebuild_runs")
		r.columns["entities"] = []string{
			"description",
			"name_embedding",
			"embedding_model_id",
			"embedding_dimension",
			"embedding_needs_rebuild",
		}
		r.tasks = append(r.tasks, "entity:name_embedding_rebuild")
	}

	if version >= 45 {
		r.tables = append(r.tables, "follow_feed_states", "follow_feed_library_states")
	}
	if version >= 48 {
		r.tables = append(r.tables, "auth_tokens")
		r.columns["users"] = []string{"password_hash"}
	}
	if version >= 49 {
		for _, table := range []string{"entities", "entity_aliases", "entity_candidates"} {
			r.columns[table] = append(r.columns[table], "entity_type")
		}
	}
	if version >= 50 {
		r.columns["chunks"] = append(r.columns["chunks"],
			"search_title", "metadata", "flags", "relation_chunks", "indirect_relation_chunks")
		r.columns["embeddings"] = append(r.columns["embeddings"], "is_enabled")
	}
	return r
}

func (r retirement) dropsRow(table string, row map[string]any) bool {
	if r.resetEntities {
		switch table {
		case "entities", "entity_aliases", "entry_entities", "entity_merge_records":
			return true
		case "follows":
			if row["target_type"] == "entity" {
				return true
			}
		case "entry_segment_runs":
			if row["phase"] == "processing" {
				return true
			}
		case "embedding_rebuild_items", "embedding_rebuild_vectors":
			if row["kind"] == "entity" {
				return true
			}
		}
	}
	return table == "task_pending_ops" && slices.Contains(r.tasks, stringValue(row["task_type"])) ||
		table == "follows" && len(r.columns["follows"]) > 0 && row["target_type"] == "topic"
}

func (r retirement) projectRow(table string, row map[string]any) {
	for _, column := range r.columns[table] {
		delete(row, column)
	}
	if r.resetEntities && table == "libraries" {
		if tiers, ok := row["model_tiers"].(map[string]any); ok {
			delete(tiers, "entity_resolution")
		}
	}
}

func projectRetiredSnapshot(snapshot *Snapshot, targetVersion int64) {
	if targetVersion >= 36 {
		projectEntryDefaults(snapshot, map[string]any{"entities_manual": false})
		if _, ok := snapshot.Tables["entity_seed_imports"]; !ok {
			snapshot.Tables["entity_seed_imports"] = []map[string]any{}
		}
	}

	r := retirementAt(targetVersion)
	r.resetEntities = snapshot.schemaVersion < 36 && targetVersion >= 36
	for _, table := range r.tables {
		delete(snapshot.Tables, table)
		delete(snapshot.JSONNulls, table)
	}
	for table, rows := range snapshot.Tables {
		kept := slices.DeleteFunc(rows, func(row map[string]any) bool {
			if r.dropsRow(table, row) {
				delete(snapshot.JSONNulls[table], rowIdentity(row))
				return true
			}
			r.projectRow(table, row)
			return false
		})
		snapshot.Tables[table] = kept
		if len(snapshot.JSONNulls[table]) == 0 {
			delete(snapshot.JSONNulls, table)
		}
	}
}

func projectRetiredManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= actual.SchemaVersion || actual.SchemaVersion < 33 {
		return expected, actual
	}
	r := retirementAt(actual.SchemaVersion)
	r.resetEntities = expected.SchemaVersion < 36 && actual.SchemaVersion >= 36
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	left.Files = make(map[string][]File, len(expected.Files))
	changed := map[string][2][]byte{}
	for table, item := range expected.Tables {
		if slices.Contains(r.tables, table) {
			continue
		}
		columns := r.columns[table]
		needsProjection := r.resetEntities && table == "libraries" || slices.ContainsFunc(
			columns,
			func(column string) bool { return slices.Contains(item.Columns, column) },
		)
		for _, row := range item.Data {
			for _, column := range columns {
				if _, present := row[column]; present {
					needsProjection = true
				}
			}
			if r.dropsRow(table, row) {
				needsProjection = true
			}
		}
		if needsProjection {
			original, _ := json.Marshal(canonicalRows(table, item.Data))
			if item.SHA256 != sum(original) {
				return expected, actual
			}
			before, _ := json.MarshalIndent(item.Data, "", "  ")
			data, _ := json.Marshal(item.Data)
			var rows []map[string]any
			decoder := json.NewDecoder(strings.NewReader(string(data)))
			decoder.UseNumber()
			if decoder.Decode(&rows) != nil {
				return expected, actual
			}
			nulls := make(map[string][]string, len(item.JSONNulls))
			for id, columns := range item.JSONNulls {
				nulls[id] = append([]string{}, columns...)
			}
			for _, row := range rows {
				r.projectRow(table, row)
				if r.dropsRow(table, row) {
					delete(nulls, rowIdentity(row))
				}
			}
			rows = slices.DeleteFunc(rows, func(row map[string]any) bool { return r.dropsRow(table, row) })
			if len(nulls) == 0 {
				nulls = nil
			}
			item.JSONNulls = nulls
			item.Rows = int64(len(rows))
			item.Data = rows
			item.Columns = slices.DeleteFunc(
				append([]string{}, item.Columns...),
				func(value string) bool { return slices.Contains(columns, value) },
			)
			encoded, _ := json.Marshal(canonicalRows(table, rows))
			item.SHA256 = sum(encoded)
			after, _ := json.MarshalIndent(rows, "", "  ")
			changed[table] = [2][]byte{append(before, '\n'), append(after, '\n')}
		}
		left.Tables[table] = item
	}
	for group, files := range expected.Files {
		for _, file := range files {
			table := strings.TrimSuffix(path.Base(file.Path), ".json")
			if strings.HasPrefix(file.Path, ".reliquary/") {
				if slices.Contains(r.tables, table) {
					continue
				}
				if pair, ok := changed[table]; ok && file.SHA256 == sum(pair[0]) && file.Size == int64(len(pair[0])) {
					file.SHA256 = sum(pair[1])
					file.Size = int64(len(pair[1]))
				}
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	return &left, actual
}

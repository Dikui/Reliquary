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
	tables  []string
	columns map[string][]string
	tasks   []string
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
	return r
}

func (r retirement) dropsRow(table string, row map[string]any) bool {
	return table == "task_pending_ops" && slices.Contains(r.tasks, stringValue(row["task_type"])) ||
		table == "follows" && len(r.columns["follows"]) > 0 && row["target_type"] == "topic"
}

func projectRetiredSnapshot(snapshot *Snapshot, targetVersion int64) {
	r := retirementAt(targetVersion)
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
			for _, column := range r.columns[table] {
				delete(row, column)
			}
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
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	left.Files = make(map[string][]File, len(expected.Files))
	changed := map[string][2][]byte{}
	for table, item := range expected.Tables {
		if slices.Contains(r.tables, table) {
			continue
		}
		columns := r.columns[table]
		needsProjection := slices.ContainsFunc(
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
				for _, column := range columns {
					delete(row, column)
				}
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

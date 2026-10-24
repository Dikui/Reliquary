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

func retiredTask(row map[string]any) bool {
	return slices.Contains([]string{"graph:sync", "graph:check", "graph:rebuild"}, stringValue(row["task_type"]))
}

func projectRetiredSnapshot(snapshot *Snapshot, targetVersion int64) {
	if targetVersion < 33 {
		return
	}
	for _, table := range retiredTables {
		delete(snapshot.Tables, table)
		delete(snapshot.JSONNulls, table)
	}
	for _, row := range snapshot.Tables["task_pending_ops"] {
		if retiredTask(row) {
			delete(snapshot.JSONNulls["task_pending_ops"], rowIdentity(row))
		}
	}
	if len(snapshot.JSONNulls["task_pending_ops"]) == 0 {
		delete(snapshot.JSONNulls, "task_pending_ops")
	}
	snapshot.Tables["task_pending_ops"] = slices.DeleteFunc(snapshot.Tables["task_pending_ops"], retiredTask)
	for table, column := range retiredColumns {
		for _, row := range snapshot.Tables[table] {
			delete(row, column)
		}
	}
}

func projectRetiredManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 33 || actual.SchemaVersion < 33 {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	left.Files = make(map[string][]File, len(expected.Files))
	changed := map[string][2][]byte{}
	for table, item := range expected.Tables {
		if slices.Contains(retiredTables, table) {
			continue
		}
		column, ok := retiredColumns[table]
		needsProjection := ok && slices.Contains(item.Columns, column)
		for _, row := range item.Data {
			if _, present := row[column]; ok && present {
				needsProjection = true
			}
			if table == "task_pending_ops" && retiredTask(row) {
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
				if ok {
					delete(row, column)
				}
				if table == "task_pending_ops" && retiredTask(row) {
					delete(nulls, rowIdentity(row))
				}
			}
			if table == "task_pending_ops" {
				rows = slices.DeleteFunc(rows, retiredTask)
			}
			if len(nulls) == 0 {
				nulls = nil
			}
			item.JSONNulls = nulls
			item.Rows = int64(len(rows))
			item.Data = rows
			item.Columns = slices.DeleteFunc(
				append([]string{}, item.Columns...),
				func(value string) bool { return value == column },
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
				if slices.Contains(retiredTables, table) {
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

package maintenance

import (
	"encoding/json"
	"strings"
)

// Older safe replicas omit columns that did not exist yet. Restoration may
// introduce only these exact migration defaults; existing values remain exact.
var entryIngestDefaults = map[string]any{
	"acquisition": "", "incomplete": false, "external_completion_attempted": false,
	"ingest_api_key_id": json.Number("0"), "ingest_api_key_name": "", "last_ingest_api_key_id": json.Number("0"), "last_ingest_api_key_name": "",
	"missing_materials": []any{}, "ingest_attachments": []any{}, "content_type_hint": "", "related_entry_id": "",
}
var logIngestDefaults = map[string]any{"api_key_id": json.Number("0"), "api_key_name": ""}
var initialMaintenanceData = map[string]any{
	"backup_at": nil, "backup_status": "never", "backup_path": "", "backup_size_bytes": json.Number("0"), "backup_error": "",
	"drill_at": nil, "drill_status": "never", "drill_error": "", "backup_schedule": "0 3 * * 0", "drill_schedule": "0 4 1 * *",
	"schedule_timezone": "Asia/Shanghai", "backup_directory": "../reliquary-backups", "automatic_enabled": true,
}

func addLogDefaults(row map[string]any) {
	for column, value := range logIngestDefaults {
		if _, found := row[column]; !found {
			row[column] = value
		}
	}
}
func addSnapshotMigrationDefaults(snapshot *Snapshot) {
	if _, current := snapshot.Tables["ingest_assets"]; current {
		return
	}
	snapshot.Tables["ingest_assets"] = []map[string]any{}
	snapshot.Tables["ingest_discards"] = []map[string]any{}
	for _, row := range snapshot.Tables["entries"] {
		for column, value := range entryIngestDefaults {
			if _, found := row[column]; !found {
				row[column] = value
			}
		}
	}
	for _, row := range snapshot.Tables["operation_logs"] {
		addLogDefaults(row)
	}
	if _, present := snapshot.Tables["maintenance_state"]; !present {
		snapshot.Tables["maintenance_state"] = []map[string]any{initialMaintenanceData}
	}
}

// The export proves the old tables were preserved. Extend old expectations
// with reviewed defaults, never with arbitrary rows from the current database.
func projectCollectionSchemaDefaults(expected, actual *Manifest, legacyFormat int) (*Manifest, *Manifest) {
	if !expected.collectionMigrated {
		return expected, actual
	}
	left := *expected
	left.Tables = map[string]TableManifest{}
	left.Files = map[string][]File{}
	for table, entry := range expected.Tables {
		left.Tables[table] = entry
	}
	for group, files := range expected.Files {
		left.Files[group] = append([]File{}, files...)
	}
	affected := map[string][]map[string]any{}
	for table, defaults := range map[string]map[string]any{"entries": entryIngestDefaults, "operation_logs": logIngestDefaults} {
		entry, found := left.Tables[table]
		if !found {
			continue
		}
		columns := map[string]bool{}
		for _, column := range entry.Columns {
			columns[column] = true
		}
		missing := map[string]any{}
		for column, value := range defaults {
			if actual.SchemaVersion < 23 {
				column = entryColumn(table, column, true)
			}
			if !columns[column] {
				missing[column] = value
				entry.Columns = append(entry.Columns, column)
			}
		}
		if len(missing) == 0 {
			continue
		}
		affected[table] = entry.Data
		rows := make([]map[string]any, 0, len(entry.Data))
		for _, row := range entry.Data {
			copy := map[string]any{}
			for key, value := range row {
				copy[key] = value
			}
			for column, value := range missing {
				copy[column] = value
			}
			rows = append(rows, copy)
		}
		entry.Data = rows
		data, _ := json.Marshal(canonicalRows(table, rows))
		entry.SHA256 = sum(data)
		left.Tables[table] = entry
	}
	for table, rows := range map[string][]map[string]any{"ingest_assets": {}, "ingest_discards": {}, "maintenance_state": {initialMaintenanceData}} {
		if _, present := left.Tables[table]; present {
			continue
		}
		current, found := actual.Tables[table]
		if !found {
			continue
		}
		current.Data = rows
		current.Rows = int64(len(rows))
		current.JSONNulls = nil
		data, _ := json.Marshal(canonicalRows(table, rows))
		current.SHA256 = sum(data)
		left.Tables[table] = current
		if table != "maintenance_state" {
			expectedData, _ := json.MarshalIndent(rows, "", "  ")
			expectedData = append(expectedData, '\n')
			for _, file := range actual.Files["vault"] {
				if file.Path == ".reliquary/data/"+table+".json" && file.SHA256 == sum(expectedData) && file.Size == int64(len(expectedData)) {
					left.Files["vault"] = append(left.Files["vault"], file)
				}
			}
		}
	}
	for i, file := range left.Files["vault"] {
		if !strings.HasPrefix(file.Path, ".reliquary/data/") {
			continue
		}
		table := currentTable(strings.TrimSuffix(strings.TrimPrefix(file.Path, ".reliquary/data/"), ".json"))
		before, affected := affected[table]
		if !affected {
			continue
		}
		oldData, _ := json.MarshalIndent(namedRows(before, true, legacyFormat), "", "  ")
		oldData = append(oldData, '\n')
		if file.SHA256 != sum(oldData) || file.Size != int64(len(oldData)) {
			continue
		}
		next := left.Tables[table].Data
		newData, _ := json.MarshalIndent(next, "", "  ")
		newData = append(newData, '\n')
		for _, actualFile := range actual.Files["vault"] {
			if actualFile.Path == ".reliquary/data/"+table+".json" && actualFile.SHA256 == sum(newData) && actualFile.Size == int64(len(newData)) {
				left.Files["vault"][i] = actualFile
			}
		}
	}
	return &left, actual
}

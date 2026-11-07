package maintenance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

var managedEntityDefaults = map[string]map[string]any{
	"libraries":              {"entity_mode": "extracted", "entity_schema": map[string]any{}},
	"entities":               {"kind": "", "code": "", "attributes": map[string]any{}, "revision": 1, "archived_at": nil},
	"entry_entities":         {"origin": "extracted", "excluded": false, "evidence": []any{}, "body_hash": ""},
	"library_relation_types": {"hierarchical": false, "subject_kinds": []any{}, "object_kinds": []any{}},
}

var entityAutoMatchDefaults = map[string]map[string]any{
	"entities": {"auto_match": map[string]any{}},
}

var relationValidityDefaults = map[string]map[string]any{
	"relations": {"valid_from": nil, "valid_to": nil},
}

var productShelfDefaults = map[string]map[string]any{
	"libraries": {"shelf_enabled": false, "shelf_config": map[string]any{}},
}

var shelfSourceDefaults = map[string]map[string]any{
	"operation_logs": {"source": map[string]any{}, "changeset_id": ""},
}

func projectManagedEntitiesSnapshot(snapshot *Snapshot, targetVersion int64) {
	defer projectShelfFieldsSnapshot(snapshot, targetVersion)
	if snapshot.schemaVersion < 46 && targetVersion >= 46 {
		if _, exists := snapshot.Tables["shelf_document_versions"]; !exists {
			snapshot.Tables["shelf_document_versions"] = []map[string]any{}
		}
	}
	projectEntityDefaultsSnapshot(snapshot, targetVersion, 43, shelfSourceDefaults)
	if snapshot.schemaVersion < 43 && targetVersion >= 43 {
		if _, exists := snapshot.Tables["shelf_changesets"]; !exists {
			snapshot.Tables["shelf_changesets"] = []map[string]any{}
		}
	}
	projectEntityDefaultsSnapshot(snapshot, targetVersion, 42, productShelfDefaults)
	projectEntityDefaultsSnapshot(snapshot, targetVersion, 37, managedEntityDefaults)
	projectManagedEvolutionSnapshot(snapshot, targetVersion)
	projectEntityDefaultsSnapshot(snapshot, targetVersion, 39, entityAutoMatchDefaults)
	projectEntityDefaultsSnapshot(snapshot, targetVersion, 40, relationValidityDefaults)
	if snapshot.schemaVersion < 41 && targetVersion >= 41 {
		if _, exists := snapshot.Tables["entity_name_confirmations"]; !exists {
			snapshot.Tables["entity_name_confirmations"] = []map[string]any{}
		}
	}
}

func projectEntityDefaultsSnapshot(snapshot *Snapshot, targetVersion, version int64, defaults map[string]map[string]any) {
	if snapshot.schemaVersion >= version || targetVersion < version {
		return
	}
	for table, columns := range defaults {
		for _, row := range snapshot.Tables[table] {
			for column, value := range columns {
				if table == "relations" && column == "valid_from" {
					value = row["updated_at"]
				}
				row[column] = value
			}
		}
	}
}

func projectManagedEntitiesManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	expected, actual = projectEmptyPersistentTableManifest(expected, actual, "shelf_document_versions", 46)
	expected, actual = projectEntityDefaultsManifest(expected, actual, 43, shelfSourceDefaults)
	expected, actual = projectEmptyPersistentTableManifest(expected, actual, "shelf_changesets", 43)
	expected, actual = projectEntityDefaultsManifest(expected, actual, 42, productShelfDefaults)
	expected, actual = projectEntityDefaultsManifest(expected, actual, 37, managedEntityDefaults)
	expected, actual = projectManagedEvolutionManifest(expected, actual)
	expected, actual = projectEntityDefaultsManifest(expected, actual, 39, entityAutoMatchDefaults)
	expected, actual = projectEntityDefaultsManifest(expected, actual, 40, relationValidityDefaults)
	expected, actual = projectShelfFieldsManifest(expected, actual)
	return projectManagedNamesManifest(expected, actual)
}

// Only an empty confirmation table is implied by upgrading an older replica.
// Saved decisions remain authoritative and must still compare exactly.
func projectManagedNamesManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	return projectEmptyPersistentTableManifest(expected, actual, "entity_name_confirmations", 41)
}

func projectEmptyPersistentTableManifest(expected, actual *Manifest, table string, version int64) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= version || actual.SchemaVersion < version {
		return expected, actual
	}
	if _, exists := expected.Tables[table]; exists {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables)+1)
	for name, item := range expected.Tables {
		left.Tables[name] = item
	}
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		left.Files[group] = append([]File{}, files...)
	}
	registration := PersistentTables[table]
	columns := []string{}
	for column := range registration.Columns.Columns() {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	left.Tables[table] = TableManifest{Columns: columns, Data: []map[string]any{}, SHA256: sum([]byte("[]"))}
	for _, file := range actual.Files["vault"] {
		if file.Path == ".reliquary/"+registration.File {
			left.Files["vault"] = append(left.Files["vault"], File{Path: file.Path, SHA256: sum([]byte("[]\n")), Size: 3})
		}
	}
	return &left, actual
}

func projectEntityDefaultsManifest(expected, actual *Manifest, version int64, defaults map[string]map[string]any) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= version || actual.SchemaVersion < version {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	for table, item := range expected.Tables {
		left.Tables[table] = item
	}
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		left.Files[group] = append([]File{}, files...)
	}
	for table, columns := range defaults {
		item, ok := expected.Tables[table]
		if !ok || item.Data == nil {
			continue
		}
		hash, _ := json.Marshal(canonicalRows(table, item.Data))
		if item.SHA256 != sum(hash) {
			continue
		}
		before, _ := json.MarshalIndent(item.Data, "", "  ")
		before = append(before, '\n')
		rows := make([]map[string]any, 0, len(item.Data))
		for _, row := range item.Data {
			copy := catalogCopy(row)
			for column, value := range columns {
				if table == "relations" && column == "valid_from" {
					value = row["updated_at"]
				}
				copy[column] = value
			}
			rows = append(rows, copy)
		}
		item.Data = rows
		item.Columns = append([]string{}, item.Columns...)
		for column := range columns {
			item.Columns = append(item.Columns, column)
		}
		sort.Strings(item.Columns)
		hash, _ = json.Marshal(canonicalRows(table, rows))
		item.SHA256 = sum(hash)
		left.Tables[table] = item
		after, _ := json.MarshalIndent(rows, "", "  ")
		after = append(after, '\n')
		path := ".reliquary/data/" + table + ".json"
		if registration, ok := PersistentTables[table]; ok {
			path = ".reliquary/" + registration.File
		}
		if table == "operation_logs" {
			path = ".reliquary/operation_logs.jsonl"
			before = operationLogReplicaBytes(item.Data, columns, true)
			after = operationLogReplicaBytes(rows, nil, false)
		}
		for group, files := range left.Files {
			for i, file := range files {
				if file.Path == path && file.SHA256 == sum(before) && file.Size == int64(len(before)) {
					file.SHA256, file.Size = sum(after), int64(len(after))
					left.Files[group][i] = file
				}
			}
		}
	}
	return &left, actual
}

// Portable logs retain the established append-only JSONL representation.
func operationLogReplicaBytes(rows []map[string]any, remove map[string]any, strip bool) []byte {
	copies := namedRows(rows, true)
	sort.Slice(copies, func(i, j int) bool {
		a, b := fmt.Sprint(copies[i]["created_at"]), fmt.Sprint(copies[j]["created_at"])
		if a == b {
			return fmt.Sprint(copies[i]["id"]) < fmt.Sprint(copies[j]["id"])
		}
		return a < b
	})
	var data bytes.Buffer
	for _, row := range copies {
		if strip {
			for key := range remove {
				delete(row, key)
			}
		}
		encoded, _ := json.Marshal(row)
		data.Write(encoded)
		data.WriteByte('\n')
	}
	return data.Bytes()
}

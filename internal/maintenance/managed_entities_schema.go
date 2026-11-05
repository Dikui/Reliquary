package maintenance

import (
	"encoding/json"
	"sort"
)

var managedEntityDefaults = map[string]map[string]any{
	"libraries":              {"entity_mode": "extracted", "entity_schema": map[string]any{}},
	"entities":               {"kind": "", "code": "", "attributes": map[string]any{}, "revision": 1, "archived_at": nil},
	"entry_entities":         {"origin": "extracted", "excluded": false, "evidence": []any{}, "body_hash": ""},
	"library_relation_types": {"hierarchical": false, "subject_kinds": []any{}, "object_kinds": []any{}},
}

func projectManagedEntitiesSnapshot(snapshot *Snapshot, targetVersion int64) {
	projectEntityDefaultsSnapshot(snapshot, targetVersion, 37, managedEntityDefaults)
	projectManagedEvolutionSnapshot(snapshot, targetVersion)
}

func projectEntityDefaultsSnapshot(snapshot *Snapshot, targetVersion, version int64, defaults map[string]map[string]any) {
	if snapshot.schemaVersion >= version || targetVersion < version {
		return
	}
	for table, columns := range defaults {
		for _, row := range snapshot.Tables[table] {
			for column, value := range columns {
				row[column] = value
			}
		}
	}
}

func projectManagedEntitiesManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	expected, actual = projectEntityDefaultsManifest(expected, actual, 37, managedEntityDefaults)
	return projectManagedEvolutionManifest(expected, actual)
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

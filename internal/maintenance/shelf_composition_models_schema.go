package maintenance

import (
	"encoding/json"

	"reliquary/internal/types"
)

// Mirror migration 53 when restoring older replicas. Facts and their history
// stay untouched; the legacy values cannot establish model ownership.
func projectShelfCompositionModelsSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 53 || targetVersion < 53 {
		return
	}
	shelves := map[any]bool{}
	for _, row := range snapshot.Tables["libraries"] {
		if row["shelf_enabled"] != true {
			continue
		}
		shelves[row["id"]] = true
		schema, _ := row["entity_schema"].(map[string]any)
		schema = catalogCopy(schema)
		fields, _ := schema["fields"].([]any)
		fields = append([]any{}, fields...)
		for i, field := range fields {
			if definition, ok := field.(map[string]any); ok && (definition["key"] == "cluster_model" || definition["key"] == "container_model") {
				definition = catalogCopy(definition)
				definition["archived"] = true
				fields[i] = definition
			}
		}
		schema["fields"] = fields
		row["entity_schema"] = schema
	}
	data, _ := json.Marshal(types.ShelfCompositionFields()[1:])
	var defaults []any
	_ = json.Unmarshal(data, &defaults)
	for _, row := range snapshot.Tables["library_relation_types"] {
		if !shelves[row["library_id"]] || row["name"] != "composed_of" {
			continue
		}
		fields, _ := row["fields"].([]any)
		fields = append([]any{}, fields...)
		for _, candidate := range defaults {
			found := false
			for _, field := range fields {
				if definition, ok := field.(map[string]any); ok && definition["key"] == candidate.(map[string]any)["key"] {
					found = true
				}
			}
			if !found {
				fields = append(fields, candidate)
			}
		}
		row["fields"] = fields
	}
}

func projectShelfCompositionModelsManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 53 || actual.SchemaVersion < 53 {
		return expected, actual
	}
	tables := []string{"libraries", "library_relation_types"}
	snapshot := &Snapshot{schemaVersion: expected.SchemaVersion, Tables: map[string][]map[string]any{}}
	for _, table := range tables {
		item := expected.Tables[table]
		hash, _ := json.Marshal(canonicalRows(table, item.Data))
		if item.Rows > 0 && (item.Data == nil || item.SHA256 != sum(hash)) {
			return expected, actual
		}
		// Keep an empty table as [] so its replica file hash still matches.
		snapshot.Tables[table] = []map[string]any{}
		for _, row := range item.Data {
			snapshot.Tables[table] = append(snapshot.Tables[table], catalogCopy(row))
		}
	}
	projectShelfCompositionModelsSnapshot(snapshot, actual.SchemaVersion)
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	for name, item := range expected.Tables {
		left.Tables[name] = item
	}
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		left.Files[group] = append([]File{}, files...)
	}
	for _, table := range tables {
		item, ok := left.Tables[table]
		if !ok {
			continue
		}
		before, _ := json.MarshalIndent(item.Data, "", "  ")
		before = append(before, '\n')
		item.Data = snapshot.Tables[table]
		hash, _ := json.Marshal(canonicalRows(table, item.Data))
		item.SHA256 = sum(hash)
		left.Tables[table] = item
		after, _ := json.MarshalIndent(item.Data, "", "  ")
		after = append(after, '\n')
		path := ".reliquary/" + PersistentTables[table].File
		for group, files := range left.Files {
			for i, file := range files {
				if file.Path == path && file.SHA256 == sum(before) && file.Size == int64(len(before)) {
					left.Files[group][i].SHA256, left.Files[group][i].Size = sum(after), int64(len(after))
				}
			}
		}
	}
	return &left, actual
}

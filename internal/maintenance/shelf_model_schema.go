package maintenance

import (
	"encoding/json"

	"reliquary/internal/types"
)

func projectShelfFieldsSnapshot(snapshot *Snapshot, targetVersion int64) {
	defer projectShelfBindingsSnapshot(snapshot, targetVersion)
	if snapshot.schemaVersion >= 47 || targetVersion < 44 {
		return
	}
	for _, row := range snapshot.Tables["libraries"] {
		if row["shelf_enabled"] != true {
			continue
		}
		schema, ok := row["entity_schema"].(map[string]any)
		if !ok {
			continue
		}
		next := catalogCopy(schema)
		fields, _ := schema["fields"].([]any)
		fields = append([]any{}, fields...)
		data, _ := json.Marshal(types.ProductShelfFields())
		var defaults []any
		_ = json.Unmarshal(data, &defaults)
		for _, definition := range defaults {
			candidate := definition.(map[string]any)
			if (candidate["key"] == "requirement" || candidate["key"] == "current_stage") && targetVersion < 47 {
				continue
			}
			if candidate["key"] == "certification" && targetVersion < 45 {
				continue
			}
			found := false
			for _, field := range fields {
				if item, ok := field.(map[string]any); ok && item["key"] == candidate["key"] {
					found = true
				}
			}
			if !found {
				fields = append(fields, definition)
			}
		}
		next["fields"] = fields
		row["entity_schema"] = next
	}
}

func projectShelfFieldsManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 47 || actual.SchemaVersion < 44 {
		return expected, actual
	}
	item, ok := expected.Tables["libraries"]
	if !ok || item.Data == nil {
		return expected, actual
	}
	beforeHash, _ := json.Marshal(canonicalRows("libraries", item.Data))
	if item.SHA256 != sum(beforeHash) {
		return expected, actual
	}
	before, _ := json.MarshalIndent(item.Data, "", "  ")
	before = append(before, '\n')
	rows := make([]map[string]any, 0, len(item.Data))
	for _, row := range item.Data {
		rows = append(rows, catalogCopy(row))
	}
	projectShelfFieldsSnapshot(&Snapshot{schemaVersion: expected.SchemaVersion, Tables: map[string][]map[string]any{"libraries": rows}}, actual.SchemaVersion)
	item.Data = rows
	data, _ := json.Marshal(canonicalRows("libraries", rows))
	item.SHA256 = sum(data)
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	for table, entry := range expected.Tables {
		left.Tables[table] = entry
	}
	left.Tables["libraries"] = item
	after, _ := json.MarshalIndent(rows, "", "  ")
	after = append(after, '\n')
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		for _, file := range files {
			if file.Path == ".reliquary/config/libraries.json" && file.SHA256 == sum(before) && file.Size == int64(len(before)) {
				file.SHA256, file.Size = sum(after), int64(len(after))
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	return &left, actual
}

func projectShelfBindingsSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 47 || targetVersion < 47 {
		return
	}
	shelves := map[any]bool{}
	for _, l := range snapshot.Tables["libraries"] {
		if l["shelf_enabled"] == true {
			shelves[l["id"]] = true
		}
	}
	// Project the historical version 47 definition, independently of the
	// current product-line-only requirement schema.
	raw, _ := json.Marshal(types.EntityFieldDefinition{Key: "module_types", Name: "适用模块类型", Type: "list", Kinds: []string{}})
	var definition any
	_ = json.Unmarshal(raw, &definition)
	for _, row := range snapshot.Tables["library_relation_types"] {
		if !shelves[row["library_id"]] || row["name"] != "applies_to" {
			continue
		}
		row["object_kinds"] = []any{"product_line", "document_requirement"}
		fields, _ := row["fields"].([]any)
		found := false
		for _, field := range fields {
			if f, ok := field.(map[string]any); ok && f["key"] == "module_types" {
				found = true
			}
		}
		if !found {
			row["fields"] = append(append([]any{}, fields...), definition)
		}
	}
}
func projectShelfBindingsManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 47 || actual.SchemaVersion < 47 {
		return expected, actual
	}
	item, ok := expected.Tables["library_relation_types"]
	if !ok || item.Data == nil {
		return expected, actual
	}
	hash, _ := json.Marshal(canonicalRows("library_relation_types", item.Data))
	if item.SHA256 != sum(hash) {
		return expected, actual
	}
	before, _ := json.MarshalIndent(item.Data, "", "  ")
	before = append(before, '\n')
	rows := make([]map[string]any, 0, len(item.Data))
	for _, row := range item.Data {
		rows = append(rows, catalogCopy(row))
	}
	projectShelfBindingsSnapshot(&Snapshot{schemaVersion: expected.SchemaVersion, Tables: map[string][]map[string]any{"libraries": expected.Tables["libraries"].Data, "library_relation_types": rows}}, actual.SchemaVersion)
	item.Data = rows
	data, _ := json.Marshal(canonicalRows("library_relation_types", rows))
	item.SHA256 = sum(data)
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	for table, item := range expected.Tables {
		left.Tables[table] = item
	}
	left.Tables["library_relation_types"] = item
	after, _ := json.MarshalIndent(rows, "", "  ")
	after = append(after, '\n')
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		for _, file := range files {
			if file.Path == ".reliquary/data/library_relation_types.json" && file.SHA256 == sum(before) && file.Size == int64(len(before)) {
				file.SHA256, file.Size = sum(after), int64(len(after))
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	return &left, actual
}

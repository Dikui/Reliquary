package maintenance

import "encoding/json"

// The legacy bucket is explicit and preserves data without guessing worksheet
// ownership from a name that older imports may already have combined.
func projectShelfRequirementListsSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 51 || targetVersion < 51 {
		return
	}
	requirements := map[any]bool{}
	for _, row := range snapshot.Tables["entities"] {
		if row["kind"] == "document_requirement" {
			requirements[row["id"]] = true
			projectLegacyRequirementList(row)
		}
	}
	for _, row := range snapshot.Tables["entity_attribute_versions"] {
		if requirements[row["entity_id"]] {
			projectLegacyRequirementList(row)
		}
	}
	for _, row := range snapshot.Tables["shelf_document_versions"] {
		ids := []any{}
		if id, ok := row["requirement_id"].(string); ok && id != "" {
			ids = append(ids, id)
		}
		row["requirement_ids"] = ids
	}
}
func projectLegacyRequirementList(row map[string]any) {
	attributes, _ := row["attributes"].(map[string]any)
	attributes = catalogCopy(attributes)
	requirement, _ := attributes["requirement"].(map[string]any)
	requirement = catalogCopy(requirement)
	if name, _ := requirement["list_name"].(string); name == "" {
		requirement["list_name"] = "历史未分清单"
	}
	attributes["requirement"] = requirement
	row["attributes"] = attributes
}

func projectShelfRequirementListsManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 51 || actual.SchemaVersion < 51 {
		return expected, actual
	}
	tables := []string{"entities", "entity_attribute_versions", "shelf_document_versions"}
	snapshot := &Snapshot{schemaVersion: expected.SchemaVersion, Tables: map[string][]map[string]any{}}
	for _, table := range tables {
		item := expected.Tables[table]
		hash, _ := json.Marshal(canonicalRows(table, item.Data))
		if item.Rows > 0 && (item.Data == nil || item.SHA256 != sum(hash)) {
			return expected, actual
		}
		for _, row := range item.Data {
			snapshot.Tables[table] = append(snapshot.Tables[table], catalogCopy(row))
		}
	}
	projectShelfRequirementListsSnapshot(snapshot, actual.SchemaVersion)
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
		if item.Data == nil {
			item.Data = []map[string]any{}
		}
		if table == "shelf_document_versions" {
			item.Columns = append(append([]string{}, item.Columns...), "requirement_ids")
		}
		hash, _ := json.Marshal(canonicalRows(table, item.Data))
		item.SHA256 = sum(hash)
		left.Tables[table] = item
		after, _ := json.MarshalIndent(item.Data, "", "  ")
		after = append(after, '\n')
		path := ".reliquary/" + PersistentTables[table].File
		if table == "entities" {
			path = ".reliquary/data/entities.json"
		}
		for group, files := range left.Files {
			for i, file := range files {
				if file.Path == path && file.SHA256 == sum(before) && file.Size == int64(len(before)) {
					// Import may retain the verified original sidecar or regenerate it.
					unchanged := false
					for _, candidate := range actual.Files[group] {
						if candidate == file {
							unchanged = true
							break
						}
					}
					if !unchanged {
						file.SHA256, file.Size = sum(after), int64(len(after))
						left.Files[group][i] = file
					}
				}
			}
		}
	}
	return &left, actual
}

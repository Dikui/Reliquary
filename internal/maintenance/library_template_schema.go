package maintenance

import (
	"encoding/json"

	"reliquary/internal/types"
)

func projectLibraryTemplateSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 34 || targetVersion < 34 {
		return
	}
	for _, row := range snapshot.Tables["libraries"] {
		row["template"] = types.TemplateStandard
		row["source_evidence_rule"] = ""
	}
}

func projectLibraryTemplateManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 34 || actual.SchemaVersion < 34 {
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
	projectLibraryTemplateSnapshot(&Snapshot{schemaVersion: expected.SchemaVersion, Tables: map[string][]map[string]any{"libraries": rows}}, actual.SchemaVersion)
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

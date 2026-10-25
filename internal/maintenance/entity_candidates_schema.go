package maintenance

import (
	"encoding/json"
	"path"
	"slices"
)

func projectEntityCandidatesSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 40 || targetVersion < 40 {
		return
	}
	for _, table := range []string{"entity_candidates", "entity_candidate_occurrences"} {
		if _, ok := snapshot.Tables[table]; !ok {
			snapshot.Tables[table] = []map[string]any{}
		}
	}
	for _, row := range snapshot.Tables["facts"] {
		for _, field := range []string{"subject_entity_id", "object_entity_id"} {
			if _, ok := row[field]; !ok {
				row[field] = nil
			}
		}
	}
}

func projectEntityCandidatesManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 40 || actual.SchemaVersion < 40 {
		return expected, actual
	}
	for _, table := range []string{"entity_candidates", "entity_candidate_occurrences"} {
		expected, actual = projectEmptyRegisteredTable(expected, actual, table)
	}
	old, ok := expected.Tables["facts"]
	if !ok || slices.Contains(old.Columns, "subject_entity_id") {
		return expected, actual
	}
	// Clone the older rows and add only the migration's nullable defaults.
	raw, _ := json.Marshal(old.Data)
	var rows []map[string]any
	_ = json.Unmarshal(raw, &rows)
	projectEntityCandidatesSnapshot(&Snapshot{Tables: map[string][]map[string]any{"facts": rows}}, 40)
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	for table, item := range expected.Tables {
		left.Tables[table] = item
	}
	before, _ := json.MarshalIndent(old.Data, "", "  ")
	before = append(before, '\n')
	old.Data = rows
	old.Columns = append(append([]string{}, old.Columns...), "subject_entity_id", "object_entity_id")
	slices.Sort(old.Columns)
	canonical, _ := json.Marshal(canonicalRows("facts", rows))
	old.SHA256 = sum(canonical)
	left.Tables["facts"] = old
	after, _ := json.MarshalIndent(rows, "", "  ")
	after = append(after, '\n')
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		for _, file := range files {
			if path.Base(file.Path) == "facts.json" && file.SHA256 == sum(before) {
				file.SHA256 = sum(after)
				file.Size = int64(len(after))
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	return &left, actual
}

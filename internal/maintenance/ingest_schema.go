package maintenance

import (
	"encoding/json"
	"path"
)

// projectIngestSnapshot materializes only newly introduced empty defaults.
// Values already present in a current replica are never overwritten.
func projectIngestSnapshot(snapshot *Snapshot) {
	for _, row := range snapshot.Tables["entries"] {
		for field, value := range map[string]any{"source_ref": "", "source_body_hash": "", "identifier": "", "graph": nil} {
			if _, present := row[field]; !present {
				row[field] = value
			}
		}
	}
}

// The 10 to 11 migration adds optional fields with lossless empty defaults.
func projectIngestManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	old, ok := expected.Tables["entries"]
	if !ok {
		return expected, actual
	}
	current := actual.Tables["entries"]
	has := func(columns []string, field string) bool {
		for _, column := range columns {
			if column == field {
				return true
			}
		}
		return false
	}
	if has(old.Columns, "source_ref") || !has(current.Columns, "source_ref") {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	for table, item := range expected.Tables {
		left.Tables[table] = item
	}
	before, _ := json.MarshalIndent(old.Data, "", "  ")
	before = append(before, '\n')
	data, _ := json.Marshal(old.Data)
	var rows []map[string]any
	_ = json.Unmarshal(data, &rows)
	projectIngestSnapshot(&Snapshot{Tables: map[string][]map[string]any{"entries": rows}})
	old.Data = rows
	old.Columns = current.Columns
	canonical, _ := json.Marshal(canonicalRows("entries", rows))
	old.SHA256 = sum(canonical)
	left.Tables["entries"] = old
	after, _ := json.MarshalIndent(rows, "", "  ")
	after = append(after, '\n')
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		for _, file := range files {
			if path.Base(file.Path) == "entries.json" && file.SHA256 == sum(before) {
				file.SHA256 = sum(after)
				file.Size = int64(len(after))
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	return &left, actual
}

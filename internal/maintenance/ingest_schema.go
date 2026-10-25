package maintenance

import (
	"encoding/json"
	"path"
	"slices"
)

// projectIngestSnapshot materializes only newly introduced empty defaults.
// Values already present in a current replica are never overwritten.
func projectIngestSnapshot(snapshot *Snapshot) {
	projectEntryDefaults(snapshot, map[string]any{
		"source_ref": "", "source_body_hash": "", "identifier": "", "graph": nil,
	})
}

func projectSourceRevisionSnapshot(snapshot *Snapshot) {
	projectEntryDefaults(snapshot, map[string]any{"source_id": "", "source_revision": nil})
}

func projectEntryDefaults(snapshot *Snapshot, defaults map[string]any) {
	for _, row := range snapshot.Tables["entries"] {
		for field, value := range defaults {
			if _, present := row[field]; !present {
				row[field] = value
			}
		}
	}
}

// Optional ingest fields have lossless empty defaults when crossing their migrations.
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
	defaults := map[string]any{}
	for field, value := range map[string]any{
		"source_ref": "", "source_body_hash": "", "identifier": "", "graph": nil,
		"source_id": "", "source_revision": nil, "entities_manual": false, "domains_manual": false,
	} {
		if !has(old.Columns, field) && has(current.Columns, field) {
			defaults[field] = value
		}
	}
	if len(defaults) == 0 {
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
	projectEntryDefaults(&Snapshot{Tables: map[string][]map[string]any{"entries": rows}}, defaults)
	old.Data = rows
	old.Columns = append([]string(nil), old.Columns...)
	for field := range defaults {
		old.Columns = append(old.Columns, field)
	}
	slices.Sort(old.Columns)
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

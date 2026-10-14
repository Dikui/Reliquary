package maintenance

import "sort"

// Portable replica verification compares snapshots directly, before a new
// manifest is written. Apply the same versioned empty default on that path.
func projectSegmentSnapshot(original, current *Snapshot) {
	if original.schemaVersion < 17 && current.schemaVersion >= 17 {
		if _, exists := original.Tables["entry_processing_outputs"]; !exists {
			original.Tables["entry_processing_outputs"] = []map[string]any{}
		}
	}
	if original.schemaVersion >= 16 || current.schemaVersion < 16 {
		return
	}
	if _, exists := original.Tables["entry_segment_runs"]; !exists {
		original.Tables["entry_segment_runs"] = []map[string]any{}
	}
}

// Version 16 only adds an empty checkpoint table. Project precisely that
// empty default when verifying an older backup against an upgraded database.
func projectSegmentManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	expected, actual = projectProcessingOutputManifest(expected, actual)
	if expected.SchemaVersion >= 16 || actual.SchemaVersion < 16 {
		return expected, actual
	}
	if _, present := expected.Tables["entry_segment_runs"]; present {
		return expected, actual
	}
	if _, present := actual.Tables["entry_segment_runs"]; !present {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables)+1)
	for table, item := range expected.Tables {
		left.Tables[table] = item
	}
	columns := []string{}
	for column := range Rules["entry_segment_runs"].Columns() {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	left.Tables["entry_segment_runs"] = TableManifest{Columns: columns, SHA256: sum([]byte("[]")), Data: []map[string]any{}}
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		left.Files[group] = append([]File{}, files...)
	}
	for _, files := range actual.Files {
		for _, file := range files {
			if file.Path == ".reliquary/data/entry_segment_runs.json" {
				left.Files["vault"] = append(left.Files["vault"], File{Path: file.Path, SHA256: sum([]byte("[]\n")), Size: 3})
			}
		}
	}
	return &left, actual
}

func projectProcessingOutputManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 17 || actual.SchemaVersion < 17 {
		return expected, actual
	}
	if _, ok := expected.Tables["entry_processing_outputs"]; ok {
		return expected, actual
	}
	if _, ok := actual.Tables["entry_processing_outputs"]; !ok {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables)+1)
	for table, item := range expected.Tables {
		left.Tables[table] = item
	}
	columns := []string{}
	for column := range Rules["entry_processing_outputs"].Columns() {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	left.Tables["entry_processing_outputs"] = TableManifest{Columns: columns, SHA256: sum([]byte("[]")), Data: []map[string]any{}}
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		left.Files[group] = append([]File{}, files...)
	}
	for _, files := range actual.Files {
		for _, file := range files {
			if file.Path == ".reliquary/data/entry_processing_outputs.json" {
				left.Files["vault"] = append(left.Files["vault"], File{Path: file.Path, SHA256: sum([]byte("[]\n")), Size: 3})
			}
		}
	}
	return &left, actual
}

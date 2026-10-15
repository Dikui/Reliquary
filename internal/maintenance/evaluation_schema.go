package maintenance

import "sort"

var evaluationPortableTables = []string{"evaluation_sets", "evaluation_questions", "evaluation_answers", "evaluation_runs", "evaluation_run_results"}

func projectEvaluationSnapshot(original, current *Snapshot) {
	if original.schemaVersion >= 21 || current.schemaVersion < 21 {
		return
	}
	for _, table := range evaluationPortableTables {
		if _, ok := original.Tables[table]; !ok {
			original.Tables[table] = []map[string]any{}
		}
	}
}

// The upgrade adds empty tables only. Existing evaluation rows and modern
// manifests always remain subject to exact verification.
func projectEvaluationManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 21 || actual.SchemaVersion < 21 {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables)+len(evaluationPortableTables))
	left.Files = make(map[string][]File, len(expected.Files))
	for table, m := range expected.Tables {
		left.Tables[table] = m
	}
	for group, files := range expected.Files {
		left.Files[group] = append([]File{}, files...)
	}
	for _, table := range evaluationPortableTables {
		if _, ok := left.Tables[table]; ok {
			continue
		}
		if _, ok := actual.Tables[table]; !ok {
			continue
		}
		columns := []string{}
		for c := range Rules[table].Columns() {
			columns = append(columns, c)
		}
		sort.Strings(columns)
		left.Tables[table] = TableManifest{Columns: columns, SHA256: sum([]byte("[]")), Data: []map[string]any{}}
		for _, files := range actual.Files {
			for _, file := range files {
				if file.Path == ".reliquary/"+PersistentTables[table].File {
					left.Files["vault"] = append(left.Files["vault"], File{Path: file.Path, SHA256: sum([]byte("[]\n")), Size: 3})
				}
			}
		}
	}
	if _, ok := actual.Tables["search_records"]; ok {
		if _, exists := left.Tables["search_records"]; !exists {
			columns := []string{}
			for column := range BackupOnlyTables["search_records"].Columns() {
				columns = append(columns, column)
			}
			sort.Strings(columns)
			left.Tables["search_records"] = TableManifest{Columns: columns, SHA256: sum([]byte("[]"))}
		}
	}
	return &left, actual
}

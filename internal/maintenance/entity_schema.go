package maintenance

import "sort"

// An upgrade adds an empty seed marker; startup imports the directory later.
func projectEntitySeedManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 36 || actual.SchemaVersion < 36 {
		return expected, actual
	}
	return projectEmptyRegisteredTable(expected, actual, "entity_seed_imports")
}

func projectEmptyRegisteredTable(expected, actual *Manifest, table string) (*Manifest, *Manifest) {
	if _, ok := expected.Tables[table]; ok {
		return expected, actual
	}
	if _, ok := actual.Tables[table]; !ok {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables)+1)
	for name, item := range expected.Tables {
		left.Tables[name] = item
	}
	columns := []string{}
	for column := range PersistentTables[table].Columns.Columns() {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	left.Tables[table] = TableManifest{Columns: columns, SHA256: sum([]byte("[]")), Data: []map[string]any{}}
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		left.Files[group] = append([]File{}, files...)
	}
	for _, file := range actual.Files["vault"] {
		if file.Path == ".reliquary/"+PersistentTables[table].File {
			left.Files["vault"] = append(left.Files["vault"],
				File{
					Path:   file.Path,
					SHA256: sum([]byte("[]\n")),
					Size:   3,
				})
		}
	}
	return &left, actual
}

package maintenance

import (
	"encoding/json"
	"path"
	"slices"
)

var manualGroupingDefaults = map[string]map[string]any{
	"entries": {"grouping_manual": false},
	"facts":   {"manual_corrected": false, "merged_into_id": ""},
	"stories": {"manual_corrected": false, "merged_into_id": ""},
}

func projectManualGroupingSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 42 || targetVersion < 42 {
		return
	}
	for table, defaults := range manualGroupingDefaults {
		for _, row := range snapshot.Tables[table] {
			for field, value := range defaults {
				if _, ok := row[field]; !ok {
					row[field] = value
				}
			}
		}
	}
}

func projectManualGroupingManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 42 || actual.SchemaVersion < 42 {
		return expected, actual
	}
	for table, defaults := range manualGroupingDefaults {
		old, ok := expected.Tables[table]
		if !ok {
			continue
		}
		missing := map[string]any{}
		for field, value := range defaults {
			if !slices.Contains(old.Columns, field) {
				missing[field] = value
			}
		}
		if len(missing) == 0 {
			continue
		}
		// Only add top-level defaults. Keep JSON numbers and nested user values exact.
		rows := make([]map[string]any, len(old.Data))
		for i, original := range old.Data {
			row := make(map[string]any, len(original)+len(missing))
			for field, value := range original {
				row[field] = value
			}
			for field, value := range missing {
				row[field] = value
			}
			rows[i] = row
		}
		left := *expected
		left.Tables = make(map[string]TableManifest, len(expected.Tables))
		for name, item := range expected.Tables {
			left.Tables[name] = item
		}
		before, _ := json.MarshalIndent(old.Data, "", "  ")
		before = append(before, '\n')
		old.Data = rows
		old.Columns = append([]string{}, old.Columns...)
		for field := range missing {
			old.Columns = append(old.Columns, field)
		}
		slices.Sort(old.Columns)
		canonical, _ := json.Marshal(canonicalRows(table, rows))
		old.SHA256 = sum(canonical)
		left.Tables[table] = old
		after, _ := json.MarshalIndent(rows, "", "  ")
		after = append(after, '\n')
		left.Files = make(map[string][]File, len(expected.Files))
		for group, files := range expected.Files {
			for _, file := range files {
				if path.Base(file.Path) == table+".json" && file.SHA256 == sum(before) {
					file.SHA256 = sum(after)
					file.Size = int64(len(after))
				}
				left.Files[group] = append(left.Files[group], file)
			}
		}
		expected = &left
	}
	return expected, actual
}

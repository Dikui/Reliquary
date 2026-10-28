package maintenance

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

func projectLibraryColorSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 35 || targetVersion < 35 {
		return
	}
	rows := append([]map[string]any{}, snapshot.Tables["libraries"]...)
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		// PostgreSQL sorts absent creation times last; IDs break timestamp ties.
		if (a["created_at"] == nil) != (b["created_at"] == nil) {
			return a["created_at"] != nil
		}
		x, _ := time.Parse(time.RFC3339Nano, stringValue(a["created_at"]))
		y, _ := time.Parse(time.RFC3339Nano, stringValue(b["created_at"]))
		if !x.Equal(y) {
			return x.Before(y)
		}
		return stringValue(a["id"]) < stringValue(b["id"])
	})
	counts := map[string]int{}
	for _, row := range rows {
		space := fmt.Sprint(row["space_id"])
		row["color"] = counts[space]%8 + 1
		counts[space]++
	}
}

func projectLibraryColorManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 35 || actual.SchemaVersion < 35 {
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
	projectLibraryColorSnapshot(&Snapshot{
		schemaVersion: expected.SchemaVersion, Tables: map[string][]map[string]any{"libraries": rows},
	}, actual.SchemaVersion)
	item.Data = rows
	item.Columns = append(append([]string{}, item.Columns...), "color")
	sort.Strings(item.Columns)
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
			if file.Path == ".reliquary/config/libraries.json" &&
				file.SHA256 == sum(before) && file.Size == int64(len(before)) {
				file.SHA256, file.Size = sum(after), int64(len(after))
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	return &left, actual
}

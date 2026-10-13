package maintenance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
)

// Deleted historical owners cannot safely be assigned to a current library.
// Keep those records in an inert legacy scope, with all original data intact.
func projectDiscardSnapshot(snapshot *Snapshot) {
	owners := map[string]string{}
	for _, entry := range snapshot.Tables["entries"] {
		owners[fmt.Sprint(entry["space_id"])+":"+stringValue(entry["id"])] = stringValue(entry["library_id"])
	}
	for _, row := range snapshot.Tables["ingest_discards"] {
		if _, present := row["library_id"]; present {
			continue
		}
		space := fmt.Sprint(row["space_id"])
		library := owners[space+":"+stringValue(row["article_id"])]
		if library == "" {
			library = "legacy-space:" + space
		}
		row["library_id"] = library
		hash := sha256.Sum256([]byte("url:" + stringValue(row["normalized_url"])))
		row["fingerprint"] = hex.EncodeToString(hash[:])
	}
}

func projectDiscardManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	old, ok := expected.Tables["ingest_discards"]
	if !ok {
		return expected, actual
	}
	current := actual.Tables["ingest_discards"]
	has := func(cols []string, field string) bool {
		for _, c := range cols {
			if c == field {
				return true
			}
		}
		return false
	}
	if has(old.Columns, "library_id") || !has(current.Columns, "library_id") {
		return expected, actual
	}
	raw, _ := json.Marshal(old.Data)
	var rows []map[string]any
	_ = json.Unmarshal(raw, &rows)
	projectDiscardSnapshot(&Snapshot{Tables: map[string][]map[string]any{"entries": expected.Tables["entries"].Data, "ingest_discards": rows}})
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	for table, item := range expected.Tables {
		left.Tables[table] = item
	}
	before, _ := json.MarshalIndent(old.Data, "", "  ")
	before = append(before, '\n')
	old.Data, old.Columns = rows, current.Columns
	canonical, _ := json.Marshal(canonicalRows("ingest_discards", rows))
	old.SHA256 = sum(canonical)
	left.Tables["ingest_discards"] = old
	after, _ := json.MarshalIndent(rows, "", "  ")
	after = append(after, '\n')
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		for _, file := range files {
			if path.Base(file.Path) == "ingest_discards.json" && file.SHA256 == sum(before) {
				file.SHA256 = sum(after)
				file.Size = int64(len(after))
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	return &left, actual
}

package maintenance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

// Migration 15 retains the legacy space watermark and copies it to each library.
func projectFollowSnapshot(snapshot *Snapshot) {
	for _, row := range snapshot.Tables["topics"] {
		if _, ok := row["library_ids"]; !ok {
			row["library_ids"] = []any{}
		}
	}
	if _, ok := snapshot.Tables["follow_feed_library_states"]; ok {
		return
	}
	rows := []map[string]any{}
	for _, state := range snapshot.Tables["follow_feed_states"] {
		for _, library := range snapshot.Tables["libraries"] {
			if fmt.Sprint(state["space_id"]) == fmt.Sprint(library["space_id"]) {
				rows = append(rows, map[string]any{"space_id": state["space_id"], "library_id": library["id"], "last_viewed_at": state["last_viewed_at"]})
			}
		}
	}
	snapshot.Tables["follow_feed_library_states"] = canonicalRows("follow_feed_library_states", rows)
}

func projectFollowManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if _, old := expected.Tables["follow_feed_library_states"]; old {
		return expected, actual
	}
	if _, current := actual.Tables["follow_feed_library_states"]; !current {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables)+1)
	snapshot := &Snapshot{Tables: map[string][]map[string]any{}}
	for name, item := range expected.Tables {
		raw, _ := json.Marshal(item.Data)
		var rows []map[string]any
		if item.Data != nil {
			d := json.NewDecoder(bytes.NewReader(raw))
			d.UseNumber()
			_ = d.Decode(&rows)
		}
		item.Data = rows
		left.Tables[name] = item
		snapshot.Tables[name] = rows
	}
	projectFollowSnapshot(snapshot)
	for _, name := range []string{"topics", "follow_feed_library_states"} {
		item := left.Tables[name]
		item.Data = snapshot.Tables[name]
		item.Rows = int64(len(item.Data))
		if name == "topics" {
			item.Columns = append(append([]string{}, item.Columns...), "library_ids")
			slices.Sort(item.Columns)
		} else {
			item.Columns = []string{"last_viewed_at", "library_id", "space_id"}
		}
		raw, _ := json.Marshal(canonicalRows(name, item.Data))
		item.SHA256 = sum(raw)
		left.Tables[name] = item
	}
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		for _, file := range files {
			if file.Path == ".reliquary/data/topics.json" {
				before, _ := json.MarshalIndent(expected.Tables["topics"].Data, "", "  ")
				before = append(before, '\n')
				if file.SHA256 == sum(before) && file.Size == int64(len(before)) {
					after, _ := json.MarshalIndent(snapshot.Tables["topics"], "", "  ")
					after = append(after, '\n')
					file.SHA256, file.Size = sum(after), int64(len(after))
				}
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	// Database-only manifests have an empty vault group. They must not gain
	// a physical file merely because the new table is projected into them.
	if len(expected.Files["vault"]) > 0 || len(actual.Files["vault"]) > 0 {
		raw, _ := json.MarshalIndent(snapshot.Tables["follow_feed_library_states"], "", "  ")
		raw = append(raw, '\n')
		left.Files["vault"] = append(left.Files["vault"], File{Path: ".reliquary/data/follow_feed_library_states.json", SHA256: sum(raw), Size: int64(len(raw))})
	}
	return &left, actual
}

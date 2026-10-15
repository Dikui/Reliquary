package maintenance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"time"
)

const defaultVectorModelID = "builtin-qwen3-embedding-06b"

// These values exactly match migration 14, including SQL defaults. Only the
// migration-generated timestamps come from the migrated database.
func migrationVectorModel(created, updated any) map[string]any {
	return map[string]any{
		"id": defaultVectorModelID, "space_id": json.Number("1"), "name": "qwen3-embedding:0.6b", "display_name": "",
		"type": "Embedding", "source": "local", "description": "Local text embedding",
		"parameters": map[string]any{"embedding_parameters": map[string]any{"dimension": json.Number("1024"), "truncate_prompt_tokens": json.Number("8192")}, "base_url": "http://127.0.0.1:11434"},
		"is_default": false, "status": "active", "is_builtin": true, "managed_by": "", "deleted_at": nil,
		"created_at": created, "updated_at": updated,
	}
}

func vectorSnapshotLegacy(snapshot *Snapshot) bool {
	if snapshot.schemaVersion >= 14 {
		return false
	}
	for _, row := range snapshot.Tables["embedding_rebuild_runs"] {
		if _, ok := row["candidate"]; ok {
			return false
		}
	}
	for _, row := range snapshot.Tables["models"] {
		if row["id"] == defaultVectorModelID {
			return false
		}
	}
	return true
}

func projectVectorSnapshot(snapshot *Snapshot, created, updated any, legacy bool) {
	for _, row := range snapshot.Tables["embedding_rebuild_runs"] {
		if _, ok := row["candidate"]; !ok {
			row["candidate"] = false
		}
		if _, ok := row["switched_at"]; !ok {
			row["switched_at"] = nil
		}
	}
	if !legacy {
		return
	}
	hasModel, personal := false, false
	for _, row := range snapshot.Tables["models"] {
		hasModel = hasModel || row["id"] == defaultVectorModelID
	}
	for _, row := range snapshot.Tables["spaces"] {
		personal = personal || fmt.Sprint(row["id"]) == "1"
		if row["embedding_model_id"] == "" {
			row["embedding_model_id"] = defaultVectorModelID
		}
	}
	if personal && !hasModel {
		snapshot.Tables["models"] = append(snapshot.Tables["models"], migrationVectorModel(created, updated))
	}
	for _, rows := range snapshot.Tables {
		sortRows(rows)
	}
}

// Project only the exact 12-to-14 schema transition. Current snapshots and
// modifications to migrated defaults still undergo the ordinary comparisons.
func projectVectorManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if slices.Contains(expected.Tables["embedding_rebuild_runs"].Columns, "candidate") || !slices.Contains(actual.Tables["embedding_rebuild_runs"].Columns, "candidate") {
		return expected, actual
	}
	return projectVectorManifestCopy(expected, actual)
}

func projectVectorManifestCopy(expected, actual *Manifest) (*Manifest, *Manifest) {
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	snapshot := &Snapshot{Tables: map[string][]map[string]any{}}
	for table, item := range expected.Tables {
		raw, _ := json.Marshal(item.Data)
		rows := []map[string]any(nil)
		if item.Data != nil {
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.UseNumber()
			_ = decoder.Decode(&rows)
		}
		item.Data = rows
		left.Tables[table], snapshot.Tables[table] = item, rows
	}
	var created, updated any
	for _, row := range actual.Tables["models"].Data {
		if row["id"] == defaultVectorModelID {
			// Both defaults are evaluated in the same migration statement.
			stamp, ok := row["created_at"].(string)
			if _, err := time.Parse(time.RFC3339Nano, stamp); ok && err == nil && row["updated_at"] == stamp {
				created, updated = stamp, stamp
			}
		}
	}
	projectVectorSnapshot(snapshot, created, updated, true)
	affected := map[string]bool{"models": true, "spaces": true, "embedding_rebuild_runs": true}
	for table := range affected {
		item := left.Tables[table]
		item.Data = snapshot.Tables[table]
		if table == "embedding_rebuild_runs" {
			item.Columns = append(append([]string{}, item.Columns...), "candidate", "switched_at")
			slices.Sort(item.Columns)
		}
		item.Rows = int64(len(item.Data))
		raw, _ := json.Marshal(canonicalRows(table, item.Data))
		item.SHA256 = sum(raw)
		left.Tables[table] = item
	}
	for table, registration := range RegeneratedTables {
		if registration.Since == 14 {
			if _, present := left.Tables[table]; !present {
				left.Tables[table] = TableManifest{Regenerated: true, Columns: []string{}}
			}
		}
	}
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		for _, file := range files {
			table := path.Base(file.Path)
			table = table[:len(table)-len(path.Ext(table))]
			if affected[table] && (file.Path == ".reliquary/config/"+table+".json" || file.Path == ".reliquary/data/"+table+".json") {
				before, _ := json.MarshalIndent(expected.Tables[table].Data, "", "  ")
				before = append(before, '\n')
				if file.SHA256 == sum(before) && file.Size == int64(len(before)) {
					after, _ := json.MarshalIndent(snapshot.Tables[table], "", "  ")
					after = append(after, '\n')
					file.SHA256, file.Size = sum(after), int64(len(after))
				}
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	return &left, actual
}

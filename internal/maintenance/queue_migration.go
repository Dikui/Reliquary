package maintenance

import (
	"encoding/json"
	"errors"
)

// Loaded files have already passed checksum validation. Only the retired
// transport rows are discarded when importing into the current schema.
func migrateQueueSnapshot(snapshot *Snapshot) error {
	if _, old := snapshot.Tables["task_jobs"]; !old {
		return nil
	}
	if _, current := snapshot.Tables["task_pending_ops"]; current {
		return errors.New("replica contains both legacy tasks and pending operations")
	}
	delete(snapshot.Tables, "task_jobs")
	delete(snapshot.JSONNulls, "task_jobs")
	snapshot.Tables["task_pending_ops"] = []map[string]any{}
	for _, row := range snapshot.Tables["task_dead_letters"] {
		if row["scope"] == "knowledge_base" {
			row["scope"] = "library"
		}
	}
	return nil
}

// The current schema deliberately discards old queue messages. Compare the
// remaining backup data exactly across that migration, including dead letters.
func projectQueueMigration(expected, actual *Manifest) (*Manifest, *Manifest) {
	if _, old := expected.Tables["task_jobs"]; !old {
		return expected, actual
	}
	if _, stillOld := actual.Tables["task_jobs"]; stillOld {
		return expected, actual
	}
	current, present := actual.Tables["task_pending_ops"]
	if !present || current.Rows != 0 || len(current.Data) != 0 {
		return expected, actual
	}
	left, right := *expected, *actual
	left.Tables = map[string]TableManifest{}
	right.Tables = map[string]TableManifest{}
	for name, row := range expected.Tables {
		if name != "task_jobs" {
			left.Tables[name] = row
		}
	}
	for name, row := range actual.Tables {
		if name != "task_pending_ops" {
			right.Tables[name] = row
		}
	}
	// Historical scope conversion is limited to this backup projection. Runtime
	// payload readers never accept the old library field.
	if archive, ok := left.Tables["task_dead_letters"]; ok && archive.Data != nil {
		archive.Data = canonicalRows("task_dead_letters", archive.Data)
		for _, row := range archive.Data {
			if row["scope"] == "knowledge_base" {
				row["scope"] = "library"
			}
		}
		raw, _ := json.Marshal(archive.Data)
		archive.SHA256 = sum(raw)
		left.Tables["task_dead_letters"] = archive
	}
	left.Files = map[string][]File{}
	right.Files = map[string][]File{}
	for group, files := range expected.Files {
		left.Files[group] = []File{}
		for _, file := range files {
			if group == "vault" && file.Path == ".reliquary/data/task_jobs.json" {
				continue
			}
			if group == "vault" && file.Path == ".reliquary/data/task_dead_letters.json" {
				if rows := left.Tables["task_dead_letters"].Data; rows != nil {
					raw, _ := json.MarshalIndent(rows, "", "  ")
					raw = append(raw, '\n')
					file.SHA256 = sum(raw)
					file.Size = int64(len(raw))
				}
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	for group, files := range actual.Files {
		right.Files[group] = []File{}
		for _, file := range files {
			if group == "vault" && file.Path == ".reliquary/data/task_pending_ops.json" {
				continue
			}
			right.Files[group] = append(right.Files[group], file)
		}
	}
	return &left, &right
}

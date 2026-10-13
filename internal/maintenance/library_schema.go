package maintenance

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"reliquary/internal/types"
)

func hasLibrarySchema(rows []map[string]any) bool {
	for _, row := range rows {
		if _, ok := row["template"]; ok {
			return true
		}
	}
	return false
}

// Materialize only the documented 9 to 10 migration. Reference rows supply
// generated IDs and timestamps; all configuration values come from defaults
// or the original snapshot, never from the target's mutable configuration.
func migrateLibrarySnapshot(snapshot *Snapshot, reference []map[string]any) error {
	if hasLibrarySchema(snapshot.Tables["libraries"]) {
		return nil
	}
	roles := map[string]map[string]any{}
	for _, row := range reference {
		if stringValue(row["system_role"]) != "" {
			roles[fmt.Sprint(row["space_id"])+":"+stringValue(row["system_role"])] = row
		}
	}
	ids := map[string]map[string]string{}
	defaults := types.SystemLibraryDefaults()
	original := snapshot.Tables["libraries"]
	libraries := []map[string]any{}
	contents := []map[string]any{}
	relations := []map[string]any{}
	embeddings := map[string]any{}
	for _, old := range original {
		space := fmt.Sprint(old["space_id"])
		row := map[string]any{}
		for k, v := range old {
			row[k] = v
		}
		row["system_role"] = ""
		row["template"] = "own"
		row["directory"] = fmt.Sprintf("archived-%x", md5.Sum([]byte(fmt.Sprint(old["id"]))))
		row["review_criteria"] = ""
		row["source_evidence_rule"] = ""
		row["feed_enabled"] = false
		if old["deleted_at"] != nil {
			libraries = append(libraries, row)
			continue
		}
		ids[space] = map[string]string{}
		embedding := old["embedding_model_id"]
		if !nonempty(embedding) {
			embedding = ""
		}
		if tiers, ok := old["model_tiers"].(map[string]any); ok && nonempty(tiers["embedding"]) {
			embedding = tiers["embedding"]
		}
		embeddings[space] = embedding
		for _, d := range defaults {
			next := map[string]any{}
			for k, v := range row {
				next[k] = v
			}
			next["system_role"] = d.SystemRole
			next["template"] = d.Template
			next["directory"] = d.Directory
			next["review_criteria"] = d.ReviewCriteria
			next["source_evidence_rule"] = d.SourceEvidenceRule
			next["feed_enabled"] = d.FeedEnabled
			next["name"] = d.Name
			next["description"] = d.Description
			if d.SystemRole == "institutional" {
				if name := stringValue(old["name"]); name != "Library" && name != "库" {
					next["name"] = old["name"]
				}
				if nonempty(old["description"]) {
					next["description"] = old["description"]
				}
			} else if reference != nil {
				actual, ok := roles[space+":"+d.SystemRole]
				if !ok {
					return fmt.Errorf("missing migrated system role %s", d.SystemRole)
				}
				next["id"] = actual["id"]
				next["created_at"] = actual["created_at"]
				next["updated_at"] = actual["updated_at"]
			} else {
				next["id"] = uuid.NewString()
				now := time.Now().UTC().Format(time.RFC3339Nano)
				next["created_at"] = now
				next["updated_at"] = now
			}
			id := fmt.Sprint(next["id"])
			ids[space][d.SystemRole] = id
			libraries = append(libraries, next)
			for _, c := range d.ContentTypes {
				contents = append(contents, map[string]any{"library_id": id, "name": c.Name, "position": c.Position, "enabled": c.Enabled, "extract_graph": c.ExtractGraph})
			}
			for _, r := range d.RelationTypes {
				relations = append(relations, map[string]any{"library_id": id, "name": r.Name, "position": r.Position, "enabled": r.Enabled})
			}
		}
	}
	snapshot.Tables["libraries"] = libraries
	snapshot.Tables["library_content_types"] = contents
	snapshot.Tables["library_relation_types"] = relations
	for _, space := range snapshot.Tables["spaces"] {
		space["embedding_model_id"] = ""
		if v, ok := embeddings[fmt.Sprint(space["id"])]; ok {
			space["embedding_model_id"] = v
		}
	}
	entryLibrary := map[string]string{}
	for _, entry := range snapshot.Tables["entries"] {
		if byRole := ids[fmt.Sprint(entry["space_id"])]; byRole != nil {
			id := byRole[types.SystemRoleForSource(stringValue(entry["source_kind"]))]
			entry["library_id"] = id
			entryLibrary[fmt.Sprint(entry["id"])] = id
		}
	}
	for _, table := range []string{"chunks", "embeddings", "article_embeddings", "image_embeddings", "entry_repost_sources"} {
		for _, row := range snapshot.Tables[table] {
			if id := entryLibrary[fmt.Sprint(row["entry_id"])]; id != "" {
				row["library_id"] = id
			}
		}
	}
	for _, row := range snapshot.Tables["task_pending_ops"] {
		if payload, ok := row["payload"].(map[string]any); ok {
			if _, present := payload["library_id"]; present {
				if id := entryLibrary[fmt.Sprint(payload["knowledge_id"])]; id != "" {
					payload["library_id"] = id
				}
			}
		}
	}
	for _, rows := range snapshot.Tables {
		sortRows(rows)
	}
	return nil
}

func projectLibraryManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	found := false
	for _, column := range actual.Tables["libraries"].Columns {
		if column == "template" {
			found = true
		}
	}
	old := false
	for _, column := range expected.Tables["libraries"].Columns {
		if column == "template" {
			old = true
		}
	}
	if !found || old {
		return expected, actual
	}
	snapshot := &Snapshot{Tables: map[string][]map[string]any{}}
	// Deep copies preserve the caller's original evidence and JSON number types.
	raw, _ := json.Marshal(expected.Tables)
	var copied map[string]TableManifest
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&copied) != nil {
		return expected, actual
	}
	for table, entry := range copied {
		if entry.Data == nil && expected.Tables[table].Data != nil {
			entry.Data = []map[string]any{}
		}
		snapshot.Tables[table] = entry.Data
	}
	if migrateLibrarySnapshot(snapshot, actual.Tables["libraries"].Data) != nil {
		return expected, actual
	}
	left := *expected
	left.Tables = map[string]TableManifest{}
	left.Files = map[string][]File{}
	for table, entry := range copied {
		left.Tables[table] = entry
	}
	affected := map[string]bool{"libraries": true, "spaces": true, "entries": true, "chunks": true, "embeddings": true, "article_embeddings": true, "image_embeddings": true, "entry_repost_sources": true, "task_pending_ops": true, "library_content_types": true, "library_relation_types": true}
	for table := range affected {
		if _, present := actual.Tables[table]; !present {
			continue
		}
		entry, exists := left.Tables[table]
		if !exists {
			entry = actual.Tables[table]
		}
		entry.Columns = actual.Tables[table].Columns
		entry.Data = snapshot.Tables[table]
		if !entry.Regenerated {
			entry.Rows = int64(len(entry.Data))
			raw, _ := json.Marshal(canonicalRows(table, entry.Data))
			entry.SHA256 = sum(raw)
		}
		left.Tables[table] = entry
	}
	for group, files := range expected.Files {
		for _, file := range files {
			table := strings.TrimSuffix(path.Base(file.Path), ".json")
			if affected[table] && strings.Contains(file.Path, ".reliquary/") {
				before, _ := json.MarshalIndent(expected.Tables[table].Data, "", "  ")
				before = append(before, '\n')
				if file.SHA256 == sum(before) {
					after, _ := json.MarshalIndent(snapshot.Tables[table], "", "  ")
					after = append(after, '\n')
					file.SHA256 = sum(after)
					file.Size = int64(len(after))
				}
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	for _, table := range []string{"library_content_types", "library_relation_types"} {
		raw, _ := json.MarshalIndent(snapshot.Tables[table], "", "  ")
		raw = append(raw, '\n')
		for group, files := range actual.Files {
			for _, file := range files {
				if path.Base(file.Path) == table+".json" && file.SHA256 == sum(raw) && file.Size == int64(len(raw)) {
					left.Files[group] = append(left.Files[group], file)
				}
			}
		}
	}
	return &left, actual
}

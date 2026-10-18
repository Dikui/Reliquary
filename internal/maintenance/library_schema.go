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

var systemLibraryLabelMigration = map[string][2]string{
	"institutional": {"机构发布", "官方信息"},
	"opinions":      {"人物观点", "媒体信息"},
	"references":    {"参考文献", "参考文档"},
	"mine":          {"我的资料", "个人产出"},
	"ai-reports":    {"AI 报告", "AI产出"},
}

var systemLibraryDirectoryMigration = map[string]string{
	"institutional": "official",
	"opinions":      "media",
	"mine":          "personal",
	"ai-reports":    "ai-output",
}

func legacySystemLibraryRole(role string) string {
	for old, current := range systemLibraryDirectoryMigration {
		if role == current {
			return old
		}
	}
	return role
}

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
			roles[fmt.Sprint(row["space_id"])+":"+legacySystemLibraryRole(stringValue(row["system_role"]))] = row
		}
	}
	ids := map[string]map[string]string{}
	defaults := types.SystemLibraryDefaults()
	// The 9 to 10 projection must retain the text written by frozen 000010,
	// independently of later corrections to the current initialization copy.
	for _, library := range defaults {
		library.SystemRole = legacySystemLibraryRole(library.SystemRole)
		library.Directory = legacySystemLibraryRole(library.Directory)
		library.Name = systemLibraryLabelMigration[library.SystemRole][0]
		if library.SystemRole == "opinions" {
			library.Description = "外部个人的观点：专家、分析师、KOL 的评论与文章。"
		}
		if library.SystemRole == "ai-reports" {
			library.ReviewCriteria = "不审核、不处理，只建索引。标题、Markdown 正文必填；关联资料、已存在的实体与主题可选。"
		}
	}
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
			role := "institutional"
			switch entry["source_kind"] {
			case "mine":
				role = "mine"
			case "report":
				role = "ai-reports"
			}
			id := byRole[role]
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
				if id := entryLibrary[legacyTaskEntryID(payload)]; id != "" {
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

func projectLibraryLabelsSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 26 || targetVersion < 26 {
		return
	}
	for _, row := range snapshot.Tables["libraries"] {
		if labels, ok := systemLibraryLabelMigration[stringValue(row["system_role"])]; ok && row["name"] == labels[0] {
			row["name"] = labels[1]
		}
	}
}

func projectLibraryLabelsManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 26 || actual.SchemaVersion < 26 {
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
		copy := make(map[string]any, len(row))
		for field, value := range row {
			copy[field] = value
		}
		rows = append(rows, copy)
	}
	projectLibraryLabelsSnapshot(&Snapshot{schemaVersion: expected.SchemaVersion, Tables: map[string][]map[string]any{"libraries": rows}}, actual.SchemaVersion)
	item.Data = rows
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
			if file.Path == ".reliquary/config/libraries.json" && file.SHA256 == sum(before) && file.Size == int64(len(before)) {
				file.SHA256, file.Size = sum(after), int64(len(after))
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	return &left, actual
}

// Version 27 changes only built-in identities and their default archive paths.
// Source URLs, custom directories, user JSON and historical evidence stay opaque.
func projectLibraryDirectoriesSnapshot(snapshot *Snapshot, targetVersion int64) map[string]string {
	if snapshot.schemaVersion >= 27 || targetVersion < 27 {
		return nil
	}
	directories := map[string][2]string{}
	for _, row := range snapshot.Tables["libraries"] {
		old := stringValue(row["system_role"])
		if current, ok := systemLibraryDirectoryMigration[old]; ok {
			row["system_role"] = current
			if row["directory"] == old {
				row["directory"] = current
				directories[stringValue(row["id"])] = [2]string{old, current}
			}
		}
	}
	files := map[string]string{}
	for _, row := range snapshot.Tables["entries"] {
		directory, ok := directories[stringValue(row["library_id"])]
		old := stringValue(row["vault_path"])
		if !ok || !strings.HasPrefix(old, directory[0]+"/") {
			continue
		}
		current := directory[1] + strings.TrimPrefix(old, directory[0])
		row["vault_path"] = current
		if row["source"] == old {
			row["source"] = current
		}
		files[directory[0]] = directory[1]
	}
	return files
}

func projectLibraryDirectoriesManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 27 || actual.SchemaVersion < 27 {
		return expected, actual
	}
	snapshot := &Snapshot{schemaVersion: expected.SchemaVersion, Tables: map[string][]map[string]any{}}
	for _, table := range []string{"libraries", "entries"} {
		item, ok := expected.Tables[table]
		if !ok || item.Data == nil {
			return expected, actual
		}
		raw, _ := json.Marshal(canonicalRows(table, item.Data))
		if item.SHA256 != sum(raw) {
			return expected, actual
		}
		rows := make([]map[string]any, 0, len(item.Data))
		for _, row := range item.Data {
			copy := make(map[string]any, len(row))
			for field, value := range row {
				copy[field] = value
			}
			rows = append(rows, copy)
		}
		snapshot.Tables[table] = rows
	}
	directories := projectLibraryDirectoriesSnapshot(snapshot, actual.SchemaVersion)
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	for table, entry := range expected.Tables {
		left.Tables[table] = entry
	}
	for table, rows := range snapshot.Tables {
		item := left.Tables[table]
		item.Data = rows
		raw, _ := json.Marshal(canonicalRows(table, rows))
		item.SHA256 = sum(raw)
		left.Tables[table] = item
	}
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		for _, file := range files {
			if group == "vault" {
				for old, current := range directories {
					if strings.HasPrefix(file.Path, old+"/") {
						file.Path = current + strings.TrimPrefix(file.Path, old)
						break
					}
				}
				for table, rows := range snapshot.Tables {
					if file.Path != ".reliquary/config/"+table+".json" && file.Path != ".reliquary/data/"+table+".json" {
						continue
					}
					before, _ := json.MarshalIndent(expected.Tables[table].Data, "", "  ")
					before = append(before, '\n')
					if file.SHA256 == sum(before) && file.Size == int64(len(before)) {
						after, _ := json.MarshalIndent(rows, "", "  ")
						after = append(after, '\n')
						file.SHA256, file.Size = sum(after), int64(len(after))
					}
				}
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	return &left, actual
}

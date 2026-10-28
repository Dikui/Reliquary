package maintenance

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
)

func catalogSchemaRule(table string, rule Rule, version int64) Rule {
	if version >= 33 {
		return rule
	}
	if table == "entities" || table == "entity_aliases" || table == "topics" {
		rule.Sidecar = strings.TrimSpace(strings.ReplaceAll(" "+rule.Sidecar+" ", " library_id ", " "))
	}
	if table == "topics" && version >= 15 {
		rule.Sidecar += " library_ids"
	}
	return rule
}

func catalogCopy(row map[string]any) map[string]any {
	result := make(map[string]any, len(row))
	for key, value := range row {
		result[key] = value
	}
	return result
}

func catalogID(kind, id, library string) string {
	digest := md5.Sum([]byte(kind + ":" + id + ":" + library))
	return hex.EncodeToString(digest[:])
}

type catalogMap map[string]map[string]string

func (m catalogMap) owners(id string) []string {
	owners := make([]string, 0, len(m[id]))
	for library := range m[id] {
		owners = append(owners, library)
	}
	sort.Strings(owners)
	return owners
}
func (m catalogMap) remap(id, library string) string {
	if next := m[id][library]; next != "" {
		return next
	}
	return id
}

// Match migration 33's deterministic split without changing article bodies or
// user metadata. Historical audit records keep their original IDs and payloads.
func projectCatalogSnapshot(snapshot *Snapshot, targetVersion int64) error {
	if snapshot.schemaVersion >= 33 || targetVersion < 33 {
		return nil
	}
	entries := map[string]string{}
	for _, row := range snapshot.Tables["entries"] {
		entries[stringValue(row["id"])] = stringValue(row["library_id"])
	}
	libraries := map[string][]string{}
	for _, row := range snapshot.Tables["libraries"] {
		space := fmt.Sprint(row["space_id"])
		libraries[space] = append(libraries[space], stringValue(row["id"]))
	}
	scopes := catalogMap{}
	edges := map[string][]string{}
	connect := func(a, b string) {
		if a != "" && b != "" {
			edges[a] = append(edges[a], b)
			edges[b] = append(edges[b], a)
		}
	}
	for _, row := range snapshot.Tables["entities"] {
		scopes[stringValue(row["id"])] = map[string]string{}
		connect(stringValue(row["id"]), stringValue(row["merged_into_id"]))
	}
	for _, row := range snapshot.Tables["entity_merge_records"] {
		connect(stringValue(row["keep_entity_id"]), stringValue(row["merge_entity_id"]))
	}
	for _, row := range snapshot.Tables["entity_merge_suggestions"] {
		connect(stringValue(row["new_entity_id"]), stringValue(row["candidate_entity_id"]))
	}
	type owner struct{ id, library string }
	queue := []owner{}
	seed := func(id, library string) {
		if library != "" && scopes[id] != nil && scopes[id][library] == "" {
			scopes[id][library] = id
			queue = append(queue, owner{id, library})
		}
	}
	for _, row := range snapshot.Tables["entry_entities"] {
		seed(stringValue(row["entity_id"]), entries[stringValue(row["entry_id"])])
	}
	for _, table := range []string{"relations", "relation_suppressions"} {
		for _, row := range snapshot.Tables[table] {
			seed(stringValue(row["subject_id"]), stringValue(row["library_id"]))
			seed(stringValue(row["object_id"]), stringValue(row["library_id"]))
		}
	}
	for _, row := range snapshot.Tables["entity_merge_suggestions"] {
		library := entries[stringValue(row["entry_id"])]
		seed(stringValue(row["new_entity_id"]), library)
		seed(stringValue(row["candidate_entity_id"]), library)
	}
	for i := 0; i < len(queue); i++ {
		item := queue[i]
		for _, next := range edges[item.id] {
			seed(next, item.library)
		}
	}
	for _, row := range snapshot.Tables["entities"] {
		id := stringValue(row["id"])
		if len(scopes[id]) == 0 {
			for _, library := range libraries[fmt.Sprint(row["space_id"])] {
				scopes[id][library] = id
			}
		}
		if len(scopes[id]) == 0 {
			return fmt.Errorf("library isolation needs an existing library for entity %s", id)
		}
		for i, library := range scopes.owners(id) {
			if i > 0 {
				scopes[id][library] = catalogID("entity", id, library)
			}
		}
	}
	topics := catalogMap{}
	for _, row := range snapshot.Tables["topics"] {
		id := stringValue(row["id"])
		topics[id] = map[string]string{}
		var configured []string
		raw, err := json.Marshal(row["library_ids"])
		if err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &configured); err != nil {
			return fmt.Errorf("topic %s library scope: %w", id, err)
		}
		for _, library := range libraries[fmt.Sprint(row["space_id"])] {
			if len(configured) == 0 || slices.Contains(configured, library) {
				topics[id][library] = id
			}
		}
	}
	for _, row := range snapshot.Tables["entry_topics"] {
		id := stringValue(row["topic_id"])
		if topics[id] != nil {
			topics[id][entries[stringValue(row["entry_id"])]] = id
		}
	}
	for id := range topics {
		if len(topics[id]) == 0 {
			return fmt.Errorf("library isolation needs an existing library for topic %s", id)
		}
		for i, library := range topics.owners(id) {
			if i > 0 {
				topics[id][library] = catalogID("topic", id, library)
			}
		}
	}
	rows := []map[string]any{}
	for _, row := range snapshot.Tables["entities"] {
		id := stringValue(row["id"])
		for _, library := range scopes.owners(id) {
			copy := catalogCopy(row)
			copy["id"], copy["library_id"] = scopes[id][library], library
			copy["merged_into_id"] = scopes.remap(stringValue(row["merged_into_id"]), library)
			rows = append(rows, copy)
		}
	}
	snapshot.Tables["entities"] = rows
	rows = []map[string]any{}
	for _, row := range snapshot.Tables["entity_aliases"] {
		id := stringValue(row["entity_id"])
		for _, library := range scopes.owners(id) {
			copy := catalogCopy(row)
			copy["entity_id"], copy["library_id"] = scopes[id][library], library
			if copy["entity_id"] != id {
				copy["id"] = catalogID("alias", stringValue(row["id"]), library)
			}
			rows = append(rows, copy)
		}
	}
	snapshot.Tables["entity_aliases"] = rows
	rows = []map[string]any{}
	for _, row := range snapshot.Tables["topics"] {
		id := stringValue(row["id"])
		for _, library := range topics.owners(id) {
			copy := catalogCopy(row)
			copy["id"], copy["library_id"] = topics[id][library], library
			delete(copy, "library_ids")
			rows = append(rows, copy)
		}
	}
	snapshot.Tables["topics"] = rows
	for _, row := range snapshot.Tables["entry_entities"] {
		row["entity_id"] = scopes.remap(stringValue(row["entity_id"]), entries[stringValue(row["entry_id"])])
	}
	for _, row := range snapshot.Tables["entry_topics"] {
		row["topic_id"] = topics.remap(stringValue(row["topic_id"]), entries[stringValue(row["entry_id"])])
	}
	relationLibraries := map[string]string{}
	for _, table := range []string{"relations", "relation_suppressions"} {
		for _, row := range snapshot.Tables[table] {
			library := stringValue(row["library_id"])
			row["subject_id"] = scopes.remap(stringValue(row["subject_id"]), library)
			row["object_id"] = scopes.remap(stringValue(row["object_id"]), library)
			if table == "relations" {
				relationLibraries[stringValue(row["id"])] = library
			}
		}
	}
	rows = []map[string]any{}
	for _, row := range snapshot.Tables["follows"] {
		mappings := scopes
		if row["target_type"] == "topic" {
			mappings = topics
		}
		id := stringValue(row["target_id"])
		for _, library := range mappings.owners(id) {
			copy := catalogCopy(row)
			copy["target_id"] = mappings[id][library]
			rows = append(rows, copy)
		}
	}
	snapshot.Tables["follows"] = rows
	rows = []map[string]any{}
	for _, row := range snapshot.Tables["entity_merge_records"] {
		id := stringValue(row["keep_entity_id"])
		for _, library := range scopes.owners(id) {
			copy := catalogCopy(row)
			copy["keep_entity_id"] = scopes.remap(id, library)
			copy["merge_entity_id"] = scopes.remap(stringValue(row["merge_entity_id"]), library)
			if copy["keep_entity_id"] != id {
				copy["id"] = catalogID("merge", stringValue(row["id"]), library)
			}
			rows = append(rows, copy)
		}
	}
	snapshot.Tables["entity_merge_records"] = rows
	for _, row := range snapshot.Tables["entity_merge_suggestions"] {
		library := entries[stringValue(row["entry_id"])]
		row["new_entity_id"] = scopes.remap(stringValue(row["new_entity_id"]), library)
		row["candidate_entity_id"] = scopes.remap(stringValue(row["candidate_entity_id"]), library)
	}
	for _, row := range snapshot.Tables["topic_backfill_jobs"] {
		if len(topics[stringValue(row["topic_id"])]) > 1 && slices.Contains([]string{"pending", "running", "failed"}, stringValue(row["status"])) {
			row["status"] = "cancelled"
		}
	}
	var remapJSON func(any, string) any
	remapJSON = func(value any, library string) any {
		switch value := value.(type) {
		case string:
			if next := scopes[value][library]; next != "" {
				return next
			}
			return topics.remap(value, library)
		case []any:
			result := make([]any, len(value))
			for i, v := range value {
				result[i] = remapJSON(v, library)
			}
			return result
		case map[string]any:
			result := make(map[string]any, len(value))
			for key, v := range value {
				result[key] = remapJSON(v, library)
			}
			return result
		default:
			return value
		}
	}
	for _, row := range snapshot.Tables["entry_processing_outputs"] {
		row["relations"] = remapJSON(row["relations"], stringValue(row["library_id"]))
	}
	for _, row := range snapshot.Tables["entry_segment_runs"] {
		library := entries[stringValue(row["entry_id"])]
		row["plan"] = remapJSON(row["plan"], library)
		row["result"] = remapJSON(row["result"], library)
	}
	for _, row := range snapshot.Tables["relation_sources"] {
		row["evidence"] = remapJSON(row["evidence"], relationLibraries[stringValue(row["relation_id"])])
	}
	for table := range snapshot.Tables {
		sortRows(snapshot.Tables[table])
	}
	return nil
}

func projectCatalogManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 33 || actual.SchemaVersion < 33 {
		return expected, actual
	}
	// Keep mismatched evidence visible instead of legitimizing altered data.
	tables := make(map[string]TableManifest, len(expected.Tables))
	snapshot := &Snapshot{schemaVersion: expected.SchemaVersion, Tables: map[string][]map[string]any{}}
	for table, item := range expected.Tables {
		tables[table] = item
		if _, backupOnly := BackupOnlyTables[table]; backupOnly || item.Regenerated {
			continue
		}
		if item.Rows > 0 && item.Data == nil {
			return expected, actual
		}
		raw, _ := json.Marshal(canonicalRows(table, item.Data))
		if sum(raw) != item.SHA256 {
			return expected, actual
		}
		raw, _ = json.Marshal(item.Data)
		var rows []map[string]any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&rows) != nil {
			return expected, actual
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		snapshot.Tables[table] = rows
	}
	if projectCatalogSnapshot(snapshot, actual.SchemaVersion) != nil {
		return expected, actual
	}
	left := *expected
	left.Tables = tables
	for table, rows := range snapshot.Tables {
		item := tables[table]
		if item.Regenerated {
			continue
		}
		before := expected.Tables[table]
		item.Data = rows
		item.Rows = int64(len(rows))
		item.Columns = append([]string{}, before.Columns...)
		if table == "entities" || table == "entity_aliases" || table == "topics" {
			item.Columns = slices.DeleteFunc(item.Columns, func(col string) bool { return col == "library_ids" })
			item.Columns = append(item.Columns, "library_id")
			sort.Strings(item.Columns)
		}
		data, _ := json.Marshal(canonicalRows(table, rows))
		item.SHA256 = sum(data)
		left.Tables[table] = item
	}
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		for _, file := range files {
			table := strings.TrimSuffix(path.Base(file.Path), ".json")
			if before, ok := expected.Tables[table]; ok && strings.HasPrefix(file.Path, ".reliquary/") {
				data, _ := json.MarshalIndent(before.Data, "", "  ")
				data = append(data, '\n')
				if file.SHA256 == sum(data) && file.Size == int64(len(data)) {
					data, _ = json.MarshalIndent(left.Tables[table].Data, "", "  ")
					data = append(data, '\n')
					file.SHA256, file.Size = sum(data), int64(len(data))
				}
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	return &left, actual
}

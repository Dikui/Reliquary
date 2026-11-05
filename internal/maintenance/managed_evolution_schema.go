package maintenance

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

var managedEvolutionDefaults = map[string]map[string]any{
	"libraries":              {"product_library_ids": []any{}, "document_relation_types": []any{"references", "specification", "test_report", "comparison"}},
	"library_relation_types": {"fields": []any{}},
	"relations":              {"attributes": map[string]any{}},
	"entry_entities":         {"relation_type": "references"},
}

func projectManagedEvolutionSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 38 || targetVersion < 38 {
		return
	}
	projectEntityDefaultsSnapshot(snapshot, targetVersion, 38, managedEvolutionDefaults)
	for _, name := range []string{"entity_attribute_versions", "entity_reorganizations", "entity_attachments"} {
		if _, exists := snapshot.Tables[name]; !exists {
			snapshot.Tables[name] = []map[string]any{}
		}
	}
	managed := map[string]bool{}
	for _, library := range snapshot.Tables["libraries"] {
		managed[fmt.Sprint(library["id"])] = library["entity_mode"] == "managed"
	}
	existing := map[string]bool{}
	for _, version := range snapshot.Tables["entity_attribute_versions"] {
		existing[fmt.Sprint(version["entity_id"])] = true
	}
	for _, entity := range snapshot.Tables["entities"] {
		id := fmt.Sprint(entity["id"])
		if !managed[fmt.Sprint(entity["library_id"])] || existing[id] {
			continue
		}
		hash := md5.Sum([]byte("entity-attributes:" + id))
		versionID, _ := uuid.FromBytes(hash[:])
		snapshot.Tables["entity_attribute_versions"] = append(snapshot.Tables["entity_attribute_versions"], map[string]any{
			"id": versionID.String(), "space_id": entity["space_id"], "entity_id": entity["id"], "revision": entity["revision"],
			"effective_at": entity["updated_at"], "recorded_at": entity["updated_at"], "attributes": entity["attributes"],
		})
	}
}

func projectManagedEvolutionManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 38 || actual.SchemaVersion < 38 {
		return expected, actual
	}
	expected, actual = projectEntityDefaultsManifest(expected, actual, 38, managedEvolutionDefaults)
	snapshot := &Snapshot{schemaVersion: expected.SchemaVersion, Tables: map[string][]map[string]any{}}
	for _, name := range []string{"libraries", "entities"} {
		table := expected.Tables[name]
		encoded, _ := json.Marshal(canonicalRows(name, table.Data))
		if table.Rows > 0 && (table.Data == nil || sum(encoded) != table.SHA256) {
			return expected, actual
		}
		snapshot.Tables[name] = table.Data
	}
	projectManagedEvolutionSnapshot(snapshot, 38)
	for _, name := range []string{"entity_attribute_versions", "entity_reorganizations", "entity_attachments"} {
		if _, exists := expected.Tables[name]; exists {
			continue
		}
		registration := PersistentTables[name]
		columns := []string{}
		for column := range registration.Columns.Columns() {
			columns = append(columns, column)
		}
		sort.Strings(columns)
		rows := canonicalRows(name, snapshot.Tables[name])
		encoded, _ := json.Marshal(rows)
		expected.Tables[name] = TableManifest{Rows: int64(len(rows)), Columns: columns, Data: rows, SHA256: sum(encoded)}
		pretty, _ := json.MarshalIndent(rows, "", "  ")
		pretty = append(pretty, '\n')
		path := ".reliquary/" + registration.File
		for _, file := range actual.Files["vault"] {
			if file.Path == path {
				expected.Files["vault"] = append(expected.Files["vault"], File{Path: path, SHA256: sum(pretty), Size: int64(len(pretty))})
			}
		}
	}
	return expected, actual
}

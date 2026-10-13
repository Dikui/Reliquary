package maintenance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PersistentTable is the single registration for new portable SQL tables.
// Existing tables retain their established paths and column classifications.
// SQL migrations remain responsible for constraints and schema changes.
type PersistentTable struct {
	Columns Rule     `json:"columns"`
	File    string   `json:"file"`
	After   []string `json:"after"`
	Since   int64    `json:"since_schema_version"`
}

const RegistryVersion = 1

var PersistentTables = map[string]PersistentTable{
	"libraries": {
		Columns: Rule{Configuration: "system_role template directory review_criteria source_evidence_rule feed_enabled model_tiers id name description space_id chunking_config embedding_model_id summary_model_id created_at updated_at deleted_at type indexing_strategy creator_id"},
		File:    "config/libraries.json", After: []string{"spaces", "users"}, Since: 0,
	},
	"library_content_types": {
		Columns: Rule{Configuration: "library_id name position enabled extract_graph"},
		File:    "data/library_content_types.json", After: []string{"libraries"}, Since: 10,
	},
	"library_relation_types": {
		Columns: Rule{Configuration: "library_id name position enabled"},
		File:    "data/library_relation_types.json", After: []string{"libraries"}, Since: 10,
	},
}

// BackupOnlyTables must be explicitly classified when introduced. They are
// preserved by PostgreSQL backups, never by portable replicas.
var BackupOnlyTables = map[string]Rule{}

func init() {
	for table, registration := range PersistentTables {
		Rules[table] = registration.Columns
	}
	for table, columns := range BackupOnlyTables {
		Rules[table] = columns
	}
}

func registeredPath(table string) (string, bool) {
	registration, ok := PersistentTables[table]
	return registration.File, ok
}

func registryBytes(snapshot *Snapshot) ([]byte, error) {
	registrations := map[string]PersistentTable{}
	for table, registration := range PersistentTables {
		if _, ok := snapshot.Tables[table]; ok {
			registrations[table] = registration
		}
	}
	data, err := json.MarshalIndent(struct {
		Version int                        `json:"format_version"`
		Tables  map[string]PersistentTable `json:"tables"`
	}{RegistryVersion, registrations}, "", "  ")
	return append(data, '\n'), err
}

// registeredImportOrder resolves new tables automatically around the existing
// dependency order. Missing dependencies and cycles fail before any SQL writes.
func registeredImportOrder(base []string, tables map[string][]map[string]any) ([]string, error) {
	pending := map[string]PersistentTable{}
	for table, registration := range PersistentTables {
		if _, ok := tables[table]; ok {
			pending[table] = registration
		}
	}
	seen := map[string]bool{}
	result := []string{}
	drain := func() {
		for {
			ready := []string{}
			for table, registration := range pending {
				ok := true
				for _, dependency := range registration.After {
					if !seen[dependency] {
						ok = false
					}
				}
				if ok {
					ready = append(ready, table)
				}
			}
			if len(ready) == 0 {
				return
			}
			sort.Strings(ready)
			for _, table := range ready {
				result = append(result, table)
				seen[table] = true
				delete(pending, table)
			}
		}
	}
	drain()
	for _, table := range base {
		if _, registered := PersistentTables[table]; registered {
			continue
		}
		result = append(result, table)
		seen[table] = true
		drain()
	}
	if len(pending) > 0 {
		names := []string{}
		for table := range pending {
			names = append(names, table)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("persistent table dependency missing or cyclic: %s", strings.Join(names, ", "))
	}
	return result, nil
}

// RequireCurrentVaultReplica keeps the public file rebuild separate from the
// historical SQL backup readers, whose version mappings remain available.
func RequireCurrentVaultReplica(root string) error {
	data, err := os.ReadFile(filepath.Join(root, ".reliquary", "manifest.json"))
	if err != nil {
		return err
	}
	var manifest ReplicaManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if manifest.RegistryVersion != RegistryVersion {
		return fmt.Errorf("legacy file-library replica has no supported registry; restore with its matching release and re-ingest into the current per-library format")
	}
	snapshot, err := Load(root)
	if err != nil {
		return err
	}
	directories := map[string]string{}
	for _, library := range snapshot.Tables["libraries"] {
		directory, _ := library["directory"].(string)
		if directory == "" {
			return fmt.Errorf("legacy file-library replica has no library directories; use its matching release")
		}
		directories[fmt.Sprint(library["id"])] = directory
	}
	for _, entry := range snapshot.Tables["entries"] {
		path, _ := entry["vault_path"].(string)
		if path != "" && filepath.Dir(filepath.FromSlash(path)) != directories[fmt.Sprint(entry["library_id"])] {
			return fmt.Errorf("legacy file-library replica path %s does not match its library directory", path)
		}
	}
	return nil
}

func registryManifestFileMatches(file File, manifest *Manifest) bool {
	snapshot := &Snapshot{Tables: map[string][]map[string]any{}}
	for table := range manifest.Tables {
		snapshot.Tables[table] = nil
	}
	data, err := registryBytes(snapshot)
	return err == nil && file.SHA256 == sum(data) && file.Size == int64(len(data))
}

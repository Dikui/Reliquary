package maintenance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reliquary/internal/vaultpath"

	"gorm.io/gorm"
)

// Load refuses any corrupt replica before opening a database transaction.
func Load(root string) (*Snapshot, error) {
	directory, err := SafePath(root, ".reliquary")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var manifest ReplicaManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if !supportedFormat(manifest.FormatVersion) {
		return nil, fmt.Errorf("unsupported replica format %d", manifest.FormatVersion)
	}
	if manifest.RegistryVersion < 0 || manifest.RegistryVersion > RegistryVersion {
		return nil, fmt.Errorf("unsupported persistent table registry format %d", manifest.RegistryVersion)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if active, present := fields["active_uploads"]; present && bytes.Equal(bytes.TrimSpace(active), []byte("null")) {
		return nil, errors.New("active upload inventory must be an array, including when empty")
	}
	if err := validateActiveUploads(root, &manifest); err != nil {
		return nil, err
	}
	snapshot := &Snapshot{FormatVersion: manifest.FormatVersion, storedFormat: manifest.FormatVersion, registryVersion: manifest.RegistryVersion, schemaVersion: manifest.SchemaVersion, Tables: map[string][]map[string]any{}, Required: manifest.Required, ActiveUploads: manifest.ActiveUploads, JSONNulls: manifest.JSONNulls}
	for _, file := range manifest.VaultFiles {
		path, err := SafePath(root, file.Path)
		if err != nil {
			return nil, err
		}
		if vaultpath.UserMetadata(file.Path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || sum(data) != file.SHA256 || int64(len(data)) != file.Size {
			return nil, fmt.Errorf("vault file checksum mismatch: %s", file.Path)
		}
	}
	for _, file := range manifest.Files {
		path, err := SafePath(directory, file.Path)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if sum(data) != file.SHA256 || int64(len(data)) != file.Size {
			return nil, fmt.Errorf("replica file checksum mismatch: %s", file.Path)
		}
		if file.Path == "config/required_credentials.json" {
			if _, valid := decodeRequirementCopy(data); !valid {
				return nil, errors.New("portable credential reminder checksum mismatch")
			}
			continue
		}
		if file.Path == "operation_logs.jsonl" {
			rows := []map[string]any{}
			scanner := bufio.NewScanner(bytes.NewReader(data))
			scanner.Buffer(make([]byte, 65536), 64*1024*1024)
			for scanner.Scan() {
				row, err := jsonRow(scanner.Bytes())
				if err != nil {
					return nil, err
				}
				rows = append(rows, row)
			}
			if err := scanner.Err(); err != nil {
				return nil, err
			}
			sortRows(rows)
			snapshot.Tables["operation_logs"] = rows
			continue
		}
		if !strings.HasPrefix(file.Path, "data/") && !strings.HasPrefix(file.Path, "config/") {
			continue
		}
		table := currentTable(strings.TrimSuffix(filepath.Base(file.Path), ".json"))
		rule, known := Rules[table]
		if !known {
			rule, known = collectionRules[table]
		}
		if !known && table == "task_jobs" {
			rule, known = legacyQueueRule, true
		}
		if !known {
			return nil, fmt.Errorf("unknown replica table %s", table)
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var rows []map[string]any
		if err := decoder.Decode(&rows); err != nil {
			return nil, err
		}
		for _, row := range namedRows(rows, false) {
			for column := range row {
				if _, ok := rule.Columns()[column]; !ok && !legacyMarkdownColumn(table, column) {
					return nil, fmt.Errorf("unclassified replica column %s.%s", table, column)
				}
				if secretColumn(table, column) {
					return nil, fmt.Errorf("credential column in replica %s.%s", table, column)
				}
			}
		}
		snapshot.Tables[strings.TrimSuffix(filepath.Base(file.Path), ".json")] = rows
	}
	if manifest.RegistryVersion > 0 {
		found := false
		for _, file := range manifest.Files {
			if file.Path == "registry.json" {
				found = true
			}
		}
		if !found {
			return nil, errors.New("missing persistent table registry")
		}
	}
	for _, table := range manifest.Tables {
		if _, found := snapshot.Tables[table]; !found {
			return nil, fmt.Errorf("missing replica table %s", table)
		}
	}
	if err := currentSnapshot(snapshot); err != nil {
		return nil, err
	}
	if manifest.RegistryVersion > 0 {
		actual, err := os.ReadFile(filepath.Join(directory, "registry.json"))
		if err != nil {
			return nil, err
		}
		expected, err := registryBytes(snapshot)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(actual, expected) {
			return nil, errors.New("persistent table registry differs from supported table and column mapping")
		}
	}
	return snapshot, nil
}

// Import restores stable identities and every non-derived field. The optional
// materializer supplies body bytes from files in each library directory.
func Import(ctx context.Context, db *gorm.DB, root, uploads string, materialize func(map[string]any) error) (*Snapshot, error) {
	if err := ValidateClassification(ctx, db); err != nil {
		return nil, err
	}
	snapshot, err := Load(root)
	if err != nil {
		return nil, err
	}
	if err := retireSnapshotSources(root, snapshot); err != nil {
		return nil, err
	}
	markdownOnly, err := markdownOnlySchema(ctx, db)
	if err != nil {
		return nil, err
	}
	if markdownOnly {
		projectMarkdownSnapshot(snapshot)
	}
	missing := map[string]bool{}
	for _, requirement := range snapshot.Required {
		missing[requirement.Kind+":"+requirement.ID] = true
	}
	for _, row := range snapshot.Tables["entries"] {
		if materialize != nil {
			if err := materialize(row); err != nil {
				return nil, err
			}
		}
	}
	var existing int64
	if err := db.WithContext(ctx).Table("entries").Count(&existing).Error; err != nil {
		return nil, err
	}
	if existing != 0 {
		return nil, errors.New("replica import requires an empty entry database")
	}
	if db.Migrator().HasTable("library_content_types") {
		if err := migrateLibrarySnapshot(snapshot, nil); err != nil {
			return nil, err
		}
	}
	queueTable := "task_jobs"
	if db.Migrator().HasTable("task_pending_ops") {
		queueTable = "task_pending_ops"
		if err := migrateQueueSnapshot(snapshot); err != nil {
			return nil, err
		}
	}
	if db.Migrator().HasColumn("topics", "library_ids") {
		projectFollowSnapshot(snapshot)
	}
	if db.Migrator().HasColumn("entries", "source_ref") {
		projectIngestSnapshot(snapshot)
	}
	if db.Migrator().HasColumn("ingest_discards", "library_id") {
		projectDiscardSnapshot(snapshot)
	}
	if db.Migrator().HasColumn("embedding_rebuild_runs", "candidate") {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		projectVectorSnapshot(snapshot, now, now, vectorSnapshotLegacy(snapshot))
	}
	order := []string{"spaces", "users", "auth_tokens", "libraries", "library_content_types", "library_relation_types", "models", "resources", "resource_access_grants", "entities", "entity_aliases", "topics", "entries", "entry_entities", "entry_topics", "entity_merge_suggestions", "entity_merge_records", "follows", "follow_feed_states", "topic_backfill_jobs", "topic_backfill_batches", "embedding_rebuild_runs", "embedding_rebuild_items", "resource_bindings", "api_keys", "system_settings", "chatgpt_hosts", "chatgpt_accounts", "chatgpt_login_attempts", "chatgpt_policy", "entity_embedding_rebuild_runs", "entry_processing_spans", "entry_repost_sources", "ingest_assets", "ingest_discards", "operation_logs", "task_dead_letters", queueTable, "task_queue_counters", "maintenance_state"}
	if !db.Migrator().HasTable("library_content_types") {
		filtered := []string{}
		for _, table := range order {
			if table != "library_content_types" && table != "library_relation_types" {
				filtered = append(filtered, table)
			}
		}
		order = filtered
	}
	order, err = registeredImportOrder(order, snapshot.Tables)
	if err != nil {
		return nil, err
	}
	covered := map[string]bool{}
	for _, table := range order {
		covered[table] = true
	}
	for table := range snapshot.Tables {
		if !covered[table] {
			return nil, fmt.Errorf("persistent replica table %s has no import dependency order", table)
		}
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var emptyTables []string
		for _, table := range order {
			if table != "maintenance_state" {
				emptyTables = append(emptyTables, "public."+quote(table))
			}
		}
		if err := tx.Exec("TRUNCATE " + strings.Join(emptyTables, ",") + " RESTART IDENTITY CASCADE").Error; err != nil {
			return err
		}
		// This trigger records a real processing transition. Import must keep the
		// historical qualification timestamp rather than manufacture a new one.
		if err := tx.Exec("ALTER TABLE public.entries DISABLE TRIGGER set_follow_ready_at").Error; err != nil {
			return err
		}
		for _, table := range order {
			rows := snapshot.Tables[table]
			occurrences := map[string]int{}
			for _, row := range rows {
				if table == "maintenance_state" {
					updates := map[string]any{}
					for column, value := range row {
						updates[column] = value
					}
					if err := tx.Table("maintenance_state").Where("id=true").Updates(updates).Error; err != nil {
						return err
					}
					continue
				}
				if inactiveCredentialTable(table) {
					restoreInactiveCredentials(table, row, occurrences)
				}
				if table == "api_keys" {
					row["key_hash"] = "disabled-" + fmt.Sprint(row["id"])
					row["api_key"] = ""
				}
				if table == "models" && (missing[table+":"+fmt.Sprint(row["id"])] || row["source"] == "chatgpt") {
					for _, requirement := range snapshot.Required {
						if requirement.Kind == table && requirement.ID == fmt.Sprint(row["id"]) {
							markModelRequirements(row, requirement.Fields)
						}
					}
					row["status"] = "needs_configuration"
				}
				if table == "chatgpt_accounts" {
					row["credentials"] = ""
					row["status"] = "needs_login"
				}
				columns := make([]string, 0, len(row))
				for column := range row {
					columns = append(columns, column)
				}
				sort.Strings(columns)
				quoted := make([]string, len(columns))
				selected := make([]string, len(columns))
				literalNulls := map[string]bool{}
				for _, column := range snapshot.JSONNulls[table][rowIdentity(row)] {
					literalNulls[column] = true
				}
				for i, column := range columns {
					quoted[i] = quote(column)
					selected[i] = quote(column)
					if literalNulls[column] && row[column] == nil {
						selected[i] = "'null'::jsonb"
					}
				}
				data, err := json.Marshal(row)
				if err != nil {
					return err
				}
				query := "INSERT INTO public." + quote(table) + " (" + strings.Join(quoted, ",") + ") SELECT " + strings.Join(selected, ",") + " FROM jsonb_populate_record(NULL::public." + quote(table) + ", ?::jsonb)"
				if err := tx.Exec(query, string(data)).Error; err != nil {
					return fmt.Errorf("restore %s: %w", table, err)
				}
			}
		}
		if err := tx.Exec("ALTER TABLE public.entries ENABLE TRIGGER set_follow_ready_at").Error; err != nil {
			return err
		}
		// Restore every owned serial sequence, including audit history IDs.
		var serials []struct {
			Table    string `gorm:"column:table_name"`
			Column   string `gorm:"column:column_name"`
			Sequence string `gorm:"column:sequence_name"`
		}
		if err := tx.Raw(`SELECT c.table_name,c.column_name,pg_get_serial_sequence('public.'||quote_ident(c.table_name),c.column_name) AS sequence_name FROM information_schema.columns c WHERE c.table_schema='public' AND c.column_default LIKE 'nextval(%'`).Scan(&serials).Error; err != nil {
			return err
		}
		for _, serial := range serials {
			if serial.Sequence == "" {
				continue
			}
			if err := tx.Exec(`SELECT setval(?::regclass, GREATEST(1,(SELECT COALESCE(MAX(`+quote(serial.Column)+`),0) FROM public.`+quote(serial.Table)+`)), true)`, serial.Sequence).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if uploads != "" {
		if err := RestoreResources(root, uploads, snapshot.ActiveUploads); err != nil {
			return nil, err
		}
	}
	return snapshot, nil
}

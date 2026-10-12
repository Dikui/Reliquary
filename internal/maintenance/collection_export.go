package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
)

const CollectionRemovalMigration = 5

// SourcesExport preserves the retired configuration and synchronization history.
// Its rows use the same credential exclusions as complete maintenance backups.
type SourcesExport struct {
	ExportedAt             string                         `json:"exported_at"`
	MigrationVersion       int                            `json:"migration_version"`
	SourceMigrationVersion *int64                         `json:"source_migration_version,omitempty"`
	Tables                 map[string][]map[string]any    `json:"tables"`
	JSONNulls              map[string]map[string][]string `json:"json_nulls"`
}

type SourcesExportResult struct {
	Path        string `json:"path,omitempty"`
	Skipped     bool   `json:"skipped"`
	DataSources int    `json:"data_sources"`
	SyncLogs    int    `json:"sync_logs"`
}

type CollectionExportProof struct {
	File   File          `json:"file"`
	Export SourcesExport `json:"export"`
}

// ExportSources must succeed before migration 5 is executed. Repeating an
// unchanged export reuses the verified file instead of creating another copy.
func ExportSources(ctx context.Context, db *gorm.DB, root string) (*SourcesExportResult, error) {
	var migration struct {
		Version int64
		Dirty   bool
	}
	if err := db.WithContext(ctx).Table("public.schema_migrations").Take(&migration).Error; err != nil {
		return nil, err
	}
	if migration.Dirty {
		return nil, errors.New("source export requires a clean migration version")
	}
	if migration.Version >= CollectionRemovalMigration {
		return &SourcesExportResult{Skipped: true}, nil
	}
	snapshot, err := Capture(ctx, db)
	if err != nil {
		return nil, err
	}
	return exportSnapshotSources(root, snapshot, &migration.Version)
}

func sourceExport(snapshot *Snapshot, version *int64) SourcesExport {
	export := SourcesExport{ExportedAt: time.Now().UTC().Format(time.RFC3339Nano), MigrationVersion: CollectionRemovalMigration, SourceMigrationVersion: version, Tables: map[string][]map[string]any{}, JSONNulls: map[string]map[string][]string{}}
	for table := range collectionRules {
		rows := snapshot.Tables[table]
		if rows == nil {
			rows = []map[string]any{}
		}
		export.Tables[table] = rows
		if snapshot.JSONNulls[table] != nil {
			export.JSONNulls[table] = snapshot.JSONNulls[table]
		}
	}
	return export
}

func exportSnapshotSources(root string, snapshot *Snapshot, version *int64) (*SourcesExportResult, error) {
	if _, found := snapshot.Tables["data_sources"]; !found {
		return &SourcesExportResult{Skipped: true}, nil
	}
	if _, found := snapshot.Tables["sync_logs"]; !found {
		return nil, errors.New("retired source replica lacks synchronization history")
	}
	export := sourceExport(snapshot, version)
	proofs, err := collectionExports(root)
	if err != nil {
		return nil, err
	}
	result := &SourcesExportResult{DataSources: len(export.Tables["data_sources"]), SyncLogs: len(export.Tables["sync_logs"])}
	for _, proof := range proofs {
		if sameSourceExport(&export, &proof.Export) {
			result.Path = filepath.Join(root, filepath.FromSlash(proof.File.Path))
			return result, nil
		}
	}
	payload, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return nil, err
	}
	payload = append(payload, '\n')
	relative := ".reliquary/exports/sources-000005-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + sum(payload)[:12] + ".json"
	path, err := SafePath(root, relative)
	if err != nil {
		return nil, err
	}
	if err := AtomicWrite(path, payload); err != nil {
		return nil, fmt.Errorf("export retired sources before migration 000005: %w", err)
	}
	readBack, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(readBack, payload) {
		return nil, errors.New("retired source export read-back verification failed")
	}
	result.Path = path
	return result, nil
}

func sameSourceExport(left, right *SourcesExport) bool {
	for table := range collectionRules {
		a, _ := json.Marshal(canonicalRows(table, left.Tables[table]))
		b, _ := json.Marshal(canonicalRows(table, right.Tables[table]))
		if !bytes.Equal(a, b) {
			return false
		}
	}
	a, _ := json.Marshal(left.JSONNulls)
	b, _ := json.Marshal(right.JSONNulls)
	return bytes.Equal(a, b)
}

func collectionExports(root string) ([]CollectionExportProof, error) {
	path, err := SafePath(root, ".reliquary/exports")
	if err != nil {
		return nil, err
	}
	files, err := Files(path)
	if err != nil {
		return nil, err
	}
	proofs := []CollectionExportProof{}
	for _, file := range files {
		if !strings.HasPrefix(file.Path, "sources-000005-") || filepath.Ext(file.Path) != ".json" {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(path, filepath.FromSlash(file.Path)))
		if err != nil {
			return nil, err
		}
		var export SourcesExport
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&export); err != nil {
			return nil, fmt.Errorf("invalid retired source export: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, errors.New("retired source export must contain one JSON object")
		}
		if export.SourceMigrationVersion != nil && (*export.SourceMigrationVersion < 0 || *export.SourceMigrationVersion >= CollectionRemovalMigration) {
			return nil, errors.New("invalid retired source export migration version")
		}
		for table := range export.JSONNulls {
			if _, known := collectionRules[table]; !known {
				return nil, errors.New("unknown retired source export JSONB null inventory")
			}
		}
		if export.MigrationVersion != CollectionRemovalMigration || len(export.Tables) != 2 {
			return nil, errors.New("invalid retired source export inventory")
		}
		if _, err := time.Parse(time.RFC3339Nano, export.ExportedAt); err != nil {
			return nil, errors.New("invalid retired source export time")
		}
		for table, rule := range collectionRules {
			rows, found := export.Tables[table]
			if !found || rows == nil {
				return nil, errors.New("retired source export lacks a required table")
			}
			for _, row := range rows {
				for column := range row {
					if _, known := rule.Columns()[column]; !known {
						return nil, fmt.Errorf("unclassified retired source export column %s.%s", table, column)
					}
				}
				_, removed := sanitizeRow(table, row)
				if len(removed) != 0 {
					return nil, errors.New("retired source export contains credential fields")
				}
			}
		}
		file.Path = ".reliquary/exports/" + file.Path
		proofs = append(proofs, CollectionExportProof{File: file, Export: export})
	}
	return proofs, nil
}

// projectRetiredTables accepts the deletion only when the export exactly
// preserves each old safe row and JSONB null type. All other tables stay exact.
func projectRetiredTables(expected, actual *Manifest) (*Manifest, *Manifest) {
	if _, old := expected.Tables["data_sources"]; !old {
		return expected, actual
	}
	if _, stillPresent := actual.Tables["data_sources"]; stillPresent {
		return expected, actual
	}
	accepted := map[string]bool{}
	for _, proof := range actual.CollectionExports {
		matches := true
		bound := false
		for _, file := range actual.Files["vault"] {
			if file == proof.File {
				bound = true
			}
		}
		if !bound {
			continue
		}
		for table := range collectionRules {
			wanted, present := expected.Tables[table]
			rows, found := proof.Export.Tables[table]
			if !present || !found || int64(len(rows)) != wanted.Rows {
				matches = false
				break
			}
			left, right := comparableCredentialRows(table, wanted.Data, rows, expected.Required)
			a, _ := json.Marshal(left)
			b, _ := json.Marshal(right)
			if !bytes.Equal(a, b) {
				matches = false
				break
			}
			a, _ = json.Marshal(wanted.JSONNulls)
			b, _ = json.Marshal(proof.Export.JSONNulls[table])
			if !bytes.Equal(a, b) {
				matches = false
				break
			}
		}
		if matches {
			accepted[proof.File.Path] = true
		}
	}
	if len(accepted) == 0 {
		return expected, actual
	}
	left, right := *expected, *actual
	left.collectionMigrated = true
	left.Tables = map[string]TableManifest{}
	for table, entry := range expected.Tables {
		if _, retired := collectionRules[table]; !retired {
			left.Tables[table] = entry
		}
	}
	left.Files = map[string][]File{}
	right.Files = map[string][]File{}
	for group, files := range expected.Files {
		for _, file := range files {
			if group == "vault" && (file.Path == ".reliquary/config/data_sources.json" || file.Path == ".reliquary/data/sync_logs.json") {
				continue
			}
			left.Files[group] = append(left.Files[group], file)
		}
		if left.Files[group] == nil {
			left.Files[group] = []File{}
		}
	}
	for group, files := range actual.Files {
		for _, file := range files {
			if group == "vault" && accepted[file.Path] {
				continue
			}
			right.Files[group] = append(right.Files[group], file)
		}
	}
	return &left, &right
}

func retireSnapshotSources(root string, snapshot *Snapshot) error {
	if _, old := snapshot.Tables["data_sources"]; !old {
		return nil
	}
	if _, err := exportSnapshotSources(root, snapshot, nil); err != nil {
		return err
	}
	addSnapshotMigrationDefaults(snapshot)
	for table := range collectionRules {
		delete(snapshot.Tables, table)
		delete(snapshot.JSONNulls, table)
	}
	required := snapshot.Required[:0]
	for _, requirement := range snapshot.Required {
		if _, retired := collectionRules[requirement.Kind]; !retired {
			required = append(required, requirement)
		}
	}
	snapshot.Required = required
	return nil
}

// archiveCollectionReplica checks only the retired files here: unrelated
// article files may have legitimately changed since the previous flush.
func archiveCollectionReplica(root string) (bool, error) {
	path, err := SafePath(root, ".reliquary/manifest.json")
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var manifest ReplicaManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		for _, legacy := range []string{"config/data_sources.json", "data/sync_logs.json"} {
			if _, found := os.Stat(filepath.Join(root, ".reliquary", legacy)); found == nil {
				return false, err
			}
		}
		return false, nil
	}
	old := false
	for _, table := range manifest.Tables {
		if table == "data_sources" {
			old = true
		}
	}
	if !old {
		return false, nil
	}
	snapshot := &Snapshot{Tables: map[string][]map[string]any{}, JSONNulls: map[string]map[string][]string{}}
	for table := range collectionRules {
		relative := "data/" + table + ".json"
		if configTable(table) {
			relative = "config/" + table + ".json"
		}
		found := false
		for _, file := range manifest.Files {
			if file.Path != relative {
				continue
			}
			found = true
			target, err := SafePath(root, ".reliquary/"+relative)
			if err != nil {
				return false, err
			}
			payload, err := os.ReadFile(target)
			if err != nil || sum(payload) != file.SHA256 || int64(len(payload)) != file.Size {
				return false, errors.New("retired source replica checksum mismatch")
			}
			decoder := json.NewDecoder(bytes.NewReader(payload))
			decoder.UseNumber()
			var rows []map[string]any
			if err := decoder.Decode(&rows); err != nil {
				return false, err
			}
			snapshot.Tables[table] = namedRows(rows, false)
			if manifest.JSONNulls[table] != nil {
				snapshot.JSONNulls[table] = manifest.JSONNulls[table]
			}
		}
		if !found {
			return false, errors.New("retired source replica lacks a required file")
		}
	}
	_, err = exportSnapshotSources(root, snapshot, nil)
	return err == nil, err
}

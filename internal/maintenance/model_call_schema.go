package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"gorm.io/gorm"
)

// Backup-only rows stay out of portable replicas. SQL backup verification keeps
// their count and digest without copying the ledger into the manifest's Data.
func backupOnlyTableManifest(ctx context.Context, db *gorm.DB, table string, rule Rule) (TableManifest, error) {
	result := TableManifest{Columns: []string{}, BackupRowHashes: map[string]string{}}
	for column := range rule.Columns() {
		result.Columns = append(result.Columns, column)
	}
	sort.Strings(result.Columns)
	var encoded []string
	if err := db.WithContext(ctx).Raw("SELECT to_jsonb(t)::text FROM public." + quote(table) + " t").Scan(&encoded).Error; err != nil {
		return result, err
	}
	rows := []map[string]any{}
	for _, data := range encoded {
		row, err := jsonRow([]byte(data))
		if err != nil {
			return result, err
		}
		rows = append(rows, row)
	}
	canonical := canonicalRows(table, rows)
	if table == "model_calls" {
		legacy := []map[string]any{}
		defaults := true
		for _, row := range canonical {
			copy := map[string]any{}
			for key, value := range row {
				copy[key] = value
			}
			if value, ok := copy["processing_attempt"]; ok && fmt.Sprint(value) != "0" {
				defaults = false
			}
			if value, ok := copy["span_id"]; ok && value != "" {
				defaults = false
			}
			delete(copy, "processing_attempt")
			delete(copy, "span_id")
			legacy = append(legacy, copy)
		}
		if defaults {
			projected := TableManifest{Rows: int64(len(legacy)), Columns: []string{}, BackupRowHashes: map[string]string{}}
			for _, col := range result.Columns {
				if col != "processing_attempt" && col != "span_id" {
					projected.Columns = append(projected.Columns, col)
				}
			}
			data, _ := json.Marshal(legacy)
			projected.SHA256 = sum(data)
			for _, row := range legacy {
				data, _ := json.Marshal(row)
				projected.BackupRowHashes[rowIdentity(row)] = sum(data)
			}
			result.legacyCalls = &projected
		}
	}
	for _, row := range canonical {
		data, err := json.Marshal(row)
		if err != nil {
			return result, err
		}
		result.BackupRowHashes[rowIdentity(row)] = sum(data)
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return result, err
	}
	result.Rows = int64(len(rows))
	result.SHA256 = sum(data)
	return result, nil
}

func projectModelCallManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 22 || actual.SchemaVersion < 22 {
		return expected, actual
	}
	if _, ok := expected.Tables["model_calls"]; ok {
		return expected, actual
	}
	if _, ok := actual.Tables["model_calls"]; !ok {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables)+1)
	for table, item := range expected.Tables {
		left.Tables[table] = item
	}
	columns := []string{}
	for column := range BackupOnlyTables["model_calls"].Columns() {
		if column != "processing_attempt" && column != "span_id" {
			columns = append(columns, column)
		}
	}
	sort.Strings(columns)
	left.Tables["model_calls"] = TableManifest{Columns: columns, SHA256: sum([]byte("[]"))}
	return &left, actual
}

func projectCallContentManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 24 || actual.SchemaVersion < 24 {
		return expected, actual
	}
	left, right := *expected, *actual
	left.Tables = map[string]TableManifest{}
	right.Tables = map[string]TableManifest{}
	for name, table := range expected.Tables {
		left.Tables[name] = table
	}
	for name, table := range actual.Tables {
		right.Tables[name] = table
	}
	if table, ok := actual.Tables["model_calls"]; ok && table.legacyCalls != nil {
		right.Tables["model_calls"] = *table.legacyCalls
	}
	for _, name := range []string{"model_call_contents", "model_call_content_deletions"} {
		if _, ok := expected.Tables[name]; !ok {
			columns := []string{}
			for col := range BackupOnlyTables[name].Columns() {
				columns = append(columns, col)
			}
			sort.Strings(columns)
			left.Tables[name] = TableManifest{Columns: columns, SHA256: sum([]byte("[]"))}
		}
	}

	return &left, &right
}

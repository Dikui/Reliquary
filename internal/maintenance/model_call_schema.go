package maintenance

import (
	"context"
	"encoding/json"
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
		columns = append(columns, column)
	}
	sort.Strings(columns)
	left.Tables["model_calls"] = TableManifest{Columns: columns, SHA256: sum([]byte("[]"))}
	return &left, actual
}

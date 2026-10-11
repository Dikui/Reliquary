package datatable

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strings"
)

const excelSheetNameColumn = "__sheet_name"

type Loader struct {
	fileService   interfaces.FileService
	db            *sql.DB
	jobID         string
	createdTables []string
}

func NewLoader(fileService interfaces.FileService, db *sql.DB, jobID string) *Loader {
	return &Loader{fileService: fileService, db: db, jobID: jobID}
}

type TableSchema struct {
	TableName string                 `json:"table_name"`
	Columns   []ColumnInfo           `json:"columns"`
	RowCount  int64                  `json:"row_count"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}
type ColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable string `json:"nullable"`
}

func (t *Loader) SampleRows(ctx context.Context, schema *TableSchema, limit int) ([]map[string]string, error) {
	return t.executeSingleQuery(ctx, fmt.Sprintf("SELECT * FROM %s LIMIT %d", quoteIdentifier(schema.TableName), max(1, limit)))
}

func quoteIdentifier(value string) string {
	return "\"" + strings.ReplaceAll(value, "\"", "\"\"") + "\""
}

func sqlSingleQuoteEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func (t *Loader) recordCreatedTable(tableName string) bool {
	for _, name := range t.createdTables {
		if name == tableName {
			return false
		}
	}
	t.createdTables = append(t.createdTables, tableName)
	return true
}

func (t *Loader) Cleanup(ctx context.Context) {
	if len(t.createdTables) == 0 {
		logger.Infof(ctx, "[DataTableSummary] No tables to clean up for job: %s", t.jobID)
		return
	}

	logger.Infof(ctx, "[DataTableSummary] Cleaning up %d tables for job: %s", len(t.createdTables), t.jobID)

	for _, tableName := range t.createdTables {
		dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS \"%s\"", tableName)
		if _, err := t.db.ExecContext(ctx, dropSQL); err != nil {
			logger.Errorf(ctx, "[DataTableSummary] Failed to drop table '%s': %v", tableName, err)
			// Continue to drop other tables even if one fails
			continue
		}
		logger.Infof(ctx, "[DataTableSummary] Successfully dropped table '%s'", tableName)
	}

	// Clear the list after cleanup
	t.createdTables = nil
}

func (t *Loader) executeSingleQuery(ctx context.Context, sqlQuery string) ([]map[string]string, error) {
	rows, err := t.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		logger.Errorf(ctx, "[DataTableSummary] Query execution failed: %v", err)
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		logger.Errorf(ctx, "[DataTableSummary] Failed to get columns: %v", err)
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// Process results
	results := make([]map[string]string, 0)
	for rows.Next() {
		columnValues := make([]interface{}, len(columns))
		columnPointers := make([]interface{}, len(columns))
		for i := range columnValues {
			columnPointers[i] = &columnValues[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			logger.Errorf(ctx, "[DataTableSummary] Failed to scan row: %v", err)
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		rowMap := make(map[string]string)
		for i, colName := range columns {
			val := columnValues[i]
			// Convert []byte to string for better readability
			if b, ok := val.([]byte); ok {
				rowMap[colName] = string(b)
			} else {
				rowMap[colName] = fmt.Sprintf("%v", val)
			}
		}
		results = append(results, rowMap)
	}

	if err := rows.Err(); err != nil {
		logger.Errorf(ctx, "[DataTableSummary] Error iterating rows: %v", err)
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}

func (t *Loader) LoadFromCSV(ctx context.Context, filename string, tableName string) (*TableSchema, error) {
	logger.Infof(ctx, "[DataTableSummary] Loading CSV file '%s' into table '%s' for job %s", filename, tableName, t.jobID)

	// Record the created table for cleanup. If already exists, skip creation
	if t.recordCreatedTable(tableName) {
		// Create table from CSV using DuckDB's read_csv_auto function
		// with explicit header detection and VARCHAR coercion to align with
		// Excel loading behavior.
		// Table will be created in the job schema
		createTableSQL := fmt.Sprintf(
			"CREATE TABLE \"%s\" AS SELECT * FROM read_csv_auto('%s', header=true, all_varchar=true)",
			tableName, sqlSingleQuoteEscape(filename),
		)

		_, err := t.db.ExecContext(ctx, createTableSQL)
		if err != nil {
			logger.Errorf(ctx, "[DataTableSummary] Failed to create table from CSV: %v", err)
			return nil, fmt.Errorf("failed to create table from CSV: %w", err)
		}

		logger.Infof(ctx, "[DataTableSummary] Successfully created table '%s' from CSV file in job %s", tableName, t.jobID)
	}

	// Get and return the table schema
	return t.LoadFromTable(ctx, tableName)
}

func (t *Loader) LoadFromExcel(ctx context.Context, filename string, tableName string) (*TableSchema, error) {
	logger.Infof(ctx, "[DataTableSummary] Loading Excel file '%s' into table '%s' for job %s", filename, tableName, t.jobID)

	// Record the created table for cleanup. If already exists, skip creation.
	if t.recordCreatedTable(tableName) {
		sheetNames, enumErr := t.listExcelSheets(ctx, filename)
		if enumErr != nil {
			logger.Warnf(ctx,
				"[DataTableSummary] Could not enumerate sheets for '%s' (job=%s): %v. Falling back to first sheet only.",
				filename, t.jobID, enumErr,
			)
		}

		createTableSQL := buildExcelCreateTableSQL(tableName, filename, sheetNames)

		if _, err := t.db.ExecContext(ctx, createTableSQL); err != nil {
			logger.Errorf(ctx, "[DataTableSummary] Failed to create table from Excel (sheets=%v): %v", sheetNames, err)
			return nil, fmt.Errorf("failed to create table from Excel file (sheets=%v): %w", sheetNames, err)
		}

		logger.Infof(ctx,
			"[DataTableSummary] Successfully created table '%s' from Excel file in job %s (sheets=%v)",
			tableName, t.jobID, sheetNames,
		)
	}

	// Get and return the table schema
	return t.LoadFromTable(ctx, tableName)
}

func (t *Loader) listExcelSheets(ctx context.Context, filename string) ([]string, error) {
	metaSQL := fmt.Sprintf(
		"SELECT UNNEST(layers).name FROM st_read_meta('%s')",
		sqlSingleQuoteEscape(filename),
	)

	rows, err := t.db.QueryContext(ctx, metaSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to query sheet metadata: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan sheet name: %w", err)
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sheet metadata rows: %w", err)
	}
	return names, nil
}

func buildExcelCreateTableSQL(tableName, filename string, sheetNames []string) string {
	escFile := sqlSingleQuoteEscape(filename)

	// No sheet info (enumeration failed or empty): read the first sheet only.
	if len(sheetNames) == 0 {
		return fmt.Sprintf(
			"CREATE TABLE \"%s\" AS SELECT * FROM read_xlsx('%s', header=true, all_varchar=true)",
			tableName, escFile,
		)
	}

	// Single sheet: keep it simple but still tag the source for consistency
	// with the multi-sheet path.
	if len(sheetNames) == 1 {
		escSheet := sqlSingleQuoteEscape(sheetNames[0])
		return fmt.Sprintf(
			"CREATE TABLE \"%s\" AS SELECT *, '%s' AS %s FROM read_xlsx('%s', sheet = '%s', header=true, all_varchar=true)",
			tableName, escSheet, excelSheetNameColumn, escFile, escSheet,
		)
	}

	// Multiple sheets: UNION ALL BY NAME tolerates schema differences
	// between sheets (missing columns become NULL, conflicting types are
	// widened).
	parts := make([]string, 0, len(sheetNames))
	for _, sheet := range sheetNames {
		escSheet := sqlSingleQuoteEscape(sheet)
		parts = append(parts, fmt.Sprintf(
			"SELECT *, '%s' AS %s FROM read_xlsx('%s', sheet = '%s', header=true, all_varchar=true)",
			escSheet, excelSheetNameColumn, escFile, escSheet,
		))
	}
	return fmt.Sprintf(
		"CREATE TABLE \"%s\" AS %s",
		tableName,
		strings.Join(parts, "\nUNION ALL BY NAME\n"),
	)
}

func (t *Loader) LoadFromEntry(ctx context.Context, entry *types.Entry) (*TableSchema, error) {
	if entry == nil {
		return nil, fmt.Errorf("entry cannot be nil")
	}
	tableName := t.TableName(entry)

	// Normalize file type to lowercase for comparison
	fileType := strings.ToLower(entry.FileType)

	logger.Infof(ctx, "[DataTableSummary] Loading entry '%s' (type: %s) into table '%s' for job %s",
		entry.ID, fileType, tableName, t.jobID)

	localPath, cleanup, err := t.materializeEntryFile(ctx, entry)
	if err != nil {
		return nil, fmt.Errorf("failed to materialize entry '%s' for DuckDB: %w", entry.ID, err)
	}
	defer cleanup()

	switch fileType {
	case "csv":
		return t.LoadFromCSV(ctx, localPath, tableName)
	case "xlsx", "xls":
		return t.LoadFromExcel(ctx, localPath, tableName)
	default:
		logger.Warnf(ctx, "[DataTableSummary] Unsupported file type '%s' for entry '%s' in job %s",
			fileType, entry.ID, t.jobID)
		return nil, fmt.Errorf("unsupported file type: %s (supported types: csv, xlsx, xls)", fileType)
	}
}

func (t *Loader) materializeEntryFile(ctx context.Context, entry *types.Entry) (string, func(), error) {
	noop := func() {}

	reader, err := t.resolveFileServiceForEntry(ctx, entry).GetFile(ctx, entry.FilePath)
	if err != nil {
		return "", noop, fmt.Errorf("failed to open file for entry '%s': %w", entry.ID, err)
	}
	defer reader.Close()

	// Preserve the file extension so DuckDB's format auto-detection still
	// works (e.g. the CSV reader expects .csv, xlsx reader expects .xlsx).
	suffix := ""
	if ext := strings.ToLower(strings.TrimSpace(entry.FileType)); ext != "" {
		suffix = "." + ext
	}

	tmp, err := os.CreateTemp("", "reliquary-table-summary-*"+suffix)
	if err != nil {
		return "", noop, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		// Best-effort cleanup; a missing file is fine, any other error is
		// only logged to avoid masking the original operation's result.
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			logger.Warnf(ctx, "[DataTableSummary] Failed to remove temp file %s: %v", tmpPath, err)
		}
	}

	if _, err := io.Copy(tmp, reader); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", noop, fmt.Errorf("failed to copy entry '%s' to temp file: %w", entry.ID, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", noop, fmt.Errorf("failed to finalize temp file for entry '%s': %w", entry.ID, err)
	}

	logger.Infof(ctx, "[DataTableSummary] Materialized entry '%s' to temp file %s for job %s",
		entry.ID, tmpPath, t.jobID)

	return tmpPath, cleanup, nil
}

func (t *Loader) LoadFromTable(ctx context.Context, tableName string) (*TableSchema, error) {
	logger.Infof(ctx, "[DataTableSummary] Getting schema for table '%s' in job %s", tableName, t.jobID)

	// Query to get column information using PRAGMA table_info or DESCRIBE
	schemaSQL := fmt.Sprintf("DESCRIBE \"%s\"", tableName)

	rows, err := t.db.QueryContext(ctx, schemaSQL)
	if err != nil {
		logger.Errorf(ctx, "[DataTableSummary] Failed to get table schema: %v", err)
		return nil, fmt.Errorf("failed to get table schema: %w", err)
	}
	defer rows.Close()

	// Parse column information
	columns := make([]ColumnInfo, 0)
	for rows.Next() {
		var colName, colType, nullable string
		var extra1, extra2, extra3 interface{} // DuckDB DESCRIBE may return additional columns

		// Try to scan with different column counts
		err := rows.Scan(&colName, &colType, &nullable, &extra1, &extra2, &extra3)
		if err != nil {
			// Try with fewer columns
			err = rows.Scan(&colName, &colType, &nullable)
			if err != nil {
				logger.Errorf(ctx, "[DataTableSummary] Failed to scan column info: %v", err)
				return nil, fmt.Errorf("failed to scan column info: %w", err)
			}
		}

		columns = append(columns, ColumnInfo{
			Name:     colName,
			Type:     colType,
			Nullable: nullable,
		})
	}

	if err := rows.Err(); err != nil {
		logger.Errorf(ctx, "[DataTableSummary] Error iterating schema rows: %v", err)
		return nil, fmt.Errorf("error iterating schema rows: %w", err)
	}

	// Get row count
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM \"%s\"", tableName)
	var rowCount int64
	if err := t.db.QueryRowContext(ctx, countSQL).Scan(&rowCount); err != nil {
		logger.Errorf(ctx, "[DataTableSummary] Failed to get row count: %v", err)
		return nil, fmt.Errorf("failed to get row count: %w", err)
	}

	schema := &TableSchema{
		TableName: tableName,
		Columns:   columns,
		RowCount:  rowCount,
		Metadata: map[string]interface{}{
			"column_count": len(columns),
			"job_id":       t.jobID,
		},
	}

	logger.Infof(ctx, "[DataTableSummary] Retrieved schema for table '%s' in job %s: %d columns, %d rows",
		tableName, t.jobID, len(columns), rowCount)

	return schema, nil
}

func (t *Loader) TableName(entry *types.Entry) string {
	return "k_" + strings.ReplaceAll(entry.ID, "-", "_")
}

func (t *TableSchema) Description() string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Table name: %s\n", t.TableName))
	builder.WriteString(fmt.Sprintf("Columns: %d\n", len(t.Columns)))
	builder.WriteString(fmt.Sprintf("Rows: %d\n\n", t.RowCount))
	builder.WriteString("Column info:\n")

	for _, col := range t.Columns {
		builder.WriteString(fmt.Sprintf("- %s (%s)\n", col.Name, col.Type))
	}

	return builder.String()
}

func (t *Loader) resolveFileServiceForEntry(ctx context.Context, entry *types.Entry) interfaces.FileService {
	return t.fileService
}

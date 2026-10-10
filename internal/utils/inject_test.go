package utils

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestParseSQL(t *testing.T) {
	tests := []struct {
		name          string
		sql           string
		wantIsSelect  bool
		wantTables    []string
		wantSelect    []string
		wantWhere     []string
		wantWhereText string
	}{
		{
			name:          "Simple SELECT",
			sql:           "SELECT id, name, age FROM users WHERE age > 18",
			wantIsSelect:  true,
			wantTables:    []string{"users"},
			wantSelect:    []string{"id", "name", "age"},
			wantWhere:     []string{"age"},
			wantWhereText: "age > 18",
		},
		{
			name:          "SELECT with multiple WHERE conditions",
			sql:           "SELECT u.id, u.name FROM users u WHERE u.age > 18 AND u.status = 'active'",
			wantIsSelect:  true,
			wantTables:    []string{"users"},
			wantSelect:    []string{"id", "name"},
			wantWhere:     []string{"age", "status"},
			wantWhereText: "u.age > 18 AND u.status = 'active'",
		},
		{
			name:          "SELECT with JOIN",
			sql:           "SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id WHERE o.total > 100",
			wantIsSelect:  true,
			wantTables:    []string{"users", "orders"},
			wantSelect:    []string{"name", "total"},
			wantWhere:     []string{"total"},
			wantWhereText: "o.total > 100",
		},
		{
			name:          "SELECT with aggregate functions",
			sql:           "SELECT COUNT(id), AVG(score) FROM students WHERE grade = 'A'",
			wantIsSelect:  true,
			wantTables:    []string{"students"},
			wantSelect:    []string{"id", "score"},
			wantWhere:     []string{"grade"},
			wantWhereText: "grade = 'A'",
		},
		{
			name:          "SELECT with complex WHERE",
			sql:           "SELECT * FROM products WHERE price BETWEEN 10 AND 100 AND category IN ('electronics', 'books')",
			wantIsSelect:  true,
			wantTables:    []string{"products"},
			wantSelect:    []string{},
			wantWhere:     []string{"price", "category"},
			wantWhereText: "price BETWEEN 10 AND 100 AND category IN ('electronics', 'books')",
		},
		{
			name:         "INSERT statement",
			sql:          "INSERT INTO users (name, age) VALUES ('John', 25)",
			wantIsSelect: false,
		},
		{
			name:         "UPDATE statement",
			sql:          "UPDATE users SET age = 26 WHERE id = 1",
			wantIsSelect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseSQL(tt.sql)

			// Print result for debugging
			resultJSON, _ := json.MarshalIndent(result, "", "  ")
			fmt.Printf("\nTest: %s\nResult:\n%s\n", tt.name, string(resultJSON))

			if result.IsSelect != tt.wantIsSelect {
				t.Errorf("IsSelect = %v, want %v", result.IsSelect, tt.wantIsSelect)
			}

			if !tt.wantIsSelect {
				// For non-SELECT statements, just check IsSelect
				return
			}

			if result.ParseError != "" {
				t.Errorf("ParseError = %v, want empty", result.ParseError)
			}

			// Check tables
			if len(result.TableNames) != len(tt.wantTables) {
				t.Errorf("TableNames count = %d, want %d. Got: %v, Want: %v",
					len(result.TableNames), len(tt.wantTables), result.TableNames, tt.wantTables)
			} else {
				for i, table := range tt.wantTables {
					if i < len(result.TableNames) && result.TableNames[i] != table {
						t.Errorf("TableNames[%d] = %v, want %v", i, result.TableNames[i], table)
					}
				}
			}

			// Check SELECT fields
			if len(result.SelectFields) != len(tt.wantSelect) {
				t.Errorf("SelectFields count = %d, want %d. Got: %v, Want: %v",
					len(result.SelectFields), len(tt.wantSelect), result.SelectFields, tt.wantSelect)
			}

			// Check WHERE fields
			if len(result.WhereFields) != len(tt.wantWhere) {
				t.Errorf("WhereFields count = %d, want %d. Got: %v, Want: %v",
					len(result.WhereFields), len(tt.wantWhere), result.WhereFields, tt.wantWhere)
			}

			// Check WHERE clause text
			if result.WhereClause != tt.wantWhereText {
				t.Errorf("WhereClause = %q, want %q", result.WhereClause, tt.wantWhereText)
			}
		})
	}
}

func ExampleParseSQL() {
	sql := "SELECT id, name, email FROM users WHERE age > 18 AND status = 'active'"
	result := ParseSQL(sql)

	fmt.Printf("Is SELECT: %v\n", result.IsSelect)
	fmt.Printf("Tables: %v\n", result.TableNames)
	fmt.Printf("SELECT fields: %v\n", result.SelectFields)
	fmt.Printf("WHERE fields: %v\n", result.WhereFields)
	fmt.Printf("WHERE clause: %s\n", result.WhereClause)

	// Output:
	// Is SELECT: true
	// Tables: [users]
	// SELECT fields: [id name email]
	// WHERE fields: [age status]
	// WHERE clause: age > 18 AND status = 'active'
}

func TestValidateSQL_TableNames(t *testing.T) {
	tests := []struct {
		name          string
		sql           string
		allowedTables []string
		wantValid     bool
		wantErrorType string
	}{
		{
			name:          "Valid table name",
			sql:           "SELECT * FROM users WHERE id = 1",
			allowedTables: []string{"users", "orders"},
			wantValid:     true,
		},
		{
			name:          "Invalid table name",
			sql:           "SELECT * FROM products WHERE id = 1",
			allowedTables: []string{"users", "orders"},
			wantValid:     false,
			wantErrorType: "table_not_allowed",
		},
		{
			name:          "Multiple tables - all valid",
			sql:           "SELECT * FROM users u JOIN orders o ON u.id = o.user_id",
			allowedTables: []string{"users", "orders"},
			wantValid:     true,
		},
		{
			name:          "Multiple tables - one invalid",
			sql:           "SELECT * FROM users u JOIN products p ON u.id = p.user_id",
			allowedTables: []string{"users", "orders"},
			wantValid:     false,
			wantErrorType: "table_not_allowed",
		},
		{
			name:          "Case insensitive table names",
			sql:           "SELECT * FROM USERS WHERE id = 1",
			allowedTables: []string{"users", "orders"},
			wantValid:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, validation := ValidateSQL(tt.sql, WithAllowedTables(tt.allowedTables...))

			if validation.Valid != tt.wantValid {
				t.Errorf("Valid = %v, want %v", validation.Valid, tt.wantValid)
			}

			if !tt.wantValid && len(validation.Errors) > 0 {
				if validation.Errors[0].Type != tt.wantErrorType {
					t.Errorf("Error type = %v, want %v", validation.Errors[0].Type, tt.wantErrorType)
				}
			}

			// Print validation result for debugging
			if !validation.Valid {
				validationJSON, _ := json.MarshalIndent(validation, "", "  ")
				fmt.Printf("\nTest: %s\nValidation Result:\n%s\n", tt.name, string(validationJSON))
			}
		})
	}
}

// TestValidateSQL_JSONNodeBypass verifies that PG17 SQL/JSON expression nodes
// cannot be used to smuggle dangerous functions past the blacklist. These were
// previously accepted because validateNode had no handler for them and fell
// through to a permissive `return nil`.
func TestValidateSQL_JSONNodeBypass(t *testing.T) {
	dangerous := []struct {
		name string
		sql  string
	}{
		{"JSON_SCALAR + pg_read_file", "SELECT JSON_SCALAR(pg_read_file('/etc/passwd')) AS data FROM knowledge_bases LIMIT 1"},
		{"JSON_VALUE + pg_read_file", "SELECT JSON_VALUE(pg_read_file('/etc/passwd'), '$') AS data FROM knowledge_bases LIMIT 1"},
		{"JSON_QUERY + pg_read_file", "SELECT JSON_QUERY(pg_read_file('/etc/passwd'), '$') AS data FROM knowledge_bases LIMIT 1"},
		{"JSON scalar + lo_export", "SELECT JSON_SCALAR(lo_export(1, '/tmp/x')) FROM knowledge_bases LIMIT 1"},
		{"JSON() parse + pg_read_file", "SELECT JSON(pg_read_file('/etc/passwd')) FROM knowledge_bases LIMIT 1"},
		{"JSON_SERIALIZE + pg_read_file", "SELECT JSON_SERIALIZE(pg_read_file('/etc/passwd')) FROM knowledge_bases LIMIT 1"},
	}

	for _, tt := range dangerous {
		t.Run(tt.name, func(t *testing.T) {
			_, validation := ValidateSQL(tt.sql, WithAllowedTables("knowledge_bases", "knowledges"), WithSingleStatement(), WithNoDangerousFunctions())
			if validation.Valid {
				t.Fatalf("expected SQL to be REJECTED, but it was accepted: %s", tt.sql)
			}
		})
	}
}

// TestValidateSQL_UnknownNodeDefaultDeny ensures a legitimate query still
// passes after the default-deny change, guarding against over-blocking.
func TestValidateSQL_DefaultDenyAllowsLegitimate(t *testing.T) {
	legit := []string{
		"SELECT id, name FROM knowledge_bases WHERE name LIKE '%test%' ORDER BY created_at DESC LIMIT 10",
		"SELECT COUNT(*) AS c FROM knowledges WHERE parse_status = 'completed'",
		"SELECT COALESCE(title, 'untitled') FROM knowledges LIMIT 5",
		"SELECT CASE WHEN file_size > 100 THEN 'big' ELSE 'small' END FROM knowledges LIMIT 5",
		"SELECT kb.name, COUNT(k.id) FROM knowledge_bases kb LEFT JOIN knowledges k ON kb.id = k.knowledge_base_id GROUP BY kb.id, kb.name",
	}
	for _, sql := range legit {
		t.Run(sql, func(t *testing.T) {
			_, validation := ValidateSQL(sql, WithAllowedTables("knowledge_bases", "knowledges"), WithSingleStatement(), WithNoDangerousFunctions())
			if !validation.Valid {
				t.Fatalf("expected legitimate SQL to pass, got errors: %#v\nSQL: %s", validation.Errors, sql)
			}
		})
	}
}

// TestValidateSQL_DuckDBSubqueryFileRead verifies that the data_analysis tool's
// validation options (no WithNoSubqueries) still reject file-reading functions
// hidden inside a FROM subquery, as well as at the top level.
func TestValidateSQL_DuckDBFileRead(t *testing.T) {
	// Mirror the exact options used by the data_analysis (DuckDB) tool.
	opts := []SQLValidationOption{
		WithAllowedTables("k_data"),
		WithSingleStatement(),
		WithNoDangerousFunctions(),
	}

	dangerous := []struct {
		name string
		sql  string
	}{
		{"read_text in FROM subquery", "SELECT * FROM (SELECT * FROM read_text('/etc/passwd')) AS f, k_data AS k LIMIT 1"},
		{"read_text in target list", "SELECT read_text('/etc/passwd') FROM k_data LIMIT 1"},
		{"read_blob in FROM subquery", "SELECT * FROM (SELECT * FROM read_blob('/etc/passwd')) AS f LIMIT 1"},
		{"nested subquery read_csv", "SELECT * FROM (SELECT * FROM (SELECT * FROM read_csv('/etc/passwd')) AS a) AS b LIMIT 1"},
	}
	for _, tt := range dangerous {
		t.Run(tt.name, func(t *testing.T) {
			_, validation := ValidateSQL(tt.sql, opts...)
			if validation.Valid {
				t.Fatalf("expected SQL to be REJECTED, but it was accepted: %s", tt.sql)
			}
		})
	}

	t.Run("legitimate table query passes", func(t *testing.T) {
		_, validation := ValidateSQL("SELECT product, SUM(amount) FROM k_data GROUP BY product", opts...)
		if !validation.Valid {
			t.Fatalf("expected legitimate DuckDB query to pass, got: %#v", validation.Errors)
		}
	})
}

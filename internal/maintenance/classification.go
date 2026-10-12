// Package maintenance implements the portable, credential-free database replica.
package maintenance

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

type Category string

const (
	Header        Category = "header"
	Sidecar       Category = "sidecar"
	Regenerated   Category = "regenerated"
	Configuration Category = "configuration_or_secret"
)

// Rule explicitly names every column. Adding a column requires a deliberate
// preservation decision; an unknown column is a hard maintenance error.
type Rule struct {
	Header        string `json:"header"`
	Sidecar       string `json:"sidecar"`
	Regenerated   string `json:"regenerated"`
	Configuration string `json:"configuration_or_secret"`
}

func (r Rule) Columns() map[string]Category {
	result := map[string]Category{}
	for category, names := range map[Category]string{Header: r.Header, Sidecar: r.Sidecar, Regenerated: r.Regenerated, Configuration: r.Configuration} {
		for _, name := range strings.Fields(names) {
			result[name] = category
		}
	}
	return result
}

type Column struct {
	Table     string `gorm:"column:table_name"`
	Name      string `gorm:"column:column_name"`
	Generated string `gorm:"column:is_generated"`
	DataType  string `gorm:"column:udt_name"`
}

func Schema(ctx context.Context, db *gorm.DB) ([]Column, error) {
	var columns []Column
	err := db.WithContext(ctx).Raw(`SELECT c.table_name,c.column_name,c.is_generated,c.udt_name FROM information_schema.columns c JOIN information_schema.tables t ON t.table_schema=c.table_schema AND t.table_name=c.table_name WHERE c.table_schema='public' AND t.table_type='BASE TABLE' ORDER BY c.table_name,c.ordinal_position`).Scan(&columns).Error
	return columns, err
}

func ValidateClassification(ctx context.Context, db *gorm.DB) error {
	columns, err := Schema(ctx, db)
	if err != nil {
		return err
	}
	present := map[string]map[string]bool{}
	for _, c := range columns {
		rule, ok := Rules[c.Table]
		if !ok {
			return fmt.Errorf("unclassified database table %s", c.Table)
		}
		if _, ok := rule.Columns()[c.Name]; !ok {
			return fmt.Errorf("unclassified database column %s.%s", c.Table, c.Name)
		}
		if present[c.Table] == nil {
			present[c.Table] = map[string]bool{}
		}
		present[c.Table][c.Name] = true
	}
	for table, rule := range Rules {
		seen := map[string]bool{}
		for _, group := range []string{rule.Header, rule.Sidecar, rule.Regenerated, rule.Configuration} {
			for _, column := range strings.Fields(group) {
				if seen[column] {
					return fmt.Errorf("column %s.%s classified more than once", table, column)
				}
				seen[column] = true
				if present[table] != nil && !present[table][column] {
					return fmt.Errorf("classification names missing column %s.%s", table, column)
				}
			}
		}
	}
	return ValidateExtensionCatalog(ctx, db)
}

func sortedTables() []string {
	tables := make([]string, 0, len(Rules))
	for table := range Rules {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables
}

// These columns contain credentials, including one-way API authentication
// hashes. The account password hash is retained to preserve owner login.
var SecretColumns = map[string]string{
	"auth_tokens": "token", "api_keys": "key_hash api_key", "resource_access_grants": "token_hash",
	"spaces": "credentials", "chatgpt_accounts": "credentials", "chatgpt_login_attempts": "state nonce verifier",
}

func secretColumn(table, column string) bool {
	for _, name := range strings.Fields(SecretColumns[table]) {
		if name == column {
			return true
		}
	}
	return false
}

func configTable(table string) bool {
	switch table {
	case "users", "spaces", "api_keys", "models", "data_sources", "knowledge_bases", "system_settings", "chatgpt_hosts", "chatgpt_accounts", "chatgpt_policy", "maintenance_state":
		return true
	}
	return false
}

func inactiveCredentialTable(table string) bool {
	return table == "auth_tokens" || table == "resource_access_grants" || table == "chatgpt_login_attempts"
}

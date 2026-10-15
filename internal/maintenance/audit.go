package maintenance

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// Ownership reports the actual installed catalog, including extension columns.
// Secrets are deliberately omitted from portable copies and require replacement.
type Ownership struct {
	Table    string   `json:"table"`
	Column   string   `json:"column"`
	Owner    string   `json:"owner"`
	Category Category `json:"category"`
	Recovery string   `json:"recovery,omitempty"`
}

func Audit(ctx context.Context, db *gorm.DB) ([]Ownership, error) {
	if err := ValidateClassification(ctx, db); err != nil {
		return nil, err
	}
	rules, err := schemaRules(ctx, db)
	if err != nil {
		return nil, err
	}
	columns, err := CatalogSchema(ctx, db)
	if err != nil {
		return nil, err
	}
	result := make([]Ownership, 0, len(columns))
	for _, column := range columns {
		category, ok := catalogClassification(column, rules)
		if !ok {
			return nil, fmt.Errorf("unclassified column %s.%s.%s", column.Schema, column.Table, column.Name)
		}
		recovery := ""
		owner := "replica"
		if _, ok := BackupOnlyTables[column.Table]; ok {
			owner = "backup_only"
		}
		if category == Regenerated {
			owner = "regenerated"
		}
		if secretColumn(column.Table, column.Name) {
			owner = "regenerated"
			recovery = "replace_secret"
		}
		result = append(result, Ownership{Table: column.Schema + "." + column.Table, Column: column.Name, Owner: owner, Category: category, Recovery: recovery})
	}
	return result, nil
}

package maintenance

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

type CatalogColumn struct {
	Schema    string `gorm:"column:table_schema"`
	Table     string `gorm:"column:table_name"`
	Name      string `gorm:"column:column_name"`
	Kind      string `gorm:"column:relation_kind"`
	Extension string `gorm:"column:extension_name"`
	Version   string `gorm:"column:extension_version"`
}

// CatalogSchema includes all persistent non-system relations, including empty
// tables with no columns. Temporary and TOAST relations are storage internals.
func CatalogSchema(ctx context.Context, db *gorm.DB) ([]CatalogColumn, error) {
	var columns []CatalogColumn
	err := db.WithContext(ctx).Raw(`SELECT n.nspname AS table_schema,c.relname AS table_name,COALESCE(a.attname,'') AS column_name,c.relkind::text AS relation_kind,COALESCE(e.extname,'') AS extension_name,COALESCE(e.extversion,'') AS extension_version
FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped
LEFT JOIN pg_catalog.pg_depend d ON d.classid='pg_class'::regclass AND d.objid=c.oid AND d.refclassid='pg_extension'::regclass AND d.deptype='e'
LEFT JOIN pg_catalog.pg_extension e ON e.oid=d.refobjid
WHERE n.nspname NOT IN ('pg_catalog','information_schema','pg_toast') AND n.nspname NOT LIKE 'pg_toast_temp_%' AND n.nspname NOT LIKE 'pg_temp_%' AND c.relkind IN ('r','p','f','m')
ORDER BY n.nspname,c.relname,a.attnum`).Scan(&columns).Error
	return columns, err
}

// Classification for extension rows is conditional on their exact bootstrap
// fingerprints. Data outside that condition stops maintenance without dropping,
// rewriting, or treating the custom data as a cache.
func CatalogClassification(column CatalogColumn) (Category, bool) {
	if rule, found := ExtensionCatalogRegistry[column.Schema+"."+column.Table]; found {
		for _, name := range strings.Fields(rule.Columns) {
			if name == column.Name {
				return Regenerated, true
			}
		}
		return "", false
	}
	if column.Schema == "public" {
		category, found := Rules[column.Table].Columns()[column.Name]
		return category, found
	}
	return "", false
}

func ValidateExtensionCatalog(ctx context.Context, db *gorm.DB) error {
	columns, err := CatalogSchema(ctx, db)
	if err != nil {
		return err
	}
	present := map[string]map[string]bool{}
	for _, column := range columns {
		key := column.Schema + "." + column.Table
		rule, knownExtension := ExtensionCatalogRegistry[key]
		if !knownExtension {
			if column.Schema != "public" || column.Extension != "" {
				return fmt.Errorf("unclassified database table %s", key)
			}
			if _, found := Rules[column.Table]; !found {
				return fmt.Errorf("unclassified database table %s", key)
			}
			if column.Kind != "r" {
				return fmt.Errorf("unreviewed application relation kind for %s", key)
			}
		}
		if _, found := CatalogClassification(column); !found {
			return fmt.Errorf("unclassified database column %s.%s", key, column.Name)
		}
		if knownExtension && (column.Extension != rule.Extension || column.Version != rule.Version || column.Kind != "r") {
			return fmt.Errorf("extension ownership, version or relation kind differs for %s", key)
		}
		if present[key] == nil {
			present[key] = map[string]bool{}
		}
		present[key][column.Name] = true
	}
	var extensions []struct {
		Name    string `gorm:"column:extname"`
		Version string `gorm:"column:extversion"`
	}
	if err := db.WithContext(ctx).Raw(`SELECT extname,extversion FROM pg_catalog.pg_extension`).Scan(&extensions).Error; err != nil {
		return err
	}
	installed := map[string]string{}
	for _, extension := range extensions {
		installed[extension.Name] = extension.Version
	}
	keys := make([]string, 0, len(ExtensionCatalogRegistry))
	for key := range ExtensionCatalogRegistry {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		rule := ExtensionCatalogRegistry[key]
		if installed[rule.Extension] == "" {
			continue
		}
		if installed[rule.Extension] != rule.Version {
			return fmt.Errorf("unreviewed extension version for %s", rule.Extension)
		}
		if present[key] == nil {
			return fmt.Errorf("missing reviewed extension table %s", key)
		}
		seen := map[string]bool{}
		for _, name := range strings.Fields(rule.Columns) {
			if seen[name] || !present[key][name] {
				return fmt.Errorf("duplicate or missing classified extension column %s.%s", key, name)
			}
			seen[name] = true
		}
		parts := strings.SplitN(key, ".", 2)
		var fingerprint struct {
			Rows   int64  `gorm:"column:row_count"`
			SHA256 string `gorm:"column:content_sha256"`
		}
		query := `SELECT count(*) AS row_count,encode(sha256(convert_to(COALESCE(string_agg(to_jsonb(t)::text,E'\n' ORDER BY to_jsonb(t)::text COLLATE "C"),''),'UTF8')),'hex') AS content_sha256 FROM ` + quote(parts[0]) + "." + quote(parts[1]) + ` t`
		if err := db.WithContext(ctx).Raw(query).Scan(&fingerprint).Error; err != nil {
			return err
		}
		if fingerprint.Rows != rule.DefaultRows || fingerprint.SHA256 != rule.DefaultSHA256 {
			return fmt.Errorf("non-default extension data in %s; maintenance stopped to preserve custom data", key)
		}
	}
	return nil
}

func extensionTableManifests(ctx context.Context, db *gorm.DB) (map[string]TableManifest, error) {
	columns, err := CatalogSchema(ctx, db)
	if err != nil {
		return nil, err
	}
	result := map[string]TableManifest{}
	for _, column := range columns {
		key := column.Schema + "." + column.Table
		rule, found := ExtensionCatalogRegistry[key]
		if !found {
			continue
		}
		name := key
		if column.Schema == "public" {
			name = column.Table
		}
		entry := TableManifest{Rows: rule.DefaultRows, SHA256: rule.DefaultSHA256, Columns: strings.Fields(rule.Columns), Regenerated: true}
		sort.Strings(entry.Columns)
		result[name] = entry
	}
	return result, nil
}

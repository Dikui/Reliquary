package maintenance

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Frozen schemas and replica formats 1 to 5 use these row column names.
// Conversion is table-specific; arbitrary nested user JSON and logs are opaque.
var legacyEntryColumns = map[string][][2]string{
	"entries":                {{"related_article_id", "related_entry_id"}},
	"ingest_discards":        {{"article_id", "entry_id"}},
	"topic_backfill_batches": {{"article_count", "entry_count"}},
	"topic_backfill_jobs":    {{"failed_articles", "failed_entries"}},
}

func entryColumn(table, column string, legacy bool) string {
	for _, pair := range legacyEntryColumns[table] {
		if !legacy && column == pair[0] {
			return pair[1]
		}
		if legacy && column == pair[1] {
			return pair[0]
		}
	}
	return column
}

func entryNamedRows(table string, rows []map[string]any, legacy bool, version int) []map[string]any {
	converted := make([]map[string]any, len(rows))
	for i, row := range rows {
		copy := make(map[string]any, len(row))
		for column, value := range row {
			if !legacy || version < 6 {
				column = entryColumn(table, column, legacy)
			}
			copy[column] = value
		}
		converted[i] = copy
	}
	return namedRows(converted, legacy, version)
}

func validateEntryReplicaColumns(table string, rows []map[string]any, format int, schema int64) error {
	for _, row := range rows {
		for _, pair := range legacyEntryColumns[table] {
			if _, old := row[pair[0]]; !old {
				continue
			}
			if _, current := row[pair[1]]; current {
				return fmt.Errorf("conflicting replica columns %s.%s and %s", table, pair[0], pair[1])
			}
			if format >= 6 && schema >= 23 {
				return fmt.Errorf("retired replica column %s.%s", table, pair[0])
			}
		}
	}
	return nil
}

// UsesLegacyEntryNames controls header conversion only after Load has validated
// the original manifest and file checksums. Public parsers accept current names.
func (s *Snapshot) UsesLegacyEntryNames() bool { return s.storedFormat < 6 || s.schemaVersion < 23 }

// projectEntryNamingManifest binds old column names to exactly the same rows.
// It never waives value, row-count, or file integrity checks.
func projectEntryNamingManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 23 || actual.SchemaVersion < 23 {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	left.Files = make(map[string][]File, len(expected.Files))
	for group, files := range expected.Files {
		left.Files[group] = append([]File(nil), files...)
	}
	for table, item := range expected.Tables {
		if _, affected := legacyEntryColumns[table]; !affected {
			left.Tables[table] = item
			continue
		}
		before, _ := json.MarshalIndent(item.Data, "", "  ")
		before = append(before, '\n')
		item.Columns = append([]string(nil), item.Columns...)
		for i, column := range item.Columns {
			item.Columns[i] = entryColumn(table, column, false)
		}
		if item.Data != nil {
			item.Data = entryNamedRows(table, item.Data, false, FormatVersion)
			canonical, _ := json.Marshal(canonicalRows(table, item.Data))
			item.SHA256 = sum(canonical)
			after, _ := json.MarshalIndent(item.Data, "", "  ")
			after = append(after, '\n')
			for group, files := range left.Files {
				for i, file := range files {
					if file.Path == ".reliquary/data/"+table+".json" && file.SHA256 == sum(before) && file.Size == int64(len(before)) {
						left.Files[group][i].SHA256, left.Files[group][i].Size = sum(after), int64(len(after))
					}
				}
			}
		}
		left.Tables[table] = item
	}
	right := *actual
	right.Tables = make(map[string]TableManifest, len(actual.Tables))
	for table, item := range actual.Tables {
		if _, affected := legacyEntryColumns[table]; affected && item.Data != nil {
			raw, _ := json.Marshal(canonicalRows(table, item.Data))
			item.SHA256 = sum(raw)
		}
		right.Tables[table] = item
	}
	return &left, &right
}

func legacyEntryRule(table string, rule Rule) Rule {
	rename := func(group string) string {
		columns := strings.Fields(group)
		for i, column := range columns {
			columns[i] = entryColumn(table, column, true)
		}
		return strings.Join(columns, " ")
	}
	rule.Header, rule.Sidecar = rename(rule.Header), rename(rule.Sidecar)
	rule.Configuration, rule.Regenerated = rename(rule.Configuration), rename(rule.Regenerated)
	return rule
}

// Convert only owned report metadata and task envelopes in old replicas.
// User metadata, custom_metadata, processing evidence, and log values are opaque.
func projectLegacyEntryJSON(snapshot *Snapshot) error {
	if !snapshot.UsesLegacyEntryNames() {
		return nil
	}
	rename := func(fields map[string]any, old, current string) error {
		value, present := fields[old]
		if !present {
			return nil
		}
		if _, conflict := fields[current]; conflict {
			return fmt.Errorf("conflicting legacy JSON fields %s and %s", old, current)
		}
		fields[current] = value
		delete(fields, old)
		return nil
	}
	for _, row := range snapshot.Tables["entries"] {
		if row["source_kind"] != "report" {
			continue
		}
		if fields, ok := row["metadata"].(map[string]any); ok {
			if err := rename(fields, "related_article_ids", "related_entry_ids"); err != nil {
				return err
			}
		}
	}
	for _, table := range []string{"task_pending_ops", "task_dead_letters", "task_jobs"} {
		for _, row := range snapshot.Tables[table] {
			if fields, ok := row["payload"].(map[string]any); ok {
				for _, pair := range [][2]string{{"knowledge_id", "entry_id"}, {"knowledge_ids", "entry_ids"}} {
					if err := rename(fields, pair[0], pair[1]); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// The frozen 9 to 10 migration resolves task library ownership with the old
// entry key. This lookup preserves the original payload during verification.
func legacyTaskEntryID(payload map[string]any) string {
	if id := stringValue(payload["entry_id"]); id != "" {
		return id
	}
	return stringValue(payload["knowledge_id"])
}

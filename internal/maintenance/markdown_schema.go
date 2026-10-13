package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"gorm.io/gorm"
)

// Historical backups keep their original fields until restored into schema 8.
var markdownRetiredColumns = map[string][]string{
	"entries": {"content_type_uncertain", "processing_review_status", "processing_review_content_type", "processing_review_reason", "processing_mode", "vault_original_path", "ingest_attachments"},
	"spaces":  {"parser_engine_config"},
}

func legacyMarkdownColumn(table, column string) bool {
	for _, name := range markdownRetiredColumns[table] {
		if column == name {
			return true
		}
	}
	return false
}

func markdownOnlySchema(ctx context.Context, db *gorm.DB) (bool, error) {
	var exists bool
	err := db.WithContext(ctx).Raw("SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='entries' AND column_name='processing_mode')").Scan(&exists).Error
	return !exists, err
}

func projectMarkdownRows(table string, rows []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, original := range rows {
		row := make(map[string]any, len(original))
		for k, v := range original {
			row[k] = v
		}
		for _, column := range markdownRetiredColumns[table] {
			delete(row, column)
		}
		switch table {
		case "entries":
			decision := "approved"
			if metadata, ok := row["metadata"].(map[string]any); ok {
				if intake, ok := metadata["intake"].(map[string]any); ok {
					if value, ok := intake["decision"].(string); ok {
						decision = value
					}
				}
			}
			if row["follow_ready_at"] == nil && row["deleted_at"] == nil && row["source_kind"] == "article" && row["parse_status"] == "completed" && row["enable_status"] == "enabled" && decision == "approved" {
				row["follow_ready_at"] = row["created_at"]
			}
			if row["source_kind"] != "report" && (row["content_type"] == nil || row["content_type"] == "") {
				row["content_type"] = "other"
			}
			if materials, ok := row["missing_materials"].([]any); ok {
				images := []any{}
				for _, item := range materials {
					if object, ok := item.(map[string]any); ok && object["type"] == "image" {
						images = append(images, item)
					}
				}
				row["missing_materials"] = images
				if len(images) == 0 {
					row["incomplete"] = false
				}
			}
			if metadata, ok := row["metadata"].(map[string]any); ok {
				raw, _ := json.Marshal(metadata)
				var copied map[string]any
				decoder := json.NewDecoder(bytes.NewReader(raw))
				decoder.UseNumber()
				_ = decoder.Decode(&copied)
				if config, ok := copied["process_overrides"].(map[string]any); ok {
					delete(config, "parser_engine_rules")
					delete(config, "parser_engine_overrides")
					if chunks, ok := config["chunking_config"].(map[string]any); ok {
						delete(chunks, "parser_engine_rules")
						delete(chunks, "table_metadata_instructions")
					}
				}
				row["metadata"] = copied
			}
		case "libraries":
			if config, ok := row["chunking_config"].(map[string]any); ok {
				copy := map[string]any{}
				for k, v := range config {
					if k != "parser_engine_rules" && k != "table_metadata_instructions" {
						copy[k] = v
					}
				}
				row["chunking_config"] = copy
			}
		case "ingest_assets":
			if !strings.Contains(" jpg jpeg png gif bmp tiff webp ", " "+stringValue(row["file_type"])+" ") {
				continue
			}
		}
		result = append(result, row)
	}
	return result
}
func stringValue(value any) string { s, _ := value.(string); return s }

func projectMarkdownSnapshot(snapshot *Snapshot) {
	for table, rows := range snapshot.Tables {
		snapshot.Tables[table] = projectMarkdownRows(table, rows)
		for id, columns := range snapshot.JSONNulls[table] {
			kept := []string{}
			for _, column := range columns {
				if !legacyMarkdownColumn(table, column) {
					kept = append(kept, column)
				}
			}
			snapshot.JSONNulls[table][id] = kept
		}
	}
	for i := range snapshot.Required {
		if snapshot.Required[i].Kind == "spaces" {
			kept := []string{}
			for _, field := range snapshot.Required[i].Fields {
				if !strings.HasPrefix(field, "parser_engine_config.") {
					kept = append(kept, field)
				}
			}
			snapshot.Required[i].Fields = kept
		}
	}
}

// Apply only the explicit schema 7 to 8 projection. Any other row or file
// difference remains visible to verification.
func projectMarkdownManifest(expected, actual, fileOrigin *Manifest, legacyFormat int) (*Manifest, *Manifest) {
	old, current := expected.Tables["entries"], actual.Tables["entries"]
	has := func(columns []string, name string) bool {
		for _, column := range columns {
			if column == name {
				return true
			}
		}
		return false
	}
	if !has(old.Columns, "processing_mode") || has(current.Columns, "processing_mode") {
		return expected, actual
	}
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	left.Files = make(map[string][]File, len(expected.Files))
	snapshot := &Snapshot{Tables: map[string][]map[string]any{}, JSONNulls: map[string]map[string][]string{}, Required: append([]Requirement(nil), expected.Required...)}
	for table, entry := range expected.Tables {
		snapshot.Tables[table] = entry.Data
		if entry.JSONNulls != nil {
			snapshot.JSONNulls[table] = make(map[string][]string, len(entry.JSONNulls))
			for id, columns := range entry.JSONNulls {
				snapshot.JSONNulls[table][id] = append([]string(nil), columns...)
			}
		}
	}
	projectMarkdownSnapshot(snapshot)
	left.Required = snapshot.Required
	for table, entry := range expected.Tables {
		entry.Data = snapshot.Tables[table]
		if !entry.Regenerated {
			entry.Rows = int64(len(entry.Data))
		}
		entry.JSONNulls = snapshot.JSONNulls[table]
		columns := []string{}
		for _, column := range entry.Columns {
			if !legacyMarkdownColumn(table, column) {
				columns = append(columns, column)
			}
		}
		entry.Columns = columns
		if !entry.Regenerated {
			raw, _ := json.Marshal(canonicalRows(table, entry.Data))
			entry.SHA256 = sum(raw)
		}
		left.Tables[table] = entry
	}
	for group, files := range expected.Files {
		for _, file := range files {
			if group == "vault" && strings.HasSuffix(file.Path, ".json") {
				prefix := ".reliquary/data/"
				if strings.HasPrefix(file.Path, ".reliquary/config/") {
					prefix = ".reliquary/config/"
				}
				if strings.HasPrefix(file.Path, prefix) {
					table := currentTable(strings.TrimSuffix(strings.TrimPrefix(file.Path, prefix), ".json"))
					before, present := fileOrigin.Tables[table]
					after, retained := left.Tables[table]
					if present && retained {
						for _, rows := range [][]map[string]any{before.Data, namedRows(before.Data, true, legacyFormat)} {
							oldBytes, _ := json.MarshalIndent(rows, "", "  ")
							oldBytes = append(oldBytes, '\n')
							if file.SHA256 != sum(oldBytes) || file.Size != int64(len(oldBytes)) {
								continue
							}
							newBytes, _ := json.MarshalIndent(after.Data, "", "  ")
							newBytes = append(newBytes, '\n')
							file.Path, file.SHA256, file.Size = prefix+table+".json", sum(newBytes), int64(len(newBytes))
							break
						}
					}
				}
			}
			left.Files[group] = append(left.Files[group], file)
		}
	}
	return &left, actual
}

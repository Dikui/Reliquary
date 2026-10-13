package maintenance

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatVersion is the personal-space replica and verification manifest format.
const FormatVersion = 5

func supportedFormat(version int) bool { return version >= 1 && version <= FormatVersion }

// Only SQL row envelopes are renamed. Nested JSON, queue payloads and log values
// remain byte-for-byte equivalent JSON values.
func currentTable(table string) string {
	switch table {
	case "knowledge_bases":
		return "libraries"
	case "knowledges":
		return "entries"
	case "knowledge_entities":
		return "entry_entities"
	case "knowledge_topics":
		return "entry_topics"
	case "knowledge_processing_spans":
		return "entry_processing_spans"
	case "knowledge_repost_sources":
		return "entry_repost_sources"
	case "tenants":
		return "spaces"
	case "tenant_api_keys":
		return "api_keys"
	default:
		return table
	}
}

// Legacy row envelopes are versioned; opaque JSON is never traversed.
func legacyTable(table string, versions ...int) string {
	version := 1
	if len(versions) > 0 {
		version = versions[0]
	}
	if version < 4 && table == "libraries" {
		return "knowledge_bases"
	}
	if version == 1 {
		switch table {
		case "spaces":
			return "tenants"
		case "api_keys":
			return "tenant_api_keys"
		}
	}
	if version < 3 {
		switch table {
		case "entries":
			return "knowledges"
		case "entry_entities":
			return "knowledge_entities"
		case "entry_topics":
			return "knowledge_topics"
		case "entry_processing_spans":
			return "knowledge_processing_spans"
		case "entry_repost_sources":
			return "knowledge_repost_sources"
		}
	}
	return table
}

func namedRows(rows []map[string]any, legacy bool, versions ...int) []map[string]any {
	version := 1
	if len(versions) > 0 {
		version = versions[0]
	}
	result := make([]map[string]any, len(rows))
	for i, row := range rows {
		copy := make(map[string]any, len(row))
		for column, value := range row {
			if legacy {
				if version < 4 && column == "library_id" {
					column = "knowledge_base_id"
				}
				if version == 1 && column == "space_id" {
					column = "tenant_id"
				}
				if version < 3 {
					switch column {
					case "entry_id":
						column = "knowledge_id"
					case "entry_ids":
						column = "knowledge_ids"
					}
				}
			} else {
				switch column {
				case "knowledge_base_id":
					column = "library_id"
				case "tenant_id":
					column = "space_id"
				case "knowledge_id":
					column = "entry_id"
				case "knowledge_ids":
					column = "entry_ids"
				}
			}
			copy[column] = value
		}
		result[i] = copy
	}
	sortRows(result)
	return result
}

func currentRequirements(required []Requirement) []Requirement {
	result := append([]Requirement(nil), required...)
	for i := range result {
		result[i].Kind = currentTable(result[i].Kind)
	}
	return result
}

func currentSnapshot(snapshot *Snapshot) error {
	if !supportedFormat(snapshot.FormatVersion) {
		return fmt.Errorf("unsupported replica format %d", snapshot.FormatVersion)
	}
	tables := make(map[string][]map[string]any, len(snapshot.Tables))
	nulls := make(map[string]map[string][]string, len(snapshot.JSONNulls))
	for oldTable, rows := range snapshot.Tables {
		table := currentTable(oldTable)
		if _, exists := tables[table]; exists {
			return fmt.Errorf("duplicate replica table mapping %s", table)
		}
		tables[table] = namedRows(rows, false)
		if previous := snapshot.JSONNulls[oldTable]; previous != nil {
			nulls[table] = map[string][]string{}
			for _, row := range rows {
				if columns, present := previous[rowIdentity(row)]; present {
					nulls[table][rowIdentity(namedRows([]map[string]any{row}, false)[0])] = columns
				}
			}
		}
	}
	snapshot.Tables, snapshot.JSONNulls = tables, nulls
	snapshot.Required = currentRequirements(snapshot.Required)
	snapshot.FormatVersion = FormatVersion
	return nil
}

// Compare old backup hashes in their original naming vocabulary. The rows and
// all business comparisons still use current SQL names. No nested key changes.
func currentManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.FormatVersion >= FormatVersion || actual.FormatVersion != FormatVersion {
		return expected, actual
	}
	left, right := *expected, *actual
	left.FormatVersion = FormatVersion
	left.Required = currentRequirements(expected.Required)
	left.Tables, right.Tables = map[string]TableManifest{}, map[string]TableManifest{}
	for old, entry := range expected.Tables {
		table := currentTable(old)
		if entry.Data != nil {
			entry.Data = namedRows(entry.Data, false)
		}
		entry.Columns = append([]string(nil), entry.Columns...)
		for i, column := range entry.Columns {
			switch column {
			case "knowledge_base_id":
				entry.Columns[i] = "library_id"
			case "tenant_id":
				entry.Columns[i] = "space_id"
			case "knowledge_id":
				entry.Columns[i] = "entry_id"
			case "knowledge_ids":
				entry.Columns[i] = "entry_ids"
			}
		}
		left.Tables[table] = entry
	}
	for table, entry := range actual.Tables {
		if entry.legacySHA256[expected.FormatVersion] != "" {
			entry.SHA256 = entry.legacySHA256[expected.FormatVersion]
		}
		right.Tables[table] = entry
	}
	// Bind a changed replica file to both naming representations of the same
	// rows. Raw originals and unrelated assets retain their strict byte checks.
	left.Files = map[string][]File{}
	for group, files := range expected.Files {
		left.Files[group] = append([]File(nil), files...)
		for i, before := range left.Files[group] {
			if group != "vault" || !strings.HasPrefix(before.Path, ".reliquary/data/") {
				continue
			}
			table := currentTable(strings.TrimSuffix(strings.TrimPrefix(before.Path, ".reliquary/data/"), ".json"))
			entry := actual.Tables[table]
			if entry.Data == nil {
				continue
			}
			oldData, _ := json.MarshalIndent(namedRows(entry.Data, true, expected.FormatVersion), "", "  ")
			newData, _ := json.MarshalIndent(entry.Data, "", "  ")
			oldData, newData = append(oldData, '\n'), append(newData, '\n')
			if before.SHA256 != sum(oldData) || before.Size != int64(len(oldData)) {
				continue
			}
			for _, after := range actual.Files[group] {
				if after.Path == ".reliquary/data/"+table+".json" && after.SHA256 == sum(newData) && after.Size == int64(len(newData)) {
					left.Files[group][i] = after
				}
			}
		}
	}
	return &left, &right
}

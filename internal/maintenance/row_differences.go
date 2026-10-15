package maintenance

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Composite identities remain readable even when non-key fields are altered.
func diagnosticIdentity(table string, row map[string]any) string {
	if id, ok := row["id"]; ok {
		return fmt.Sprint(id)
	}
	keys := map[string][]string{
		"relation_sources":           {"relation_id", "entry_id"},
		"relation_suppressions":      {"library_id", "subject_id", "relation_type", "object_id"},
		"relation_type_merges":       {"library_id", "from_type"},
		"evaluation_answers":         {"question_id", "entry_id"},
		"evaluation_run_results":     {"run_id", "question_id"},
		"library_content_types":      {"library_id", "name"},
		"library_relation_types":     {"library_id", "name"},
		"entry_processing_outputs":   {"entry_id"},
		"entry_entities":             {"entry_id", "entity_id"},
		"entry_topics":               {"entry_id", "topic_id"},
		"follow_feed_library_states": {"space_id", "library_id"},
	}
	identity := map[string]any{}
	for _, key := range keys[table] {
		identity[key] = row[key]
	}
	if len(identity) == 0 {
		identity = row
	}
	data, _ := json.Marshal(identity)
	return string(data)
}

func rowDifferences(table string, expected, actual []map[string]any) []string {
	index := func(rows []map[string]any) map[string]string {
		result := map[string]string{}
		for _, row := range rows {
			data, _ := json.Marshal(row)
			result[diagnosticIdentity(table, row)] = string(data)
		}
		return result
	}
	left, right := index(expected), index(actual)
	result := []string{}
	for id, value := range left {
		if right[id] != value {
			result = append(result, "table "+table+" row "+id+" changed or missing")
		}
	}
	for id := range right {
		if _, ok := left[id]; !ok {
			result = append(result, "table "+table+" row "+id+" unexpected")
		}
	}
	sort.Strings(result)
	return result
}

package maintenance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

func samePolicyRows(left, right []map[string]any) bool {
	before, _ := json.Marshal(left)
	after, _ := json.Marshal(right)
	return bytes.Equal(before, after)
}

func policyAuditRows(value any) ([]map[string]any, bool) {
	rows, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := []map[string]any{}
	for _, value := range rows {
		row, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		result = append(result, row)
	}
	return result, validPolicyRows(result)
}

func validPolicyRows(rows []map[string]any) bool {
	if len(rows) > 1 {
		return false
	}
	columns := Rules["chatgpt_policy"].Columns()
	for _, row := range rows {
		if len(row) != len(columns) || fmt.Sprint(row["id"]) != "1" {
			return false
		}
		for column := range row {
			if _, found := columns[column]; !found {
				return false
			}
		}
	}
	return true
}

func policyConfigurationMatches(original, current []map[string]any) bool {
	if !validPolicyRows(current) || len(current) == 0 {
		return false
	}
	configuration := map[string]any{"id": json.Number("1"), "daily_limit": nil, "fallback_model_id": ""}
	if len(original) == 1 {
		configuration = original[0]
	}
	for _, field := range []string{"id", "daily_limit", "fallback_model_id"} {
		before, _ := json.Marshal(configuration[field])
		after, _ := json.Marshal(current[0][field])
		if !bytes.Equal(before, after) {
			return false
		}
	}
	return true
}

// New maintenance probes may consume real quota. A complete strictly ordered
// before/after chain proves every permitted mutable field transition. Policy
// configuration remains the original value, including default configuration
// when the first real reservation creates a previously absent policy row.
func verifyPolicyProbeChain(expected, actual *Manifest) bool {
	original := expected.Tables["chatgpt_policy"].Data
	current := actual.Tables["chatgpt_policy"].Data
	if original == nil {
		original = []map[string]any{}
	}
	if current == nil {
		current = []map[string]any{}
	}
	if !validPolicyRows(original) || !validPolicyRows(current) {
		return false
	}
	oldLogs := map[string]bool{}
	for _, row := range expected.Tables["operation_logs"].Data {
		oldLogs[fmt.Sprint(row["id"])] = true
	}
	chatGPTModels := map[string]bool{}
	for _, row := range actual.Tables["models"].Data {
		if row["source"] == "chatgpt" && row["type"] == "KnowledgeQA" {
			chatGPTModels[fmt.Sprint(row["id"])] = true
		}
	}
	type transition struct {
		when          time.Time
		before, after []map[string]any
	}
	transitions := []transition{}
	for _, row := range actual.Tables["operation_logs"].Data {
		if oldLogs[fmt.Sprint(row["id"])] || row["operation_type"] != "maintenance_model_probe" {
			continue
		}
		when, err := time.Parse(time.RFC3339Nano, fmt.Sprint(row["created_at"]))
		before, beforeValid := policyAuditRows(row["before_value"])
		after, afterValid := policyAuditRows(row["after_value"])
		if err != nil || row["actor"] != "maintenance" || fmt.Sprint(row["space_id"]) != "1" || !chatGPTModels[fmt.Sprint(row["object_id"])] || !beforeValid || !afterValid || samePolicyRows(before, after) || !policyConfigurationMatches(original, after) {
			return false
		}
		transitions = append(transitions, transition{when: when, before: before, after: after})
	}
	sort.Slice(transitions, func(i, j int) bool { return transitions[i].when.Before(transitions[j].when) })
	previous := original
	var lastTime time.Time
	for _, transition := range transitions {
		if !samePolicyRows(previous, transition.before) || (!lastTime.IsZero() && !transition.when.After(lastTime)) {
			return false
		}
		previous = transition.after
		lastTime = transition.when
	}
	return samePolicyRows(previous, current)
}

func parserCredentialRequirement(field string) bool {
	return strings.HasPrefix(field, "parser_engine_config.")
}

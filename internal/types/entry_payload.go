package types

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// New tasks write entry keys. Convert legacy backup task envelopes only,
// rejecting conflicts without traversing arbitrary nested JSON.
func normalizeEntryPayload(fields map[string]json.RawMessage) error {
	for _, pair := range [][2]string{{"knowledge_id", "entry_id"}, {"knowledge_ids", "entry_ids"}} {
		var seen bool
		var canonical any
		var raw json.RawMessage
		for key, value := range fields {
			if !strings.EqualFold(key, pair[0]) && !strings.EqualFold(key, pair[1]) {
				continue
			}
			var decoded any
			if pair[0] == "knowledge_id" {
				var id string
				if string(value) == "null" {
					return fmt.Errorf("task entry must be a string")
				}
				if err := json.Unmarshal(value, &id); err != nil {
					return err
				}
				decoded = id
			} else {
				var ids []string
				if err := json.Unmarshal(value, &ids); err != nil {
					return err
				}
				decoded = ids
			}
			if seen && !reflect.DeepEqual(canonical, decoded) {
				return fmt.Errorf("conflicting task entry keys")
			}
			seen, canonical, raw = true, decoded, value
			delete(fields, key)
		}
		if seen {
			fields[pair[1]] = raw
		}
	}
	return nil
}

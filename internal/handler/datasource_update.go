package handler

import (
	"encoding/json"
	"reliquary/internal/types"
)

// Only submitted editable fields replace stored values. Settings objects are
// patched recursively; explicit null and empty arrays remain distinct.
func dataSourceUpdateFields(existing *types.DataSource, patch map[string]json.RawMessage) (*types.DataSource, error) {
	result := *existing
	fields := map[string]json.RawMessage{}
	var err error
	for _, key := range []string{"name", "type", "default_content_type", "sync_schedule", "sync_mode", "sync_deletions", "status", "config"} {
		value, present := patch[key]
		if !present {
			continue
		}
		if key == "config" {
			value, err = patchDataSourceJSON(json.RawMessage(existing.Config), value)
			if err != nil {
				return nil, err
			}
		}
		fields[key] = value
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(encoded, &result)
	return &result, err
}
func patchDataSourceJSON(original, patch json.RawMessage) (json.RawMessage, error) {
	var incoming map[string]json.RawMessage
	if err := json.Unmarshal(patch, &incoming); err != nil {
		return nil, err
	}
	if incoming == nil {
		return patch, nil
	}
	var stored map[string]json.RawMessage
	_ = json.Unmarshal(original, &stored)
	if stored == nil {
		stored = map[string]json.RawMessage{}
	}
	for key, value := range incoming {
		var object map[string]json.RawMessage
		if json.Unmarshal(value, &object) == nil && object != nil {
			merged, err := patchDataSourceJSON(stored[key], value)
			if err != nil {
				return nil, err
			}
			value = merged
		}
		stored[key] = value
	}
	return json.Marshal(stored)
}

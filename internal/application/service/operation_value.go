package service

import (
	"encoding/json"

	"reliquary/internal/types"
)

// Editing history retains its persisted ownership key. Only the typed row
// values written by topic editing are adapted; arbitrary user JSON stays intact.
func marshalOperationValue(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil || string(data) == "null" {
		return data, err
	}
	row := func(data json.RawMessage) (json.RawMessage, error) {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return nil, err
		}
		if owner, ok := fields["space_id"]; ok {
			fields["tenant_id"] = owner
			delete(fields, "space_id")
		}
		return json.Marshal(fields)
	}
	switch value.(type) {
	case types.Topic, *types.Topic, types.KnowledgeTopic, *types.KnowledgeTopic:
		return row(data)
	case []types.KnowledgeTopic:
		var rows []json.RawMessage
		if err := json.Unmarshal(data, &rows); err != nil {
			return nil, err
		}
		for i := range rows {
			rows[i], err = row(rows[i])
			if err != nil {
				return nil, err
			}
		}
		return json.Marshal(rows)
	default:
		return data, nil
	}
}

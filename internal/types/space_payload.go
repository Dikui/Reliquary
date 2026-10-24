package types

import (
	"encoding/json"
	"fmt"
	"strings"
)

// UnmarshalSpacePayload accepts both generations of the persisted queue owner
// key. Writers keep the established key; mismatched aliases are rejected.
func UnmarshalSpacePayload(data []byte, destination any) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	var owner *uint64
	for name, value := range fields {
		if !strings.EqualFold(name, "tenant_id") && !strings.EqualFold(name, "space_id") {
			continue
		}
		var id uint64
		if string(value) == "null" {
			return fmt.Errorf("task space must be an integer")
		}
		if err := json.Unmarshal(value, &id); err != nil {
			return err
		}
		if owner != nil && *owner != id {
			return fmt.Errorf("conflicting task space keys")
		}
		owner = &id
		delete(fields, name)
	}
	if owner != nil {
		fields["tenant_id"], _ = json.Marshal(*owner)
	}
	if err := normalizeEntryPayload(fields); err != nil {
		return err
	}
	normalized, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(normalized, destination)
}

func (p *DocumentProcessPayload) UnmarshalJSON(data []byte) error {
	type plain DocumentProcessPayload
	return UnmarshalSpacePayload(data, (*plain)(p))
}

func (p *ManualProcessPayload) UnmarshalJSON(data []byte) error {
	type plain ManualProcessPayload
	return UnmarshalSpacePayload(data, (*plain)(p))
}

func (p *EntryListDeletePayload) UnmarshalJSON(data []byte) error {
	type plain EntryListDeletePayload
	return UnmarshalSpacePayload(data, (*plain)(p))
}

func (p *EntryListReparsePayload) UnmarshalJSON(data []byte) error {
	type plain EntryListReparsePayload
	return UnmarshalSpacePayload(data, (*plain)(p))
}

func (p *EmbeddingRebuildPayload) UnmarshalJSON(data []byte) error {
	type plain EmbeddingRebuildPayload
	return UnmarshalSpacePayload(data, (*plain)(p))
}

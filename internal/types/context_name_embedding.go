package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// ContextNameEmbedding stores a pgvector value with dimensions recorded beside it.
type ContextNameEmbedding []float32

func (v ContextNameEmbedding) Value() (driver.Value, error) {
	if len(v) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}

func (v *ContextNameEmbedding) Scan(value any) error {
	if value == nil {
		*v = nil
		return nil
	}
	var raw []byte
	switch value := value.(type) {
	case []byte:
		raw = value
	case string:
		raw = []byte(value)
	default:
		return fmt.Errorf("unsupported context name vector value %T", value)
	}
	return json.Unmarshal(raw, v)
}

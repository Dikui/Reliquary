package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// EntityNameEmbedding stores a pgvector value with dimensions recorded beside it.
type EntityNameEmbedding []float32

func (v EntityNameEmbedding) Value() (driver.Value, error) {
	if len(v) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(v)
	return string(b), err
}

func (v *EntityNameEmbedding) Scan(value any) error {
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
		return fmt.Errorf("unsupported entity name vector value %T", value)
	}
	return json.Unmarshal(raw, v)
}

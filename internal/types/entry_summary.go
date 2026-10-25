package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

type EntrySummary struct {
	Overview string `json:"overview" yaml:"overview"`
}

// RenderText supplies the overview for previews and keyword search.
func (s *EntrySummary) RenderText() string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.Overview)
}

func (s EntrySummary) Value() (driver.Value, error) { return json.Marshal(s) }

func (s *EntrySummary) Scan(value interface{}) error {
	if value == nil {
		*s = EntrySummary{}
		return nil
	}
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("invalid structured summary database value %T", value)
	}
	return json.Unmarshal(data, s)
}

// EntrySummarySearchID identifies a summary hit without a persisted body chunk.
func EntrySummarySearchID(entryID string) string { return "knowledge-summary:" + entryID }

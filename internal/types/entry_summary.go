package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

type EntrySummary struct {
	Overview string   `json:"overview" yaml:"overview"`
	Points   []string `json:"points" yaml:"points"`
	KeyTerms []string `json:"key_terms" yaml:"key_terms"`
}

// RenderText supplies deterministic Markdown for existing previews and keyword search.
func (s *EntrySummary) RenderText() string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 概述\n\n")
	b.WriteString(strings.TrimSpace(s.Overview))
	b.WriteString("\n\n## 要点\n\n")
	for _, point := range s.Points {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(point))
		b.WriteByte('\n')
	}
	b.WriteString("\n## 关键名词\n\n")
	b.WriteString(strings.Join(s.KeyTerms, "、"))
	return strings.TrimSpace(b.String())
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

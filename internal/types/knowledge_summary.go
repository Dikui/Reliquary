package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ContentTypeEvent          = "event"
	ContentTypeOpinion        = "opinion"
	ContentTypeResearch       = "research"
	ContentTypeTutorial       = "tutorial"
	ContentTypeCase           = "case"
	ContentTypeReference      = "reference"
	ProcessingModeFull        = "full"
	ProcessingModeSummaryOnly = "summary_only"
)

var ContentTypes = []string{ContentTypeEvent, ContentTypeOpinion, ContentTypeResearch, ContentTypeTutorial, ContentTypeCase, ContentTypeReference}

// NormalizeContentType leaves an unsupported classification empty.
func NormalizeContentType(value string) string {
	value = strings.TrimSpace(value)
	for _, allowed := range ContentTypes {
		if value == allowed {
			return value
		}
	}
	return ""
}

func ProcessingModeForContentType(value string) string {
	switch value {
	case ContentTypeTutorial, ContentTypeCase, ContentTypeReference:
		return ProcessingModeSummaryOnly
	default:
		return ProcessingModeFull
	}
}

// KnowledgeSummary is the common article summary contract.
type KnowledgeSummary struct {
	Overview string   `json:"overview"`
	Points   []string `json:"points"`
	KeyTerms []string `json:"key_terms"`
}

// RenderText supplies deterministic Markdown for existing previews and keyword search.
func (s *KnowledgeSummary) RenderText() string {
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

func (s KnowledgeSummary) Value() (driver.Value, error) { return json.Marshal(s) }

func (s *KnowledgeSummary) Scan(value interface{}) error {
	if value == nil {
		*s = KnowledgeSummary{}
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

// KnowledgeSummarySearchID identifies a summary hit without a persisted body chunk.
func KnowledgeSummarySearchID(knowledgeID string) string { return "knowledge-summary:" + knowledgeID }

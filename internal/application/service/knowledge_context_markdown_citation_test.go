package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
)

func TestKnowledgeContextMarkdownCitationCurrentBehavior(t *testing.T) {
	body := "Acme 发布了 **新模型**。\n\n详情请见 [Acme 公告](https://example.com/acme)。\n\n| 对象 | 版本 |\n| --- | --- |\n| Acme | v2 |"
	cases := []struct {
		name    string
		mention string
		quote   string
		accept  bool
	}{
		{name: "bold/verbatim", mention: "Acme", quote: "Acme 发布了 **新模型**。", accept: true},
		{name: "bold/rendered_sentence", mention: "Acme", quote: "Acme 发布了 新模型。", accept: false},
		{name: "bold/plain_excerpt", mention: "新模型", quote: "新模型", accept: true},
		{name: "link/verbatim", mention: "Acme", quote: "详情请见 [Acme 公告](https://example.com/acme)。", accept: true},
		{name: "link/rendered_sentence", mention: "Acme", quote: "详情请见 Acme 公告。", accept: false},
		{name: "link/plain_excerpt", mention: "Acme", quote: "Acme 公告", accept: true},
		{name: "table/verbatim", mention: "Acme", quote: "| Acme | v2 |", accept: true},
		{name: "table/rendered_row", mention: "Acme", quote: "Acme v2", accept: false},
		{name: "table/plain_excerpt", mention: "Acme", quote: "Acme", accept: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.accept, verbatimQuote(body, tc.quote))
			entity := contextExtractedEntity{Mention: tc.mention, Canonical: tc.mention, EntityType: "model", Quote: tc.quote}
			entityQuote := entity
			entityQuote.Quote = tc.mention
			entityExtraction := contextExtraction{
				Entities:  []contextExtractedEntity{entity},
				Attention: contextExtractedAttention{Reason: "Source-supported quote."},
			}
			factExtraction := contextExtraction{
				Entities: []contextExtractedEntity{entityQuote},
				Facts: []contextExtractedFact{{
					Kind: types.KnowledgeFactAttribute, Subject: tc.mention, Predicate: "status",
					Value: json.RawMessage(`"announced"`), ValueType: "string", Quote: tc.quote,
				}},
				Attention: contextExtractedAttention{Reason: "Source-supported quote."},
			}
			eventExtraction := contextExtraction{
				Entities:  []contextExtractedEntity{entityQuote},
				Events:    []contextExtractedEvent{{Title: "Model announced", EventType: "release", EntityMentions: []string{tc.mention}, Quote: tc.quote}},
				Attention: contextExtractedAttention{Reason: "Source-supported quote."},
			}
			for _, extraction := range []struct {
				name  string
				value contextExtraction
			}{{"entity", entityExtraction}, {"fact", factExtraction}, {"event", eventExtraction}} {
				t.Run(extraction.name, func(t *testing.T) {
					err := validateContextExtraction(extraction.value, body)
					if tc.accept {
						require.NoError(t, err)
					} else {
						require.Error(t, err, "Rendered Markdown quotes currently fail exact source validation")
					}
				})
			}
		})
	}
}

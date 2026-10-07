package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

func TestKnowledgeContextMarkdownCitationValidation(t *testing.T) {
	body := "Acme 发布了 **新模型**。\n\nAcme 支持 *快速运行* 和 _低延迟_。\n\nAcme 支持 `run --fast`。\n\n详情请见 [Acme 公告](https://example.com/acme)。\n\n| 对象 | 版本 |\n| --- | --- |\n| Acme | v2 |"
	cases := []struct {
		name    string
		mention string
		quote   string
		accept  bool
	}{
		{name: "bold/verbatim", mention: "Acme", quote: "Acme 发布了 **新模型**。", accept: true},
		{name: "bold/rendered_sentence", mention: "Acme", quote: "Acme 发布了 新模型。", accept: true},
		{name: "italic/rendered_sentence", mention: "Acme", quote: "Acme 支持 快速运行 和 低延迟。", accept: true},
		{name: "code/rendered_sentence", mention: "Acme", quote: "Acme 支持 run --fast。", accept: true},
		{name: "bold/plain_excerpt", mention: "新模型", quote: "新模型", accept: true},
		{name: "link/verbatim", mention: "Acme", quote: "详情请见 [Acme 公告](https://example.com/acme)。", accept: true},
		{name: "link/rendered_sentence", mention: "Acme", quote: "详情请见 Acme 公告。", accept: true},
		{name: "link/plain_excerpt", mention: "Acme", quote: "Acme 公告", accept: true},
		{name: "table/verbatim", mention: "Acme", quote: "| Acme | v2 |", accept: true},
		{name: "table/rendered_row", mention: "Acme", quote: "Acme v2", accept: true},
		{name: "table/plain_excerpt", mention: "Acme", quote: "Acme", accept: true},
		{name: "non_contiguous", mention: "Acme", quote: "Acme 发布了 新模型 v2", accept: false},
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
						require.Error(t, err, "Non-contiguous normalized excerpts must fail source validation")
					}
				})
			}
		})
	}
}

func TestMarkdownCitationNormalizationAndSourceRanges(t *testing.T) {
	data, err := os.ReadFile("testdata/markdown_citations.json")
	require.NoError(t, err)
	var cases []struct {
		Name, Body, Quote, Normalized string
		Matches                       []string
	}
	require.NoError(t, json.Unmarshal(data, &cases))
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			require.Equal(t, tc.Normalized, normalizeMarkdownCitation(tc.Body).text)
			ranges := markdownCitationRanges(tc.Body, tc.Quote)
			matches := make([]string, 0, len(ranges))
			for _, match := range ranges {
				matches = append(matches, tc.Body[match.start:match.end])
			}
			require.Equal(t, tc.Matches, matches)
			require.Equal(t, len(tc.Matches) > 0, verbatimQuote(tc.Body, tc.Quote))
		})
	}
}

func TestKnowledgeContextMarkdownCitationPreservesModelQuote(t *testing.T) {
	t.Setenv("RELIQUARY_CONTEXT_ATTENTION_ENABLED", "false")
	db, ctx, svc := p3ContextFixture(t)
	body := "Acme **Model** 发布了 *新版本*，支持 `run --fast`。"
	quote := "  Acme Model 发布了 新版本，支持 run --fast。  "
	k := p3Message(t, db, "markdown-message", body)
	extraction := contextExtraction{
		Entities:  []contextExtractedEntity{{Mention: "Acme Model", Canonical: "Acme Model", EntityType: "model", Quote: quote}},
		Facts:     []contextExtractedFact{{Kind: types.KnowledgeFactAttribute, Subject: "Acme Model", Predicate: "status", Value: json.RawMessage(`"released"`), ValueType: "string", Quote: quote}},
		Events:    []contextExtractedEvent{{Title: "Acme Model 新版本发布", EventType: "release", EntityMentions: []string{"Acme Model"}, Quote: quote}},
		Attention: contextExtractedAttention{Reason: "Source-supported model quote."},
	}
	encoded, err := json.Marshal(extraction)
	require.NoError(t, err)
	svc.models = knowledgeContextTestModelService{model: &p3CountingChat{reply: func(_ context.Context, messages []chat.Message) string {
		if strings.Contains(messages[0].Content, "same real-world object") {
			return `{"decision":"new","confidence":1}`
		}
		if strings.Contains(messages[0].Content, "same specific real-world change") {
			return `{"relation":"UNRELATED","confidence":1}`
		}
		return string(encoded)
	}}}
	require.NoError(t, svc.ProcessMessage(ctx, k, body))
	var evidence types.KnowledgeEvidence
	var fact types.KnowledgeFact
	var link types.KnowledgeEventEvidence
	var alias types.KnowledgeEntityAlias
	require.NoError(t, db.Where("knowledge_id = ?", k.ID).First(&evidence).Error)
	require.NoError(t, db.Where("evidence_id = ?", evidence.ID).First(&fact).Error)
	require.NoError(t, db.Where("evidence_id = ?", evidence.ID).First(&link).Error)
	require.NoError(t, db.Where("evidence_id = ?", evidence.ID).First(&alias).Error)
	require.Equal(t, body, evidence.Body)
	require.Equal(t, types.KnowledgeExtractionComplete, evidence.ExtractionStatus)
	require.Equal(t, quote, fact.Quote)
	require.Equal(t, evidence.ID, link.EvidenceID)
	require.Equal(t, quote, alias.Quote)
}

func TestMarkdownCitationWorkflowExcerptKeepsNormalizedQuote(t *testing.T) {
	evidence := types.KnowledgeEvidence{ID: "markdown-source", Body: "Acme 发布了 **新模型**。"}
	quote := "Acme 发布了 新模型。"
	citation := workflowCitationFromEvidence(&types.Knowledge{ID: "message"}, evidence, quote)
	require.Equal(t, quote, citation.Quote)
}

func TestKnowledgeContextMarkdownCitationRestrictedAliasVisibility(t *testing.T) {
	db, ctx, svc := p2ContextFixture(t)
	require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "markdown-shared", TenantID: 9, EntityType: "company", CanonicalName: "Aurora", NormalizedName: "aurora"}).Error)
	for _, source := range []struct{ kbID, id, body, alias string }{
		{"energy-one", "markdown-visible", "ACME **Model** has 10 staff.", "Acme Model"},
		{"energy-two", "markdown-hidden", "Secret *Model* has 20 staff.", "Secret Model"},
	} {
		p2ContextMessage(t, db, source.id, source.kbID, source.body)
		require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: source.id, TenantID: 9, KnowledgeBaseID: source.kbID, KnowledgeID: source.id, Version: 1, BodyHash: strings.Repeat("a", 64), Body: source.body, Status: types.KnowledgeEvidenceCurrent}).Error)
		require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: source.id, TenantID: 9, KnowledgeBaseID: source.kbID, EntityID: "markdown-shared", EntityType: "company", Alias: source.alias, NormalizedAlias: normalizeContextName(source.alias), EvidenceID: source.id, Quote: source.body}).Error)
		require.NoError(t, db.Create(&types.KnowledgeFact{ID: source.id, TenantID: 9, KnowledgeBaseID: source.kbID, SubjectEntityID: "markdown-shared", Kind: types.KnowledgeFactAttribute, Predicate: "staff", Value: types.JSON(`10`), ValueType: "number", EvidenceID: source.id, Quote: source.body, Status: types.KnowledgeFactCurrent}).Error)
	}
	keyCtx := types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 1, KnowledgeBaseIDs: types.StringArray{"energy-one"}, Capabilities: types.StringArray{"retrieve"}})
	visible, err := svc.Query(keyCtx, "", types.KnowledgeContextQuery{Scope: "team", Entity: "Acme Model"})
	require.NoError(t, err)
	require.Len(t, visible.Entities, 1)
	require.Equal(t, []string{"Acme Model"}, []string(visible.Entities[0].Aliases))
	require.Len(t, visible.Facts, 1)
	require.Equal(t, "energy-one", visible.Facts[0].KnowledgeBaseID)
	require.Len(t, visible.Evidence, 1)
	hidden, err := svc.Query(keyCtx, "", types.KnowledgeContextQuery{Scope: "team", Entity: "Secret Model"})
	require.NoError(t, err)
	require.Empty(t, hidden.Entities)
	require.Empty(t, hidden.Facts)
	require.False(t, contextAliasVisible("ACME staff", visible.Evidence), "Non-contiguous aliases must remain invisible")
}

func TestMarkdownCitationLongUnmatchedMarkerRuns(t *testing.T) {
	for _, marker := range []string{"*", "_", "~", "`"} {
		t.Run(marker, func(t *testing.T) {
			body := strings.Repeat(marker, 100000) + "Acme"
			require.Equal(t, body, normalizeMarkdownCitationSource(body, false).text)
		})
	}
	require.Equal(t, "*Acme", normalizeMarkdownCitationSource("****Acme***", false).text)
}

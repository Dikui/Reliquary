package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

type onDemandPromptRecord struct {
	Stage                string         `json:"stage"`
	SystemBytes          int            `json:"system_bytes"`
	UserBytes            int            `json:"user_bytes"`
	RequestBytes         int            `json:"request_bytes"`
	RequestTokenEstimate int            `json:"request_token_estimate"`
	OutputBytes          int            `json:"output_bytes"`
	OutputTokenEstimate  int            `json:"output_token_estimate"`
	ExpectedOutputKeys   []string       `json:"expected_output_keys"`
	ExpectedOutputItems  map[string]int `json:"expected_output_items"`
	System               string         `json:"-"`
	Body                 string         `json:"-"`
	Segment              int            `json:"segment"`
	TotalSegments        int            `json:"total_segments"`
	ReservedOutputTokens int            `json:"reserved_output_tokens"`
}

// Each response uses a partition of the same source-supported baseline fixture.
// Unlike the defensive-filter fixtures, this provider honors each round's schema.
func onDemandSplitReply(t *testing.T, stage, body string) (string, map[string]int) {
	t.Helper()
	var full contextExtraction
	require.NoError(t, json.Unmarshal([]byte(onDemandOutput), &full))
	if !strings.Contains(body, onDemandBody) {
		full.Entities, full.Facts, full.Events = nil, nil, nil
	}
	items := map[string]int{"objects": len(full.Entities), "template_facts": 0, "open_facts": 0, "events": len(full.Events)}
	templateFacts := []contextExtractedFact{}
	openFacts := []contextExtractedFact{}
	for _, fact := range full.Facts {
		// These are explicit fixture expectations, independent of the production
		// template-membership predicate used for the defensive boundary.
		if fact.Predicate == "licence" || fact.Predicate == "developed by" {
			templateFacts = append(templateFacts, fact)
		} else {
			openFacts = append(openFacts, fact)
		}
	}
	items["template_facts"], items["open_facts"] = len(templateFacts), len(openFacts)
	var response any
	switch stage {
	case "context_extraction":
		response = map[string]any{"entities": full.Entities, "facts": templateFacts}
		items["open_facts"], items["events"] = 0, 0
	case "context_full_extraction":
		response = map[string]any{"facts": openFacts, "events": full.Events}
		items["objects"], items["template_facts"] = 0, 0
	default:
		response = map[string]any{"entities": full.Entities, "facts": full.Facts, "events": full.Events}
	}
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	return string(encoded), items
}

func onDemandRecordingModel(t *testing.T, records *[]onDemandPromptRecord) *p3SegmentChat {
	t.Helper()
	return &p3SegmentChat{reply: func(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (string, error) {
		stage, _ := types.LLMCallMetadataFromContext(ctx)
		if stage == "context_event_judgment" {
			return `{"relation":"UNRELATED","confidence":1}`, nil
		}
		require.Contains(t, []string{"legacy_full_extraction", "context_extraction", "context_full_extraction"}, stage)
		require.Len(t, messages, 2)
		var input struct {
			Body          string `json:"body"`
			Segment       int    `json:"segment"`
			TotalSegments int    `json:"total_segments"`
		}
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &input))
		schema := onDemandPromptSchema(t, messages[0].Content)
		responseStage := stage
		if _, hasObjects := schema["entities"]; hasObjects {
			if _, hasEvents := schema["events"]; hasEvents {
				responseStage = "legacy_full_extraction"
			}
		}
		response, items := onDemandSplitReply(t, responseStage, input.Body)
		keys := make([]string, 0, len(schema))
		for key := range schema {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		*records = append(*records, onDemandPromptRecord{
			Stage: stage, System: messages[0].Content, Body: input.Body,
			SystemBytes: len(messages[0].Content), UserBytes: len(messages[1].Content),
			RequestBytes:         len(messages[0].Content) + len(messages[1].Content),
			RequestTokenEstimate: contextTokenEstimate(messages[0].Content) + contextTokenEstimate(messages[1].Content),
			OutputBytes:          len(response), OutputTokenEstimate: contextTokenEstimate(response),
			ExpectedOutputKeys: keys, ExpectedOutputItems: items,
			Segment: input.Segment, TotalSegments: input.TotalSegments, ReservedOutputTokens: options.MaxTokens,
		})
		return response, nil
	}}
}

func onDemandPromptSchema(t *testing.T, prompt string) map[string]json.RawMessage {
	t.Helper()
	marker := strings.Index(prompt, "Return strict JSON")
	require.GreaterOrEqual(t, marker, 0)
	start := strings.Index(prompt[marker:], "{")
	require.GreaterOrEqual(t, start, 0)
	var schema map[string]json.RawMessage
	require.NoError(t, json.NewDecoder(strings.NewReader(prompt[marker+start:])).Decode(&schema))
	return schema
}

func TestOnDemandLeanAndSupplementPromptSchemas(t *testing.T) {
	domain := BuiltinAIKnowledgeDomain()
	lean := contextExtractionPrompt(domain, types.KnowledgeExtractionTemplateOnly)
	schema := onDemandPromptSchema(t, lean)
	require.Len(t, schema, 2)
	require.Contains(t, schema, "entities")
	require.Contains(t, schema, "facts")
	require.NotContains(t, lean, `"events"`)
	require.NotContains(t, strings.ToLower(lean), "open fields")
	require.NotContains(t, lean, "Other supported fields may retain their source names.")
	var entities []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(schema["entities"], &entities))
	require.Len(t, entities, 1)
	for _, key := range []string{"mention", "canonical_name", "entity_type", "description", "quote"} {
		require.Contains(t, entities[0], key)
	}
	require.Contains(t, lean, "template field or relation key")

	supplement := contextSupplementExtractionPrompt(domain)
	schema = onDemandPromptSchema(t, supplement)
	require.Len(t, schema, 2)
	require.Contains(t, schema, "facts")
	require.Contains(t, schema, "events")
	require.NotContains(t, schema, "entities")
	require.NotContains(t, supplement, "keep extracting quoted entities")
	require.Contains(t, strings.ToLower(supplement), "exclude")
	require.Contains(t, supplement, "outside this template")
	require.Less(t, len(lean), len(contextExtractionPrompt(domain, types.KnowledgeExtractionFull)))
	fullOverhead := contextTokenEstimate(contextExtractionPrompt(domain, types.KnowledgeExtractionFull))
	require.LessOrEqual(t, contextTokenEstimate(lean), fullOverhead)
	require.LessOrEqual(t, contextTokenEstimate(supplement), fullOverhead)
}

func TestOnDemandOutsideUsesOneLeanRequest(t *testing.T) {
	db, ctx, svc, _ := onDemandFixture(t)
	var records []onDemandPromptRecord
	svc.models = knowledgeContextTestModelService{model: onDemandRecordingModel(t, &records)}
	k := p3Message(t, db, "lean-source", onDemandBody)
	require.NoError(t, svc.ProcessMessage(ctx, k, onDemandBody))
	require.Len(t, records, 1)
	require.Equal(t, "context_extraction", records[0].Stage)
	require.Equal(t, []string{"entities", "facts"}, records[0].ExpectedOutputKeys)
	require.Equal(t, map[string]int{"objects": 2, "template_facts": 2, "open_facts": 0, "events": 0}, records[0].ExpectedOutputItems)
	evidence := onDemandEvidence(t, db, k.ID)
	require.Equal(t, types.KnowledgeExtractionTemplateOnly, evidence.ExtractionMode)
	var facts []types.KnowledgeFact
	require.NoError(t, db.Where("evidence_id = ?", evidence.ID).Order("predicate").Find(&facts).Error)
	require.Len(t, facts, 2)
	require.Equal(t, "developed_by", facts[0].Predicate)
	require.Equal(t, "license", facts[1].Predicate)
	var links, events int64
	require.NoError(t, db.Model(&types.KnowledgeEvidenceEntity{}).Where("evidence_id = ?", evidence.ID).Count(&links).Error)
	require.NoError(t, db.Model(&types.KnowledgeEvent{}).Count(&events).Error)
	require.EqualValues(t, 2, links)
	require.Zero(t, events)
}

func TestOnDemandPrecheckFullPreservesOriginalObjectsAndFacts(t *testing.T) {
	db, ctx, svc, _ := onDemandFixture(t)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	k := p3Message(t, db, "split-full-source", onDemandBody)
	evidence, err := svc.SnapshotMessage(ctx, k, onDemandBody)
	require.NoError(t, err)
	domain, err := NewKnowledgeDomainService(db).GetDomainForKnowledgeBase(ctx, 1, k.KnowledgeBaseID)
	require.NoError(t, err)
	var baselineRecords []onDemandPromptRecord
	baselineModel := onDemandRecordingModel(t, &baselineRecords)
	baseline, _, err := svc.extractContextSegments(types.WithLLMCallMetadata(ctx, "legacy_full_extraction", ""), baselineModel, "p3-chat", evidence, domain)
	require.NoError(t, err)
	baselineEntities, baselineMentions, err := svc.resolveContextEntities(ctx, baselineModel, 1, k.KnowledgeBaseID, baseline.Entities, domain)
	require.NoError(t, err)
	expectedFacts, err := materializeContextFacts(baseline.Facts, baselineMentions, evidence, domain)
	require.NoError(t, err)
	expectedEvents, err := svc.resolveContextEvents(ctx, baselineModel, 1, k.KnowledgeBaseID, baseline.Events, baselineMentions)
	require.NoError(t, err)

	var records []onDemandPromptRecord
	svc.models = knowledgeContextTestModelService{model: onDemandRecordingModel(t, &records)}
	require.NoError(t, svc.ProcessMessage(ctx, k, onDemandBody))
	require.Len(t, records, 1)
	require.Equal(t, []string{"entities", "events", "facts"}, records[0].ExpectedOutputKeys)
	require.Equal(t, baselineRecords[0].Body, records[0].Body)
	stored := onDemandEvidence(t, db, k.ID)
	require.Equal(t, types.KnowledgeExtractionFull, stored.ExtractionMode)
	var actualFacts []types.KnowledgeFact
	require.NoError(t, db.Where("evidence_id = ?", stored.ID).Find(&actualFacts).Error)
	for i := range expectedFacts {
		expectedFacts[i].ID, expectedFacts[i].CreatedAt = "", time.Time{}
	}
	for i := range actualFacts {
		actualFacts[i].ID, actualFacts[i].CreatedAt = "", time.Time{}
	}
	require.ElementsMatch(t, expectedFacts, actualFacts)
	var actualEntities []types.KnowledgeEntity
	require.NoError(t, db.Joins("JOIN knowledge_context_evidence_entities link ON link.entity_id = knowledge_context_entities.id AND link.tenant_id = knowledge_context_entities.tenant_id").Where("link.evidence_id = ?", stored.ID).Find(&actualEntities).Error)
	for _, expected := range baselineEntities {
		var actual types.KnowledgeEntity
		for _, entity := range actualEntities {
			if entity.ID == expected.ID {
				actual = entity
			}
		}
		require.Equal(t, expected.CanonicalName, actual.CanonicalName)
		require.Equal(t, expected.EntityType, actual.EntityType)
		require.Equal(t, expected.Description, actual.Description)
	}
	require.Len(t, actualEntities, len(baselineEntities))
	var actualEvent types.KnowledgeEvent
	require.NoError(t, db.First(&actualEvent).Error)
	require.Len(t, expectedEvents, 1)
	expectedEvent := expectedEvents[0].Event
	require.Equal(t, expectedEvent.Title, actualEvent.Title)
	require.Equal(t, expectedEvent.EventType, actualEvent.EventType)
	require.Equal(t, expectedEvent.Description, actualEvent.Description)
	require.Equal(t, expectedEvent.OccurredAt, actualEvent.OccurredAt)
	var eventEntities []types.KnowledgeEventEntity
	require.NoError(t, db.Where("event_id = ?", actualEvent.ID).Find(&eventEntities).Error)
	require.Len(t, eventEntities, 1)
	require.Equal(t, expectedEvent.EntityIDs[0], eventEntities[0].EntityID)
	var eventEvidence types.KnowledgeEventEvidence
	require.NoError(t, db.Where("event_id = ?", actualEvent.ID).First(&eventEvidence).Error)
	require.Equal(t, expectedEvents[0].Relation, eventEvidence.Relation)
	require.Equal(t, "precheck_hit", stored.ExtractionPath)
	require.Equal(t, map[string]int{"context_extraction": 1, "context_event_judgment": 1}, onDemandRecordedCalls(t, stored))
}

func TestOnDemandFixedSegmentsMatchLegacyForBothFocusModes(t *testing.T) {
	for _, focused := range []bool{false, true} {
		t.Run(fmt.Sprintf("focused_%v", focused), func(t *testing.T) {
			db, ctx, svc, _ := onDemandFixture(t)
			if focused {
				require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
			}
			var body strings.Builder
			for i := 0; i < 12; i++ {
				fmt.Fprintf(&body, "# Chapter %d\n\n%s\n\n", i, strings.Repeat(onDemandBody+" ", 90))
			}
			k := p3Message(t, db, "fixed-segments", body.String())
			evidence, err := svc.SnapshotMessage(ctx, k, body.String())
			require.NoError(t, err)
			domain, err := NewKnowledgeDomainService(db).GetDomainForKnowledgeBase(ctx, 1, k.KnowledgeBaseID)
			require.NoError(t, err)
			var baselineRecords []onDemandPromptRecord
			baseline, legacyPlan, err := svc.extractContextSegments(types.WithLLMCallMetadata(ctx, "legacy_full_extraction", ""), onDemandRecordingModel(t, &baselineRecords), "p3-chat", evidence, domain)
			require.NoError(t, err)
			require.Greater(t, legacyPlan.Total, 1)
			require.NotEmpty(t, baseline.Entities)
			var records []onDemandPromptRecord
			svc.models = knowledgeContextTestModelService{model: onDemandRecordingModel(t, &records)}
			require.NoError(t, svc.ProcessMessage(ctx, k, body.String()))
			first, second := []onDemandPromptRecord{}, []onDemandPromptRecord{}
			for _, record := range records {
				if record.Stage == "context_extraction" {
					first = append(first, record)
				} else {
					second = append(second, record)
				}
			}
			require.Len(t, first, len(baselineRecords), "shorter first prompt cannot change the legacy full-window segment plan")
			for i, record := range first {
				require.Equal(t, baselineRecords[i].Body, record.Body)
				require.Equal(t, baselineRecords[i].Segment, record.Segment)
				require.Equal(t, baselineRecords[i].TotalSegments, record.TotalSegments)
				require.Equal(t, baselineRecords[i].ReservedOutputTokens, record.ReservedOutputTokens)
				require.LessOrEqual(t, record.RequestTokenEstimate, baselineRecords[i].RequestTokenEstimate)
			}
			if focused {
				for _, record := range first {
					require.Equal(t, []string{"entities", "events", "facts"}, record.ExpectedOutputKeys)
				}
			}
			require.Empty(t, second, "direct focus names use one extraction request for each segment")
			stored := onDemandEvidence(t, db, k.ID)
			require.Equal(t, legacyPlan.Total, stored.TotalSegments)
			require.Equal(t, legacyPlan.Processed, stored.ProcessedSegments)
		})
	}
}

func TestOnDemandSupplementReferencesVerifiedObjectsAndOwnSegmentQuotes(t *testing.T) {
	firstQuote := "Acme Inc has license MIT."
	secondQuote := "Acme Inc has 10 staff."
	segment := secondQuote + " Unknown has 5 staff."
	verified := []contextExtractedEntity{{Mention: "Acme Inc", Canonical: "Acme", EntityType: "company", Quote: firstQuote}}
	for _, test := range []struct {
		name    string
		subject string
		quote   string
		wantErr string
	}{
		{name: "first_object_reused_without_repeating_its_identifying_quote", subject: "Acme Inc", quote: secondQuote},
		{name: "provider_cannot_introduce_a_second_pass_object", subject: "Unknown", quote: "Unknown has 5 staff.", wantErr: "quoted subject"},
		{name: "supplement_quote_cannot_cross_segment_boundary", subject: "Acme Inc", quote: firstQuote, wantErr: "source excerpt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := &p3SegmentChat{reply: func(context.Context, []chat.Message, *chat.ChatOptions) (string, error) {
				response := map[string]any{
					"entities": []contextExtractedEntity{{Mention: "Unknown", Canonical: "Unknown", EntityType: "company", Quote: "Unknown has 5 staff."}},
					"facts": []contextExtractedFact{
						{Kind: "attribute", Subject: test.subject, Predicate: "staff", Value: json.RawMessage("10"), ValueType: "number", Quote: test.quote},
						{Kind: "attribute", Subject: "Acme Inc", Predicate: "licence", Value: json.RawMessage(`"second pass replacement"`), ValueType: "text", Quote: secondQuote},
					},
					"events": []contextExtractedEvent{},
				}
				encoded, err := json.Marshal(response)
				return string(encoded), err
			}}
			result, _, err := extractContextSupplementSegmentsWithPlan(context.Background(), model,
				&types.KnowledgeEvidence{Body: firstQuote + " " + segment}, BuiltinAIKnowledgeDomain(),
				contextSegmentPlan{Segments: []string{segment}, Total: 1, OutputLimit: 512, Coverage: "complete"}, verified)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				return
			}
			require.NoError(t, err)
			require.Empty(t, result.Entities)
			require.Len(t, result.Facts, 1, "provider repetition of template facts is discarded defensively")
			require.Equal(t, "staff", result.Facts[0].Predicate)
		})
	}
}

func TestOnDemandPromptCostComparison(t *testing.T) {
	db, ctx, svc, _ := onDemandFixture(t)
	k := p3Message(t, db, "cost-outside", onDemandBody)
	k.Title = "Cost comparison source"
	require.NoError(t, db.Model(k).Update("title", k.Title).Error)
	evidence, err := svc.SnapshotMessage(ctx, k, onDemandBody)
	require.NoError(t, err)
	domain, err := NewKnowledgeDomainService(db).GetDomainForKnowledgeBase(ctx, 1, k.KnowledgeBaseID)
	require.NoError(t, err)
	var baseline, outside, inside []onDemandPromptRecord
	baselineModel := onDemandRecordingModel(t, &baseline)
	full, _, err := svc.extractContextSegments(types.WithLLMCallMetadata(ctx, "legacy_full_extraction", ""), baselineModel, "p3-chat", evidence, domain)
	require.NoError(t, err)
	_, mentions, err := svc.resolveContextEntities(ctx, baselineModel, 1, k.KnowledgeBaseID, full.Entities, domain)
	require.NoError(t, err)
	_, err = svc.resolveContextEvents(ctx, baselineModel, 1, k.KnowledgeBaseID, full.Events, mentions)
	require.NoError(t, err)
	svc.models = knowledgeContextTestModelService{model: onDemandRecordingModel(t, &outside)}
	require.NoError(t, svc.ProcessMessage(ctx, k, onDemandBody))
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	focused := p3Message(t, db, "cost-focused", onDemandBody)
	focused.Title = k.Title
	require.NoError(t, db.Model(focused).Update("title", focused.Title).Error)
	svc.models = knowledgeContextTestModelService{model: onDemandRecordingModel(t, &inside)}
	require.NoError(t, svc.ProcessMessage(ctx, focused, onDemandBody))
	require.Len(t, baseline, 1)
	require.Len(t, outside, 1)
	require.Len(t, inside, 1)
	require.Less(t, outside[0].RequestBytes, baseline[0].RequestBytes)
	require.Less(t, outside[0].OutputBytes, baseline[0].OutputBytes)
	require.Equal(t, baseline[0].SystemBytes, inside[0].SystemBytes)
	require.Equal(t, baseline[0].OutputBytes, inside[0].OutputBytes)
	totalCalls := func(calls map[string]int) int {
		total := 0
		for _, count := range calls {
			total += count
		}
		return total
	}
	outsideCalls := onDemandRecordedCalls(t, onDemandEvidence(t, db, k.ID))
	insideCalls := onDemandRecordedCalls(t, onDemandEvidence(t, db, focused.ID))
	require.Equal(t, 2, totalCalls(baselineModel.calls))
	require.Equal(t, 1, totalCalls(outsideCalls))
	require.Equal(t, 2, totalCalls(insideCalls))
	comparison := map[string]any{
		"fixture_body":        onDemandBody,
		"measurement":         "UTF-8 byte counts of recorded system and user prompts and source-matched fixture responses; conservative token estimate uses the existing P3 estimator, not provider billing tokens",
		"attention_enabled":   false,
		"known_objects_exact": 2,
		"baseline":            map[string]any{"extraction_calls": len(baseline), "total_structured_calls": totalCalls(baselineModel.calls), "model_calls": baselineModel.calls, "rounds": baseline},
		"outside":             map[string]any{"extraction_calls": len(outside), "total_structured_calls": totalCalls(outsideCalls), "model_calls": outsideCalls, "rounds": outside},
		"inside":              map[string]any{"extraction_calls": len(inside), "total_structured_calls": totalCalls(insideCalls), "model_calls": insideCalls, "rounds": inside},
	}
	encoded, err := json.MarshalIndent(comparison, "", "  ")
	require.NoError(t, err)
	t.Log(string(encoded))
	if destination := os.Getenv("RELIQUARY_ISSUE07_PRECHECK_COST_PATH"); destination != "" {
		require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0755))
		require.NoError(t, os.WriteFile(destination, append(encoded, '\n'), 0644))
	}
}

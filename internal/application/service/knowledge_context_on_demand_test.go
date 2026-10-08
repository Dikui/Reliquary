package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

const onDemandBody = "Acme Inc has license MIT, 10 staff, and is developed by Maker. Acme Inc released a product on 2026-10-07."

const onDemandOutput = `{"entities":[{"mention":"Acme Inc","canonical_name":"Acme","entity_type":"company","quote":"Acme Inc has license MIT, 10 staff, and is developed by Maker."},{"mention":"Maker","canonical_name":"Maker","entity_type":"company","quote":"Acme Inc has license MIT, 10 staff, and is developed by Maker."}],"facts":[{"kind":"attribute","subject":"Acme Inc","predicate":"licence","source_predicate":"license","value":"MIT","value_type":"text","quote":"Acme Inc has license MIT, 10 staff, and is developed by Maker."},{"kind":"attribute","subject":"Acme Inc","predicate":"staff","value":10,"value_type":"number","quote":"Acme Inc has license MIT, 10 staff, and is developed by Maker."},{"kind":"relation","subject":"Acme Inc","predicate":"developed by","object":"Maker","value_type":"entity","quote":"Acme Inc has license MIT, 10 staff, and is developed by Maker."}],"events":[{"title":"Acme product release","event_type":"release","description":"Acme Inc released a product","occurred_at":"2026-10-07","entity_mentions":["Acme Inc"],"quote":"Acme Inc released a product on 2026-10-07."}]} `

func onDemandFixture(t *testing.T) (*gorm.DB, context.Context, *KnowledgeContextService, *p3SegmentChat) {
	t.Helper()
	t.Setenv("RELIQUARY_CONTEXT_ATTENTION_ENABLED", "false")
	db, ctx, svc := p3ContextFixture(t)
	entities := []types.KnowledgeEntity{p3Entity("acme", "Acme", "company"), p3Entity("maker", "Maker", "company")}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { _, err := persistContextEntities(tx, entities); return err }))
	require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: "acme-inc", TenantID: 1, EntityID: "acme", EntityType: "company", Alias: "Acme Inc", NormalizedAlias: "acme inc"}).Error)
	model := &p3SegmentChat{reply: func(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (string, error) {
		stage, _ := types.LLMCallMetadataFromContext(ctx)
		if stage == "context_event_judgment" {
			return `{"relation":"UNRELATED","confidence":1}`, nil
		}
		return onDemandOutput, nil
	}}
	svc.models = knowledgeContextTestModelService{model: model}
	return db, ctx, svc, model
}

func onDemandEvidence(t *testing.T, db *gorm.DB, knowledgeID string) types.KnowledgeEvidence {
	t.Helper()
	var evidence types.KnowledgeEvidence
	require.NoError(t, db.Where("knowledge_id = ?", knowledgeID).First(&evidence).Error)
	return evidence
}

func onDemandRecordedCalls(t *testing.T, evidence types.KnowledgeEvidence) map[string]int {
	t.Helper()
	var calls []types.ModelCallCount
	require.NoError(t, json.Unmarshal(evidence.ModelCalls, &calls))
	result := map[string]int{}
	for _, call := range calls {
		result[call.Stage] += call.Calls
	}
	return result
}

func TestOnDemandOutsideFocusOnlyTemplateFactsAndBudget(t *testing.T) {
	db, ctx, svc, model := onDemandFixture(t)
	k := p3Message(t, db, "outside", onDemandBody)
	require.NoError(t, svc.ProcessMessage(ctx, k, onDemandBody))
	evidence := onDemandEvidence(t, db, k.ID)
	require.Equal(t, types.KnowledgeExtractionTemplateOnly, evidence.ExtractionMode)
	require.Equal(t, "complete", evidence.ExtractionCoverage)
	require.Equal(t, 1, evidence.ProcessedSegments)
	require.Equal(t, 1, evidence.TotalSegments)
	var facts []types.KnowledgeFact
	require.NoError(t, db.Where("evidence_id = ?", evidence.ID).Order("predicate").Find(&facts).Error)
	require.Len(t, facts, 2)
	require.Equal(t, "developed_by", facts[0].Predicate, "template relation aliases remain eligible")
	require.Equal(t, "license", facts[1].Predicate, "template field aliases remain eligible")
	require.Equal(t, "license", facts[1].OriginalPredicate)
	var events int64
	require.NoError(t, db.Model(&types.KnowledgeEvent{}).Count(&events).Error)
	require.Zero(t, events, "even a provider returning events cannot cross the template-only boundary")
	var links []types.KnowledgeEvidenceEntity
	require.NoError(t, db.Where("evidence_id = ?", evidence.ID).Find(&links).Error)
	require.Len(t, links, 2)
	require.Equal(t, map[string]int{"context_extraction": 1}, onDemandRecordedCalls(t, evidence))
	require.Equal(t, 1, model.calls["context_extraction"])

	// The former complete pass uses the identical model output and one segment,
	// plus one event judgment. Current outside-focus calls are strictly fewer.
	domain, err := NewKnowledgeDomainService(db).GetDomainForKnowledgeBase(ctx, 1, k.KnowledgeBaseID)
	require.NoError(t, err)
	baseline, plan, err := svc.extractContextSegments(ctx, model, "p3-chat", &evidence, domain)
	require.NoError(t, err)
	require.Len(t, baseline.Facts, 3)
	require.Len(t, baseline.Events, 1)
	require.LessOrEqual(t, 1, plan.Processed+len(baseline.Events))
	t.Log("outside message: extraction=1, object_judgment=0, event_judgment=0, attention=0; previous complete extraction=2")
}

func TestOnDemandStarredObjectFullMatchesOriginalOutput(t *testing.T) {
	db, ctx, svc, _ := onDemandFixture(t)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	k := p3Message(t, db, "starred-object", onDemandBody)
	evidence, err := svc.SnapshotMessage(ctx, k, onDemandBody)
	require.NoError(t, err)
	domain, err := NewKnowledgeDomainService(db).GetDomainForKnowledgeBase(ctx, 1, k.KnowledgeBaseID)
	require.NoError(t, err)
	var kb types.KnowledgeBase
	require.NoError(t, db.First(&kb, "id = ?", k.KnowledgeBaseID).Error)
	baselineModel, err := GetTierChatModel(ctx, db, svc.models, &kb, types.ModelTierExtraction)
	require.NoError(t, err)
	baseline, _, err := svc.extractContextSegments(ctx, baselineModel, "p3-chat", evidence, domain)
	require.NoError(t, err)
	light, err := GetTierChatModel(ctx, db, svc.models, &kb, types.ModelTierLight)
	require.NoError(t, err)
	_, mentions, err := svc.resolveContextEntities(ctx, light, 1, k.KnowledgeBaseID, baseline.Entities, domain)
	require.NoError(t, err)
	expectedFacts, err := materializeContextFacts(baseline.Facts, mentions, evidence, domain)
	require.NoError(t, err)
	require.NoError(t, svc.ProcessMessage(ctx, k, onDemandBody))
	stored := onDemandEvidence(t, db, k.ID)
	require.Equal(t, types.KnowledgeExtractionFull, stored.ExtractionMode)
	var facts []types.KnowledgeFact
	require.NoError(t, db.Where("evidence_id = ?", stored.ID).Find(&facts).Error)
	for i := range expectedFacts {
		expectedFacts[i].ID = ""
		expectedFacts[i].CreatedAt = time.Time{}
	}
	for i := range facts {
		facts[i].ID = ""
		facts[i].CreatedAt = time.Time{}
	}
	require.ElementsMatch(t, expectedFacts, facts, "full mode must retain the former source assertions unchanged")
	var event types.KnowledgeEvent
	require.NoError(t, db.First(&event).Error)
	require.Equal(t, baseline.Events[0].Title, event.Title)
	require.Equal(t, baseline.Events[0].EventType, event.EventType)
	require.Equal(t, baseline.Events[0].Description, event.Description)
	require.Equal(t, verifiedContextTime(baseline.Events[0].OccurredAt, baseline.Events[0].Quote), event.OccurredAt)
	require.Equal(t, map[string]int{"context_extraction": 1, "context_full_extraction": 1, "context_event_judgment": 1}, onDemandRecordedCalls(t, stored))
	t.Log("starred object message: extraction=2, object_judgment=0, event_judgment=1, attention=0, total=3")
}

func TestOnDemandStarredTopicAndEnabledTrackingTriggerFull(t *testing.T) {
	for _, mode := range []string{"topic", "workflow_entity_id", "workflow_alias", "workflow_object_id", "disabled_workflow"} {
		t.Run(mode, func(t *testing.T) {
			db, ctx, svc, _ := onDemandFixture(t)
			k := p3Message(t, db, "source-"+mode, onDemandBody)
			if mode == "topic" {
				focusContextTestMessage(t, db, k)
			} else {
				cfg := map[string]any{"entity_id": "acme"}
				if mode == "workflow_alias" {
					cfg = map[string]any{"entity": "Acme Inc"}
				}
				if mode == "workflow_object_id" {
					cfg = map[string]any{"object_entity_id": "maker"}
				}
				workflow := types.KnowledgeWorkflow{ID: "tracking", TenantID: 1, KnowledgeBaseID: k.KnowledgeBaseID, OwnerID: "p3-reviewer",
					Name: mode, Enabled: mode != "disabled_workflow", Steps: []types.KnowledgeWorkflowStep{{ID: "facts", Type: types.WorkflowStepFactQuery, Config: cfg}}}
				require.NoError(t, db.Create(&workflow).Error)
			}
			require.NoError(t, svc.ProcessMessage(ctx, k, onDemandBody))
			evidence := onDemandEvidence(t, db, k.ID)
			var facts, events int64
			require.NoError(t, db.Model(&types.KnowledgeFact{}).Where("evidence_id = ?", evidence.ID).Count(&facts).Error)
			require.NoError(t, db.Model(&types.KnowledgeEvent{}).Count(&events).Error)
			if mode == "disabled_workflow" {
				require.Equal(t, types.KnowledgeExtractionTemplateOnly, evidence.ExtractionMode)
				require.EqualValues(t, 2, facts)
				require.Zero(t, events)
			} else {
				require.Equal(t, types.KnowledgeExtractionFull, evidence.ExtractionMode)
				require.EqualValues(t, 3, facts)
				require.EqualValues(t, 1, events)
			}
		})
	}
}

func TestOnDemandStarChangesNeverReextractOrDeleteHistory(t *testing.T) {
	db, ctx, svc, model := onDemandFixture(t)
	old := p3Message(t, db, "old-template-source", onDemandBody)
	require.NoError(t, svc.ProcessMessage(ctx, old, onDemandBody))
	before := onDemandEvidence(t, db, old.ID)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	require.NoError(t, svc.ProcessMessage(ctx, old, onDemandBody))
	require.Equal(t, before, onDemandEvidence(t, db, old.ID))
	require.Equal(t, 1, model.calls["context_extraction"])
	require.Zero(t, model.calls["context_full_extraction"])
	newMessage := p3Message(t, db, "new-full-source", onDemandBody)
	require.NoError(t, svc.ProcessMessage(ctx, newMessage, onDemandBody))
	fullBefore := onDemandEvidence(t, db, newMessage.ID)
	var factsBefore []types.KnowledgeFact
	var eventsBefore []types.KnowledgeEvent
	require.NoError(t, db.Order("id").Find(&factsBefore).Error)
	require.NoError(t, db.Order("id").Find(&eventsBefore).Error)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", false))
	var factsAfter []types.KnowledgeFact
	var eventsAfter []types.KnowledgeEvent
	require.NoError(t, db.Order("id").Find(&factsAfter).Error)
	require.NoError(t, db.Order("id").Find(&eventsAfter).Error)
	require.Equal(t, factsBefore, factsAfter)
	require.Equal(t, eventsBefore, eventsAfter)
	require.Equal(t, fullBefore, onDemandEvidence(t, db, newMessage.ID))
	require.Equal(t, types.KnowledgeExtractionTemplateOnly, onDemandEvidence(t, db, old.ID).ExtractionMode)
}

func TestOnDemandZeroTemplateFactsStillAssociateRecognizedObjects(t *testing.T) {
	db, ctx, svc, model := onDemandFixture(t)
	model.reply = func(context.Context, []chat.Message, *chat.ChatOptions) (string, error) {
		return `{"entities":[{"mention":"Acme Inc","canonical_name":"Acme","entity_type":"company","quote":"Acme Inc has 10 staff."}],"facts":[{"kind":"attribute","subject":"Acme Inc","predicate":"staff","value":10,"value_type":"number","quote":"Acme Inc has 10 staff."}],"events":[]}`, nil
	}
	k := p3Message(t, db, "object-without-facts", "Acme Inc has 10 staff.")
	require.NoError(t, svc.ProcessMessage(ctx, k, "Acme Inc has 10 staff."))
	evidence := onDemandEvidence(t, db, k.ID)
	var facts int64
	require.NoError(t, db.Model(&types.KnowledgeFact{}).Count(&facts).Error)
	require.Zero(t, facts)
	var links []types.KnowledgeEvidenceEntity
	require.NoError(t, db.Where("evidence_id = ?", evidence.ID).Find(&links).Error)
	require.Equal(t, []types.KnowledgeEvidenceEntity{{TenantID: 1, EvidenceID: evidence.ID, EntityID: "acme"}}, links)
	result, err := svc.Query(ctx, k.KnowledgeBaseID, types.KnowledgeContextQuery{EntityID: "acme"})
	require.NoError(t, err)
	require.Len(t, result.Entities, 1)
	require.Len(t, result.Evidence, 1)
	require.Empty(t, result.Facts)
	require.EqualValues(t, 1, result.TemplateOnlyMessageCount)
}

func TestOnDemandOutsideFocusKeepsSingleAttentionCall(t *testing.T) {
	db, ctx, svc, model := onDemandFixture(t)
	t.Setenv("RELIQUARY_CONTEXT_ATTENTION_ENABLED", "true")
	model.reply = func(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (string, error) {
		stage, _ := types.LLMCallMetadataFromContext(ctx)
		if stage == "context_attention" {
			return `{"attention":{"score":10,"reason":"Source-supported template facts."}}`, nil
		}
		return onDemandOutput, nil
	}
	k := p3Message(t, db, "outside-with-attention", onDemandBody)
	require.NoError(t, svc.ProcessMessage(ctx, k, onDemandBody))
	evidence := onDemandEvidence(t, db, k.ID)
	require.Equal(t, map[string]int{"context_extraction": 1, "context_attention": 1}, onDemandRecordedCalls(t, evidence))
	t.Log("outside message with default attention: extraction=1, attention=1, total=2; previous full pipeline total=3")
}

func TestOnDemandFailedFullPassRecordsModeAndCoverageSeparately(t *testing.T) {
	db, ctx, svc, model := onDemandFixture(t)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	model.reply = func(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (string, error) {
		stage, _ := types.LLMCallMetadataFromContext(ctx)
		if stage == "context_full_extraction" {
			return "", errors.New("full-pass provider failure")
		}
		return onDemandOutput, nil
	}
	k := p3Message(t, db, "failed-full", onDemandBody)
	require.ErrorContains(t, svc.ProcessMessage(ctx, k, onDemandBody), "full-pass provider failure")
	evidence := onDemandEvidence(t, db, k.ID)
	require.Equal(t, types.KnowledgeExtractionFull, evidence.ExtractionMode)
	require.Equal(t, types.KnowledgeExtractionFailed, evidence.ExtractionStatus)
	require.Equal(t, "partial", evidence.ExtractionCoverage)
	require.Zero(t, evidence.ProcessedSegments)
	require.Equal(t, 1, evidence.TotalSegments)
	require.Equal(t, map[string]int{"context_extraction": 1, "context_full_extraction": 1}, onDemandRecordedCalls(t, evidence))
}

func TestOnDemandCompletedFullEvidenceRejectsDelayedTemplateCoverage(t *testing.T) {
	db, ctx, svc, _ := onDemandFixture(t)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	k := p3Message(t, db, "concurrent-full-completed", onDemandBody)
	require.NoError(t, svc.ProcessMessage(ctx, k, onDemandBody))
	completed := onDemandEvidence(t, db, k.ID)
	require.Equal(t, types.KnowledgeExtractionFull, completed.ExtractionMode)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", false))
	// A second worker started before completion and returns later, after the
	// focus changes. Its coverage and failure writes must not downgrade data.
	err := svc.recordContextExtractionCoverage(ctx, &completed, types.KnowledgeExtractionTemplateOnly,
		contextSegmentPlan{Coverage: "partial", Processed: 0, Total: 3}, nil)
	require.NoError(t, err)
	require.ErrorContains(t, svc.failContextExtraction(ctx, 1, completed.ID, errors.New("delayed worker failed")), "delayed worker failed")
	require.Equal(t, completed, onDemandEvidence(t, db, k.ID))
}

func TestOnDemandFirstPassPreservesQuotedAliasesWithoutRepeatingObjects(t *testing.T) {
	db, ctx, svc, model := onDemandFixture(t)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	body := "Acme Inc is also A Company."
	model.reply = func(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (string, error) {
		stage, _ := types.LLMCallMetadataFromContext(ctx)
		extracted := contextExtraction{Entities: []contextExtractedEntity{{Mention: "Acme Inc", Canonical: "Acme", EntityType: "company", Quote: body}}}
		if stage == "context_full_extraction" {
			require.NotContains(t, onDemandPromptSchema(t, messages[0].Content), "entities")
			return `{"facts":[],"events":[]}`, nil
		}
		extracted.Entities[0].Aliases = []string{"A Company", "Unsupported Alias"}
		encoded, err := json.Marshal(extracted)
		return string(encoded), err
	}
	k := p3Message(t, db, "full-additional-alias", body)
	require.NoError(t, svc.ProcessMessage(ctx, k, body))
	var aliases []types.KnowledgeEntityAlias
	require.NoError(t, db.Where("tenant_id = ? AND entity_id = ? AND normalized_alias IN ?", 1, "acme", []string{"a company", "unsupported alias"}).Find(&aliases).Error)
	require.Len(t, aliases, 1)
	require.Equal(t, "A Company", aliases[0].Alias)
	require.Equal(t, "exact", aliases[0].ResolutionMethod)
	require.Equal(t, body, aliases[0].Quote)
	require.Equal(t, map[string]int{"context_extraction": 1, "context_full_extraction": 1}, onDemandRecordedCalls(t, onDemandEvidence(t, db, k.ID)))
}

func TestOnDemandDelayedDuplicateWorkerPreservesCompletedModeAndBudget(t *testing.T) {
	db, ctx, svc, model := onDemandFixture(t)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	k := p3Message(t, db, "duplicate-worker-source", onDemandBody)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var initialCalls atomic.Int32
	model.reply = func(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (string, error) {
		stage, _ := types.LLMCallMetadataFromContext(ctx)
		if stage == "context_extraction" && initialCalls.Add(1) == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		if stage == "context_event_judgment" {
			return `{"relation":"UNRELATED","confidence":1}`, nil
		}
		return onDemandOutput, nil
	}
	delayed := make(chan error, 1)
	go func() { delayed <- svc.ProcessMessage(ctx, k, onDemandBody) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("first worker must pause before another worker completes the evidence")
	}
	winningErr := svc.ProcessMessage(ctx, k, onDemandBody)
	if winningErr != nil {
		unblock()
		require.NoError(t, <-delayed)
		require.NoError(t, winningErr)
	}
	completed := onDemandEvidence(t, db, k.ID)
	require.Equal(t, types.KnowledgeExtractionFull, completed.ExtractionMode)
	require.Equal(t, "complete", completed.ExtractionCoverage)
	require.Equal(t, 1, completed.ProcessedSegments)
	require.Equal(t, 1, completed.TotalSegments)
	require.Equal(t, map[string]int{"context_extraction": 1, "context_full_extraction": 1, "context_event_judgment": 1}, onDemandRecordedCalls(t, completed))
	require.NoError(t, svc.SetEntityStar(ctx, "acme", false))
	unblock()
	require.NoError(t, <-delayed)
	require.Equal(t, completed, onDemandEvidence(t, db, k.ID), "the delayed template-only worker cannot replace completed coverage or the winning call counts")
}

func TestOnDemandCompletionRestoresWinningCoverageAfterFailedDuplicate(t *testing.T) {
	db, ctx, svc, model := onDemandFixture(t)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	k := p3Message(t, db, "winner-overwrites-failed-coverage", onDemandBody)
	model.reply = func(callCtx context.Context, _ []chat.Message, _ *chat.ChatOptions) (string, error) {
		stage, _ := types.LLMCallMetadataFromContext(callCtx)
		if stage == "context_event_judgment" {
			// The winning full pass has finished, but its final transaction has
			// not started. A failed duplicate returns now and records 0/3.
			evidence := onDemandEvidence(t, db, k.ID)
			failure := errors.New("concurrent template worker failed")
			require.NoError(t, svc.recordContextExtractionCoverage(callCtx, &evidence, types.KnowledgeExtractionTemplateOnly,
				contextSegmentPlan{Processed: 0, Total: 3}, failure))
			require.ErrorContains(t, svc.failContextExtraction(callCtx, 1, evidence.ID, failure), failure.Error())
			return `{"relation":"UNRELATED","confidence":1}`, nil
		}
		return onDemandOutput, nil
	}
	require.NoError(t, svc.ProcessMessage(ctx, k, onDemandBody))
	completed := onDemandEvidence(t, db, k.ID)
	require.Equal(t, types.KnowledgeExtractionComplete, completed.ExtractionStatus)
	require.Equal(t, types.KnowledgeExtractionFull, completed.ExtractionMode)
	require.Equal(t, "complete", completed.ExtractionCoverage)
	require.Equal(t, 1, completed.ProcessedSegments)
	require.Equal(t, 1, completed.TotalSegments)
	require.Empty(t, completed.ExtractionError)
	require.Equal(t, map[string]int{"context_extraction": 1, "context_full_extraction": 1, "context_event_judgment": 1}, onDemandRecordedCalls(t, completed))
}

package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/types"
)

func TestKnowledgeContextSelectionFieldsPruneEvidenceAndPreserveSource(t *testing.T) {
	db := knowledgeContextTestDB(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(9))
	for _, id := range []string{"model-a", "model-b"} {
		k := types.Knowledge{ID: id, TenantID: 9, KnowledgeBaseID: "kb", Title: id, ParseStatus: types.ParseStatusCompleted}
		require.NoError(t, k.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved"}))
		require.NoError(t, db.Create(&k).Error)
		require.NoError(t, db.Create(&types.KnowledgeEvidence{ID: id + "-evidence", TenantID: 9, KnowledgeBaseID: "kb", KnowledgeID: id, Version: 1, BodyHash: strings.Repeat("a", 64), Body: id + " context window 256 k tokens", Status: types.KnowledgeEvidenceCurrent}).Error)
		require.NoError(t, db.Create(&types.KnowledgeEntity{ID: id, TenantID: 9, KnowledgeBaseID: "kb", CanonicalName: id, NormalizedName: id}).Error)
	}
	require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: "alias", TenantID: 9, KnowledgeBaseID: "kb", EntityID: "model-a", Alias: "A2", NormalizedAlias: "a2"}).Error)
	evidence := &types.KnowledgeEvidence{ID: "model-a-evidence", TenantID: 9, KnowledgeBaseID: "kb"}
	facts, err := materializeContextFacts([]contextExtractedFact{{Kind: "attribute", Subject: "A2", Predicate: "context window", Value: json.RawMessage(`256`), ValueType: "number", Unit: "k tokens", Quote: "model-a context window 256 k tokens"}}, map[string]types.KnowledgeEntity{"a2": {ID: "model-a"}}, evidence)
	require.NoError(t, err)
	require.Equal(t, "context_length", facts[0].Predicate)
	require.Equal(t, "context window", facts[0].OriginalPredicate)
	require.Equal(t, "k tokens", facts[0].Unit)
	require.JSONEq(t, `256`, string(facts[0].Value))
	facts[0].EffectiveFrom = parseContextTime("2025-01-01")
	facts[0].EffectiveTo = parseContextTime("2027-01-01")
	require.NoError(t, db.Create(&facts).Error)
	maxLoaded := int64(0)
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("selection_evidence_rows", func(tx *gorm.DB) {
		if tx.Statement.Table == "knowledge_context_evidence" && tx.RowsAffected > maxLoaded {
			maxLoaded = tx.RowsAffected
		}
	}))
	threshold := 128000.0
	svc := NewKnowledgeContextService(db, nil)
	result, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{Entity: "A2", Predicate: "上下文长度", MinValue: &threshold, Unit: "tokens"})
	require.NoError(t, err)
	require.Len(t, result.Facts, 1)
	require.Len(t, result.Evidence, 1)
	require.EqualValues(t, 1, maxLoaded, "object filtering must happen before source bodies are loaded")
	require.Equal(t, facts[0].Quote, result.Facts[0].Quote)
	allModels, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{Predicate: "context window", MinValue: &threshold, Unit: "tokens"})
	require.NoError(t, err)
	require.Len(t, allModels.Facts, 1, "a field query must also work without naming an object")
	timeQuery := types.KnowledgeContextQuery{Predicate: "context_length", Since: parseContextTime("2026-01-01"), Until: parseContextTime("2026-02-01")}
	withinValidity, err := svc.Query(ctx, "kb", timeQuery)
	require.NoError(t, err)
	require.Len(t, withinValidity.Facts, 1, "a fact spanning the query window must remain visible")
	timeQuery.Since, timeQuery.Until = parseContextTime("2028-01-01"), nil
	expired, err := svc.Query(ctx, "kb", timeQuery)
	require.NoError(t, err)
	require.Empty(t, expired.Facts, "a fact ending before the query window must be excluded")
	wrongUnit, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{Entity: "A2", Predicate: "context_length", MinValue: &threshold, Unit: "bytes"})
	require.NoError(t, err)
	require.Empty(t, wrongUnit.Facts, "incompatible units cannot be compared")
	_, err = svc.Query(ctx, "kb", types.KnowledgeContextQuery{MinValue: &threshold, MaxValue: new(float64)})
	require.Error(t, err)
	otherTenant := context.WithValue(ctx, types.TenantIDContextKey, uint64(10))
	_, err = svc.Query(otherTenant, "kb", types.KnowledgeContextQuery{Entity: "A2"})
	require.Error(t, err)
}

func TestContextFactTimeRangeMissingDates(t *testing.T) {
	query := types.KnowledgeContextQuery{Since: parseContextTime("2026-01-01"), Until: parseContextTime("2026-02-01")}
	// A known endpoint defines an open interval, regardless of publication time.
	evidence := types.KnowledgeEvidence{PublishedAt: "2028-01-01"}
	require.True(t, contextFactInTimeRange(types.KnowledgeFact{EffectiveFrom: query.Until}, evidence, query), "start boundary is inclusive; missing end is unbounded")
	require.True(t, contextFactInTimeRange(types.KnowledgeFact{EffectiveTo: query.Since}, evidence, query), "end boundary is inclusive; missing start is unbounded")
	// Without validity dates, publication time is a point rather than an open interval.
	fact := types.KnowledgeFact{}
	require.False(t, contextFactInTimeRange(fact, evidence, query))
	evidence.PublishedAt = "2026-01-15"
	require.True(t, contextFactInTimeRange(fact, evidence, query))
	evidence.PublishedAt = "2025-01-01"
	require.False(t, contextFactInTimeRange(fact, evidence, query))
	evidence.PublishedAt = ""
	require.False(t, contextFactInTimeRange(fact, evidence, query), "undated facts do not match a time filter")
	require.True(t, contextFactInTimeRange(fact, evidence, types.KnowledgeContextQuery{}))
}

func TestWorkflowReportMaterialPreservesStructuredCorrectionsAndBackground(t *testing.T) {
	from, to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 7, 23, 59, 59, 0, time.UTC)
	result := types.KnowledgeContextResult{
		Entities: []types.KnowledgeEntity{{ID: "a2", CanonicalName: "Model A2"}},
		Facts:    []types.KnowledgeFact{{ID: "corrected", SubjectEntityID: "a2", Predicate: "context_length", Value: types.JSON(`128`), ValueType: "number", Unit: "k tokens", EvidenceID: "new", Quote: "128 k tokens under the stated conditions", Status: "current", SupersedesID: "old-fact", CorrectionNote: "Corrected the extraction unit"}, {ID: "out-of-period", EvidenceID: "future", Quote: "future material"}},
		Events:   []types.KnowledgeEvent{{ID: "release", Title: "A2 release", EntityIDs: types.StringArray{"a2"}, EvidenceIDs: types.StringArray{"new"}}},
	}
	// JSON round-trip reproduces the resumed-workflow Data contract.
	data, err := json.Marshal(result)
	require.NoError(t, err)
	var restored any
	require.NoError(t, json.Unmarshal(data, &restored))
	outputs := map[string]workflowStepOutput{"facts": {Data: restored, Sources: []types.KnowledgeWorkflowCitation{
		{ID: "evidence:new", EvidenceID: "new", Quote: "128 k tokens under the stated conditions", PublishedAt: "2026-10-02"},
		{ID: "evidence:old", EvidenceID: "old", Quote: "A1 used a shorter context window", PublishedAt: "2026-09-01"},
		{ID: "evidence:undated", EvidenceID: "undated", Quote: "An undated claim"},
		{ID: "evidence:future", EvidenceID: "future", Quote: "future material", PublishedAt: "2026-10-08"},
	}}}
	material, citations := buildWorkflowReportMaterial(outputs, &from, &to, time.UTC)
	require.Len(t, citations, 3)
	require.Equal(t, "current_period", material.Evidence[0].Role)
	require.Equal(t, "background", material.Evidence[1].Role)
	require.Equal(t, "unknown_date", material.Evidence[2].Role)
	require.Len(t, material.Structured, 1)
	require.Len(t, material.Structured[0].Facts, 1)
	require.Len(t, material.Structured[0].Events, 1)
	require.Equal(t, "C1", material.Structured[0].Facts[0]["citation_id"])
	require.Equal(t, "Corrected the extraction unit", material.Structured[0].Facts[0]["correction_note"])
	require.Equal(t, "old-fact", material.Structured[0].Facts[0]["supersedes_id"])
	require.Contains(t, material.Coverage, "Not an exhaustive census")
}

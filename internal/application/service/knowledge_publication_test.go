package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestPublicationMissingSnapshotAndWorkbenchCountBeforePagination(t *testing.T) {
	db := knowledgeContextTestDB(t)
	svc := NewKnowledgeContextService(db, nil)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(9))
	for _, id := range []string{"missing-one", "missing-two", "dated"} {
		k := types.Knowledge{ID: id, TenantID: 9, KnowledgeBaseID: "kb", Title: id, ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: time.Now(), UpdatedAt: time.Now()}
		source := types.SourceInfo{Publisher: "Publisher", OriginalURL: "https://example.com/" + id}
		if id == "dated" {
			source.PublishedAt = "2026-10-08"
		}
		require.NoError(t, k.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Decision: "approved", SourceInfo: source}))
		require.NoError(t, db.Create(&k).Error)
		snapshot, err := svc.snapshotFormalMessage(ctx, 9, &k, "body of "+id)
		require.NoError(t, err)
		require.Equal(t, id != "dated", snapshot.PublishedAtMissing)
		require.Equal(t, source.PublishedAt, snapshot.PublishedAt)
	}
	items, excluded, err := svc.ListEvidenceWithPublicationStats(ctx, "kb", types.KnowledgeContextQuery{Limit: 1})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, 2, excluded)
	result, err := svc.Query(ctx, "kb", types.KnowledgeContextQuery{Since: parseContextTime("2026-10-01"), Limit: 1})
	require.NoError(t, err)
	require.Equal(t, 2, result.ExcludedMissingPublishedAtCount)
	require.ElementsMatch(t, []string{"knowledge:missing-one", "knowledge:missing-two"}, result.PublishedAtMissingSourceIDs)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"excluded_missing_published_at_count":2`)
}

func TestReportPublicationExclusionsDeduplicateRestoredWorkflowSources(t *testing.T) {
	from := parseContextTime("2026-10-01")
	data := types.KnowledgeContextResult{PublishedAtMissingSourceIDs: []string{"knowledge:undated", "repost:missing"}}
	encoded, err := json.Marshal(data)
	require.NoError(t, err)
	var restored any
	require.NoError(t, json.Unmarshal(encoded, &restored))
	outputs := map[string]workflowStepOutput{
		"one": {Data: restored, Sources: []types.KnowledgeWorkflowCitation{{ID: "e:undated", KnowledgeID: "undated", Quote: "unknown"}, {ID: "e:dated", KnowledgeID: "dated", Quote: "known", PublishedAt: "2026-10-08"}}},
		"two": {Data: restored, Sources: []types.KnowledgeWorkflowCitation{{ID: "e:undated", KnowledgeID: "undated", Quote: "unknown"}}},
	}
	material, citations := buildWorkflowReportMaterial(outputs, from, nil, time.UTC)
	require.Len(t, citations, 1)
	require.Equal(t, 2, material.ExcludedMissingPublishedAtCount)
	encoded, err = json.Marshal(material)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"excluded_missing_published_at_count":2`)
}

func TestReportPublicationDateFormatsUseConsistentClassification(t *testing.T) {
	from := parseContextTime("2026-10-01")
	to := parseContextTime("2026-10-03")
	outputs := map[string]workflowStepOutput{"sources": {Sources: []types.KnowledgeWorkflowCitation{
		{ID: "flat", Quote: "dated", PublishedAt: "2026-10-02 12:00:00"},
		{ID: "invalid", Quote: "invalid", PublishedAt: "not-a-date"},
	}}}
	material, citations := buildWorkflowReportMaterial(outputs, from, to, time.UTC)
	require.Len(t, citations, 1)
	require.Equal(t, "current_period", citations[0].Role)
	require.Equal(t, 1, material.ExcludedMissingPublishedAtCount)
	material, citations = buildWorkflowReportMaterial(outputs, nil, nil, time.UTC)
	require.Len(t, citations, 2)
	require.Zero(t, material.ExcludedMissingPublishedAtCount)
}

func TestReportPublicationMissingOnlyPersistsStructuredCountWithoutModel(t *testing.T) {
	db := workflowTestDB(t)
	kb := &workflowTestKBService{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1, DomainID: "ai", CreatorID: "owner"}}
	require.NoError(t, db.Create(kb.kb).Error)
	svc := NewKnowledgeWorkflowService(db, kb, nil, nil, nil, nil, nil, nil)
	wf := &types.KnowledgeWorkflow{ID: "wf", TenantID: 1, KnowledgeBaseID: "kb", OwnerID: "owner"}
	run := &types.KnowledgeWorkflowRun{ID: "run", TenantID: 1, KnowledgeBaseID: "kb"}
	step := types.KnowledgeWorkflowStep{ID: "report", Config: map[string]any{"title": "日报", "time_from": "2026-10-01"}}
	outputs := map[string]workflowStepOutput{"research": {Sources: []types.KnowledgeWorkflowCitation{{ID: "unknown", KnowledgeID: "unknown", Quote: "unknown source"}}}}
	output, err := svc.executeReport(workflowTestContext("owner", 1), wf, run, step, outputs)
	require.NoError(t, err)
	require.Equal(t, 1, output.Data.(map[string]any)["excluded_missing_published_at_count"])
	var report types.KnowledgeWorkflowReport
	require.NoError(t, db.First(&report, "id = ?", output.ReportID).Error)
	require.Equal(t, 1, report.ExcludedMissingPublishedAtCount)
	require.Contains(t, report.Markdown, "因缺少发布时间被排除的来源数：1")
	retry, err := svc.executeReport(workflowTestContext("owner", 1), wf, run, step, nil)
	require.NoError(t, err)
	require.Equal(t, output.ReportID, retry.ReportID)
	require.Equal(t, output.Data, retry.Data)
}

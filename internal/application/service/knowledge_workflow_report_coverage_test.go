package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestTimelineCoverageUsesLatestEvidenceWithHistory(t *testing.T) {
	db, ctx, svc := p3ContextFixture(t)
	require.NoError(t, db.Create(&types.KnowledgeEntity{ID: "timeline-object", TenantID: 1, CanonicalName: "Timeline", NormalizedName: "timeline", EntityType: "company"}).Error)
	k := p3Message(t, db, "timeline-message", "Timeline launched.")
	for _, evidence := range []types.KnowledgeEvidence{
		{ID: "timeline-old", TenantID: 1, KnowledgeBaseID: k.KnowledgeBaseID, KnowledgeID: k.ID, Version: 1, BodyHash: strings.Repeat("a", 64), Body: "Timeline launched.", Status: types.KnowledgeEvidenceSuperseded, ExtractionMode: types.KnowledgeExtractionTemplateOnly, ExtractionStatus: types.KnowledgeExtractionComplete},
		{ID: "timeline-new", TenantID: 1, KnowledgeBaseID: k.KnowledgeBaseID, KnowledgeID: k.ID, Version: 2, BodyHash: strings.Repeat("b", 64), Body: "Timeline launched again.", Status: types.KnowledgeEvidenceCurrent, ExtractionMode: types.KnowledgeExtractionFull, ExtractionStatus: types.KnowledgeExtractionComplete},
	} {
		require.NoError(t, db.Create(&evidence).Error)
		require.NoError(t, db.Create(&types.KnowledgeEvidenceEntity{TenantID: 1, EvidenceID: evidence.ID, EntityID: "timeline-object"}).Error)
	}
	result, err := svc.EntityDetail(ctx, "timeline-object", true)
	require.NoError(t, err)
	require.Zero(t, result.TemplateOnlyMessageCount, "a historical gap is excluded when the latest version is full")
	require.Equal(t, contextEventsCoverageNotice, result.EventsCoverageNotice)
	require.NoError(t, db.Model(&types.KnowledgeEvidence{}).Where("id = ?", "timeline-new").Update("extraction_mode", types.KnowledgeExtractionTemplateOnly).Error)
	result, err = svc.EntityDetail(ctx, "timeline-object", true)
	require.NoError(t, err)
	require.Equal(t, 1, result.TemplateOnlyMessageCount)
}

func TestWorkflowReportMaterialPreservesCoverageAfterRestore(t *testing.T) {
	result := types.KnowledgeContextResult{Entities: []types.KnowledgeEntity{}, Facts: []types.KnowledgeFact{}, Events: []types.KnowledgeEvent{}, Evidence: []types.KnowledgeEvidence{}, TemplateOnlyMessageCount: 3}
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	var restored map[string]any
	require.NoError(t, json.Unmarshal(encoded, &restored))
	material, _ := buildWorkflowReportMaterial(map[string]workflowStepOutput{
		"object-query": {Data: result}, "overlapping-query": {Data: restored},
	}, nil, nil, time.UTC)
	require.Len(t, material.EventCoverage, 2)
	require.Equal(t, 3, material.EventCoverage[0].TemplateOnlyMessageCount)
	require.Equal(t, contextEventsCoverageNotice, material.EventsCoverageNotice)
	markdown := workflowReportCoverageMarkdown(material)
	require.Contains(t, markdown, "只包含收录时命中关注范围的消息")
	require.Contains(t, markdown, "template_only 的消息数：3")
	require.Contains(t, markdown, "计数不能相加")
	require.NotContains(t, markdown, "消息数：6")
}

func TestSearchReportCoverageCountsLatestCitedMessages(t *testing.T) {
	db, ctx, contextService := p3ContextFixture(t)
	k := p3Message(t, db, "report-coverage-message", "Source body")
	for _, evidence := range []types.KnowledgeEvidence{
		{ID: "report-coverage-old", TenantID: 1, KnowledgeBaseID: k.KnowledgeBaseID, KnowledgeID: k.ID, Version: 1, BodyHash: strings.Repeat("a", 64), Status: types.KnowledgeEvidenceSuperseded, ExtractionMode: types.KnowledgeExtractionTemplateOnly},
		{ID: "report-coverage-current", TenantID: 1, KnowledgeBaseID: k.KnowledgeBaseID, KnowledgeID: k.ID, Version: 2, BodyHash: strings.Repeat("b", 64), Status: types.KnowledgeEvidenceCurrent, ExtractionMode: types.KnowledgeExtractionTemplateOnly},
	} {
		require.NoError(t, db.Create(&evidence).Error)
	}
	svc := NewKnowledgeWorkflowService(db, nil, nil, contextService, nil, nil)
	citations := []types.KnowledgeWorkflowCitation{{KnowledgeID: k.ID}, {KnowledgeID: k.ID}}
	count, err := svc.reportSourceTemplateOnlyCount(ctx, citations)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	wf := &types.KnowledgeWorkflow{KnowledgeBaseID: k.KnowledgeBaseID, Steps: []types.KnowledgeWorkflowStep{{ID: "query", Type: types.WorkflowStepFactQuery, Config: map[string]any{"scope": "all"}}}}
	material := workflowReportMaterial{EventCoverage: []workflowReportEventCoverage{{StepID: "query", TemplateOnlyMessageCount: 99}}}
	require.NoError(t, svc.refreshReportEventCoverage(ctx, wf, &material))
	require.Equal(t, 1, material.EventCoverage[0].TemplateOnlyMessageCount)
	require.NoError(t, db.Model(&types.KnowledgeEvidence{}).Where("id = ?", "report-coverage-current").Update("extraction_mode", types.KnowledgeExtractionFull).Error)
	count, err = svc.reportSourceTemplateOnlyCount(ctx, citations)
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, svc.refreshReportEventCoverage(ctx, wf, &material))
	require.Zero(t, material.EventCoverage[0].TemplateOnlyMessageCount, "generation recounts current coverage rather than a restored query's stale gap")
}

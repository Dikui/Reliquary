package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

type segmentedChat struct {
	retainedProcessingChat
	calls             map[string]map[int]int
	fail              int
	verifyFail        int
	reject            int
	mergeCalls        int
	mergeFail         bool
	cancel            context.CancelFunc
	topic             string
	reviewFail        int
	auxiliaryFrom     int
	reviewMergeCalls  int
	reviewMergeFail   bool
	reviewMergeAccept bool
	reviewMergeInput  string
}

func (m *segmentedChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	if stage == "article_graph_verification" {
		var input struct {
			Source struct {
				Segment struct {
					Number int `json:"number"`
				} `json:"segment"`
			} `json:"source"`
		}
		if err := json.Unmarshal([]byte(messages[len(messages)-1].Content), &input); err != nil {
			return nil, err
		}
		if m.verifyFail > 0 && input.Source.Segment.Number == m.verifyFail {
			return nil, fmt.Errorf("verification failure %d", m.verifyFail)
		}
		return supportedGraphVerificationFixture(messages)
	}
	if stage == "article_summary_merge" {
		m.mergeCalls++
		if m.mergeFail {
			return nil, fmt.Errorf("merge failed")
		}
		return &types.ChatResponse{Content: "```json\n" + validProcessingFixture("event") + "\n```"}, nil
	}
	if stage != "intake_review" && stage != "article_processing" {
		return &types.ChatResponse{Content: `{"decision":"new","confidence":1}`}, nil
	}
	if stage == "intake_review" && strings.Contains(messages[0].Content, "你负责全文汇总审核") {
		m.reviewMergeCalls++
		m.reviewMergeInput = messages[len(messages)-1].Content
		if m.reviewMergeFail {
			return nil, fmt.Errorf("document review failed")
		}
		approved, reason := m.reject == 0 || m.reviewMergeAccept, "全文具有实质内容"
		if !approved {
			reason = "第 2/ 段提供全文不合格的证据"
		}
		if m.auxiliaryFrom == 1 {
			approved, reason = false, "全文缺少符合收录标准的实质正文"
		}
		return &types.ChatResponse{Content: fmt.Sprintf(`{"approved":%t,"reason":%q}`, approved, reason)}, nil
	}
	var input struct {
		Segment struct {
			Number int `json:"number"`
			Count  int `json:"count"`
		} `json:"segment"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(messages[len(messages)-1].Content), &input); err != nil {
		return nil, err
	}
	if stage != "intake_review" && stage != "article_processing" {
		return &types.ChatResponse{Content: `{"decision":"new","confidence":1}`}, nil
	}
	if m.calls == nil {
		m.calls = map[string]map[int]int{}
	}
	if m.calls[stage] == nil {
		m.calls[stage] = map[int]int{}
	}
	n := input.Segment.Number
	m.calls[stage][n]++
	if stage == "intake_review" {
		if n == m.reviewFail {
			return nil, fmt.Errorf("review failure %d", n)
		}
		auxiliary := m.auxiliaryFrom > 0 && n >= m.auxiliaryFrom
		return &types.ChatResponse{Content: reviewFixtureContent(fmt.Sprintf(`{"approved":%t,"auxiliary":%t,"reason":"fixture segment %d"}`, n != m.reject && !auxiliary, auxiliary, n), messages)}, nil
	}
	if n == m.fail {
		if m.cancel != nil {
			m.cancel()
		}
		return nil, fmt.Errorf("fixture failure %d", n)
	}
	kind := "event"
	if n == 1 {
		kind = "research"
	}
	result, _ := parseArticleProcessingResult(validProcessingFixture(kind))
	entityType := "company"
	result.Entities = []articleProcessingEntity{{Name: "Acme", Type: &entityType, Aliases: []string{fmt.Sprintf("Alias%d", n)}, Description: "Acme is the publisher.", Relevance: "central", EvidenceIDs: []int{1}}}
	if m.topic != "" {
		result.Topics = []string{m.topic, m.topic}
	}
	data, _ := json.Marshal(result)
	return &types.ChatResponse{Content: string(data)}, nil
}

func segmentedHarness(t *testing.T) (*articleProcessingHarness, *segmentedChat) {
	h := newArticleProcessingHarness(t, false)
	h.service.config.ArticleProcessing.MaxBodyChars = 140
	m := &segmentedChat{}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService.model = m
	return h, m
}
func segmentedEntry(t *testing.T, h *articleProcessingHarness) *types.Entry {
	k := &types.Entry{ID: "segment-entry", SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Title: "Segment test", Type: "manual", ParseStatus: types.ParseStatusProcessing, SourceKind: "article"}
	require.NoError(t, h.db.Create(k).Error)
	require.NoError(t, k.SetIntakeMetadata(&types.EntryIntakeMetadata{Decision: "pending", SourceInfo: types.SourceInfo{OriginalURL: "https://example.org/segments", Publisher: "Acme"}}))
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, k))
	return k
}

func TestSegmentProcessingRetryAndMerge(t *testing.T) {
	h, m := segmentedHarness(t)
	k := segmentedEntry(t, h)
	h.service.spanTracker = NewSpanTracker(repository.NewEntrySpanRepository(h.db), h.db)
	_, attempt, err := h.service.tracker().OpenAttempt(h.ctx, k.ID)
	require.NoError(t, err)
	h.ctx = withAttempt(h.ctx, attempt)
	topic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{Name: "Segments", Description: "fixture"})
	require.NoError(t, err)
	m.topic = topic.ID
	body := "# Main\n\n" + strings.Repeat("paragraph with meaningful original material. ", 13)
	m.verifyFail = 3
	_, err = h.service.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.ErrorContains(t, err, "第 3/")
	require.ErrorContains(t, err, "verification failure 3")
	var first entrySegmentRun
	require.NoError(t, h.db.Where("entry_id=?", k.ID).First(&first).Error)
	require.NotEqual(t, "completed", first.Status)
	var stored types.Entry
	require.NoError(t, h.db.First(&stored, "id=?", k.ID).Error)
	require.NotEqual(t, types.ParseStatusCompleted, stored.ParseStatus)
	var progress []types.EntryProcessingSpan
	require.NoError(t, h.db.Where("entry_id=? AND name LIKE ?", k.ID, "processing.segment.%").Find(&progress).Error)
	require.Len(t, progress, 3)
	for _, span := range progress {
		require.NotNil(t, span.Input["segment_number"])
		require.NotNil(t, span.Input["segment_count"])
	}
	// A fresh service value models the absence of any in-memory checkpoint.
	restarted := *h.service
	m.verifyFail = 0
	result, err := restarted.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.NoError(t, err)
	require.Equal(t, 1, m.calls["article_processing"][1])
	require.Equal(t, 1, m.calls["article_processing"][2])
	require.Equal(t, 2, m.calls["article_processing"][3])
	require.Equal(t, 1, m.mergeCalls)
	require.Len(t, result.Entities, 1)
	require.Equal(t, []string{topic.ID}, result.Topics)
	require.Equal(t, "event", *result.ContentType)
	require.Len(t, result.Provenance.Topics[topic.ID], len(m.calls["article_processing"]))
	require.Len(t, result.Provenance.Entities, len(result.Entities))
	_, err = restarted.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.NoError(t, err)
	require.Equal(t, 1, m.mergeCalls)
	_, err = restarted.callArticleProcessing(h.ctx, h.library, k, body+" changed", nil)
	require.NoError(t, err)
	require.Equal(t, 2, m.calls["article_processing"][1])
	require.Equal(t, 2, m.mergeCalls)
	h.service.config.ArticleProcessing.SegmentOverlapChars = 3
	_, err = restarted.callArticleProcessing(h.ctx, h.library, k, body+" changed", nil)
	require.NoError(t, err)
	require.Equal(t, 3, m.calls["article_processing"][1])
	require.Equal(t, 3, m.mergeCalls)
}

func TestSegmentCancellationAndMergeFailureResume(t *testing.T) {
	h, m := segmentedHarness(t)
	k := segmentedEntry(t, h)
	body := strings.Repeat("资料正文。", 100)
	ctx, cancel := context.WithCancel(h.ctx)
	m.fail = 2
	m.cancel = cancel
	_, err := h.service.callArticleProcessing(ctx, h.library, k, body, nil)
	require.Error(t, err)
	m.fail = 0
	m.cancel = nil
	m.mergeFail = true
	_, err = h.service.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.ErrorContains(t, err, "merge failed")
	before := fmt.Sprint(m.calls)
	m.mergeFail = false
	_, err = h.service.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.NoError(t, err)
	require.Equal(t, before, fmt.Sprint(m.calls))
	require.Equal(t, 2, m.mergeCalls)
}

func TestIntakeDoesNotCreateReviewSegmentsOrApplyModelLimits(t *testing.T) {
	h, m := segmentedHarness(t)
	k := segmentedEntry(t, h)
	h.service.config.ArticleProcessing.MaxSegments = 2
	m.reject, m.reviewFail = 2, 1
	rejected, err := h.service.reviewIntake(h.ctx, h.library, k, strings.Repeat("Original draft material. ", 100))
	require.NoError(t, err)
	require.False(t, rejected)
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", intake.Decision)
	require.Empty(t, m.calls)
	var count int64
	require.NoError(t, h.db.Model(&entrySegmentRun{}).Where("entry_id=? AND phase='review'", k.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestShortArticleStillOneCallAndTypeTie(t *testing.T) {
	h, m := segmentedHarness(t)
	k := segmentedEntry(t, h)
	_, err := h.service.callArticleProcessing(h.ctx, h.library, k, "Short complete body", nil)
	require.NoError(t, err)
	require.Equal(t, 1, m.calls["article_processing"][1])
	require.Zero(t, m.mergeCalls)
	one, _ := parseArticleProcessingResult(validProcessingFixture("research"))
	two, _ := parseArticleProcessingResult(validProcessingFixture("event"))
	result := mergeArticleSegments([]*articleProcessingResult{one, two})
	require.Equal(t, "research", *result.ContentType)
}

func TestSegmentPersistenceResolvesGlobalProvenance(t *testing.T) {
	h, m := segmentedHarness(t)
	topic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{Name: "Long original", Description: "Original fixture"})
	require.NoError(t, err)
	m.topic = topic.ID
	k := h.upload(t, "long-persisted", "# Original\n\n"+strings.Repeat("This original document describes Acme products and measurements. ", 15))
	require.Equal(t, types.ParseStatusCompleted, k.ParseStatus)
	var entities []types.EntryEntity
	require.NoError(t, h.db.Where("entry_id=?", k.ID).Find(&entities).Error)
	require.Len(t, entities, 1)
	var run entrySegmentRun
	require.NoError(t, h.db.Where("entry_id=? AND phase='processing'", k.ID).First(&run).Error)
	var result articleProcessingResult
	require.NoError(t, json.Unmarshal(run.Result, &result))
	require.Len(t, result.Provenance.ResolvedEntities[entities[0].EntityID], len(m.calls["article_processing"]))
	require.Len(t, result.Provenance.Topics[topic.ID], len(m.calls["article_processing"]))
}

func TestSegmentInlineReviewUsesTheFullEnvelopeBudget(t *testing.T) {
	h, _ := segmentedHarness(t)
	k := segmentedEntry(t, h)
	h.service.config.ArticleProcessing.MaxBodyChars = 0
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService.metadata.Parameters.ContextWindow = 16000
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	source := intake.SourceInfo
	source.Confirmed = false
	metadata := struct {
		SourceInfo types.SourceInfo `json:"source_info"`
	}{source}
	budget, err := h.service.segmentBudget(models.metadata, segmentReviewPrompt(h.library), metadata, 0)
	require.NoError(t, err)
	require.True(t, h.service.singleReviewSegment(h.ctx, h.library, k, strings.Repeat("字", budget)))
	require.False(t, h.service.singleReviewSegment(h.ctx, h.library, k, strings.Repeat("字", budget+1)))
}

func TestSegmentEvidenceNumberingFitsModelContext(t *testing.T) {
	h, m := segmentedHarness(t)
	k := segmentedEntry(t, h)
	h.service.config.ArticleProcessing.MaxBodyChars = 0
	models := h.service.modelService.(articleProcessingModels)
	models.metadata.Parameters.ContextWindow = 16000
	_, err := h.service.callArticleProcessing(h.ctx, h.library, k, strings.Repeat("- x\n", 1500), nil)
	require.NoError(t, err)
	require.Greater(t, len(m.calls["article_processing"]), 1)
	require.Equal(t, 1, m.mergeCalls)
}

func TestSegmentExplicitCompletedReparseStartsNewRun(t *testing.T) {
	h, m := segmentedHarness(t)
	article := h.upload(t, "explicit-rerun", "# Original\n\n"+strings.Repeat("Complete original material. ", 15))
	firstCalls := m.calls["article_processing"][1]
	_, err := h.service.ReparseEntry(h.ctx, article.ID, nil)
	require.NoError(t, err)
	h.drain(t)
	require.Equal(t, firstCalls+1, m.calls["article_processing"][1])
	var runs int64
	require.NoError(t, h.db.Model(&entrySegmentRun{}).Where("entry_id=? AND phase='processing'", article.ID).Count(&runs).Error)
	require.EqualValues(t, 2, runs)
}

func TestSegmentLongIngestCommitsBeforeProcessingAndResumes(t *testing.T) {
	h, _ := newIngestHarness(t)
	h.service.config.ArticleProcessing.MaxBodyChars = 140
	m := &segmentedChat{fail: 2}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService.model = m
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "personal", Title: "Queued review", Content: strings.Repeat("Original measured content. ", 30), Acquisition: "verbatim"})
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Status)
	require.Empty(t, m.calls)
	require.Len(t, h.queue.tasks, 1)
	require.ErrorContains(t, h.service.ProcessManualUpdate(h.ctx, h.queue.tasks[0]), "fixture failure 2")
	var run entrySegmentRun
	require.NoError(t, h.db.Where("entry_id=? AND phase='processing'", result.EntryID).First(&run).Error)
	var plan []segmentCheckpoint
	require.NoError(t, json.Unmarshal(run.Plan, &plan))
	require.Equal(t, "completed", plan[0].Status)
	m.fail = 0
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, h.queue.tasks[0]))
	require.Equal(t, 1, m.calls["article_processing"][1])
	require.Equal(t, 2, m.calls["article_processing"][2])
	require.Equal(t, types.ParseStatusCompleted, h.article(t, result.EntryID).ParseStatus)
}

func TestSegmentAsyncReviewRejectionCannotBlockStoredBody(t *testing.T) {
	h, _ := newIngestHarness(t)
	h.service.config.ArticleProcessing.MaxBodyChars = 140
	m := &segmentedChat{reject: 2}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService.model = m
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "personal", Title: "Rejected segment", Content: strings.Repeat("Review this complete original paragraph. ", 20), Acquisition: "verbatim"})
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Status)
	require.Empty(t, m.calls)
	h.drain(t)
	article := h.article(t, result.EntryID)
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
	require.NotEmpty(t, article.VaultPath)
	intake, err := article.IntakeMetadata()
	require.NoError(t, err)
	require.Empty(t, intake.Reason)
	require.Equal(t, "approved", intake.Decision)
	require.Empty(t, m.calls["intake_review"])
	require.NotEmpty(t, m.calls["article_processing"])
}

func TestSegmentUnverifiedFingerprintRepeatsOldWorkAndThenRetainsVerifiedCheckpoints(t *testing.T) {
	h, model := segmentedHarness(t)
	k := segmentedEntry(t, h)
	body := "# Original\n\n" + strings.Repeat("Complete original measurements. ", 30)
	topic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{Name: "Original topic"})
	require.NoError(t, err)
	model.fail = 4
	_, err = h.service.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.Error(t, err)
	input, prompt, chatModel, err := h.service.prepareArticleProcessing(h.ctx, h.library, k)
	require.NoError(t, err)
	budget, err := h.service.segmentBudget(chatModel, prompt, input, 0)
	require.NoError(t, err)
	limit, overlap, reserve := h.service.segmentSettings()
	plan, err := types.PlanProcessingSegments(body, budget, overlap, limit)
	require.NoError(t, err)
	legacy := segmentHash([]any{types.ProcessingSegmentVersion, plan, []any{input, prompt, segmentModelIdentity(chatModel), budget}, budget, limit, overlap, reserve, h.service.config.ArticleProcessing, segmentGeneration(k, "processing")})
	require.NoError(t, h.db.Model(&entrySegmentRun{}).Where("entry_id=? AND phase='processing'", k.ID).Update("plan_hash", legacy).Error)
	_, err = h.service.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.Error(t, err)
	require.Equal(t, 2, model.calls["article_processing"][1], "unverified checkpoints cannot bypass verification")
	var upgraded entrySegmentRun
	require.NoError(t, h.db.Where("entry_id=? AND phase='processing'", k.ID).Order("created_at DESC,id DESC").First(&upgraded).Error)
	require.NotEqual(t, legacy, upgraded.PlanHash)
	require.NoError(t, h.db.Delete(topic).Error)
	model.fail = 0
	_, err = h.service.callArticleProcessing(h.ctx, h.library, k, body, nil)
	require.NoError(t, err)
	require.Equal(t, 2, model.calls["article_processing"][1], "topic changes retain verified checkpoints")
	var runs int64
	require.NoError(t, h.db.Model(&entrySegmentRun{}).Where("entry_id=? AND phase='processing'", k.ID).Count(&runs).Error)
	require.EqualValues(t, 2, runs)
}

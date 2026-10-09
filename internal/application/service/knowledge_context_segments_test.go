package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

func TestP3ContextSegmentPlanUsesChaptersAndMarksPartial(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&body, "# Chapter %d\n\n%s\n\n", i+1, strings.Repeat("Acme has 10 staff. ", 60))
	}
	plan, err := planContextSegments(body.String(), "small prompt", 700, 128, 20)
	require.NoError(t, err)
	require.Greater(t, plan.Total, 1)
	require.Equal(t, "complete", plan.Coverage)
	for _, segment := range plan.Segments {
		require.True(t, strings.Contains(body.String(), segment))
		require.LessOrEqual(t, contextBodyTokenEstimate(segment)+contextTokenEstimate("small prompt")+256+128, 700)
	}
	limited, err := planContextSegments(body.String(), "small prompt", 700, 128, 2)
	require.NoError(t, err)
	require.Equal(t, "partial", limited.Coverage)
	require.Len(t, limited.Segments, 2)
	require.Equal(t, plan.Total, limited.Total)
	_, err = planContextSegments("body", strings.Repeat("large prompt ", 100), 128, 32, 2)
	require.ErrorContains(t, err, "cannot fit")
}

type p3SegmentChat struct {
	knowledgeContextChatInterface
	mu    sync.Mutex
	calls map[string]int
	reply func(context.Context, []chat.Message, *chat.ChatOptions) (string, error)
}

func (m *p3SegmentChat) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	m.mu.Lock()
	if m.calls == nil {
		m.calls = map[string]int{}
	}
	m.calls[stage]++
	m.mu.Unlock()
	content, err := m.reply(ctx, messages, opts)
	return &types.ChatResponse{Content: content}, err
}
func (*p3SegmentChat) GetModelName() string { return "segment-test" }
func (*p3SegmentChat) GetModelID() string   { return "summary" }

func p3SegmentFixture(t *testing.T) (*gorm.DB, context.Context, *KnowledgeContextService) {
	t.Helper()
	db, ctx, svc := p2ContextFixture(t)
	var domain types.KnowledgeDomain
	require.NoError(t, db.First(&domain, "id = ?", "energy").Error)
	domain.Fields = append(domain.Fields, types.DomainField{Key: "staff", Label: "Staff", ValueType: "number"})
	require.NoError(t, db.Save(&domain).Error)
	require.NoError(t, db.Exec(`ALTER TABLE knowledge_bases ADD COLUMN summary_model_id TEXT NOT NULL DEFAULT 'summary'`).Error)
	require.NoError(t, db.Create(&types.Model{ID: "summary", TenantID: 9, Name: "segment-test", Type: types.ModelTypeKnowledgeQA,
		Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{ContextWindow: 4096, MaxOutputTokens: 512}}).Error)
	return db, ctx, svc
}

func p3SegmentReply(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (string, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	switch stage {
	case "context_attention":
		return `{"attention":{"score":10,"reason":"A sourced staff count."}}`, nil
	case "context_entity_judgment":
		return `{"decision":"new","entity_id":"","confidence":0}`, nil
	case "context_event_judgment":
		return `{"relation":"UNRELATED","event_id":"","confidence":1}`, nil
	}
	var input struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(messages[1].Content), &input); err != nil {
		return "", err
	}
	if !strings.Contains(input.Body, "Acme has 10 staff.") {
		return `{"entities":[],"facts":[],"events":[]}`, nil
	}
	return `{"entities":[{"mention":"Acme","canonical_name":"Acme","entity_type":"company","quote":"Acme has 10 staff."}],"facts":[{"kind":"attribute","subject":"Acme","predicate":"staff","value":10,"value_type":"number","quote":"Acme has 10 staff."}],"events":[]}`, nil
}

func TestP3LongContextExtractionDeduplicatesAndRecordsCoverage(t *testing.T) {
	for _, limit := range []int{32, 2} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			t.Setenv("RELIQUARY_CONTEXT_MAX_SEGMENTS", fmt.Sprint(limit))
			db, ctx, svc := p3SegmentFixture(t)
			var body strings.Builder
			for i := 0; i < 8; i++ {
				fmt.Fprintf(&body, "# Chapter %d\n\n%s\n\n", i+1, strings.Repeat("Acme has 10 staff. ", 150))
			}
			message := p2ContextMessage(t, db, "long", "energy-one", body.String())
			model := &p3SegmentChat{reply: p3SegmentReply}
			svc.models = knowledgeContextTestModelService{model: model}
			require.NoError(t, svc.ProcessMessage(ctx, &message, body.String()))
			var evidence types.KnowledgeEvidence
			require.NoError(t, db.Where("knowledge_id = ?", message.ID).First(&evidence).Error)
			require.Equal(t, body.String(), evidence.Body)
			require.Greater(t, evidence.TotalSegments, 2)
			require.Equal(t, min(limit, evidence.TotalSegments), evidence.ProcessedSegments)
			require.Equal(t, evidence.ProcessedSegments, model.calls["context_extraction"])
			if limit == 2 {
				require.Equal(t, "partial", evidence.ExtractionCoverage)
			} else {
				require.Equal(t, "complete", evidence.ExtractionCoverage)
			}
			var facts []types.KnowledgeFact
			require.NoError(t, db.Find(&facts).Error)
			require.Len(t, facts, 1, "identical quoted facts from different chapters are stored once")
			var calls []types.ModelCallCount
			require.NoError(t, json.Unmarshal(evidence.ModelCalls, &calls))
			require.NotEmpty(t, calls)
			for _, count := range calls {
				if count.Stage == "context_extraction" {
					require.Equal(t, types.ModelTierExtraction, count.Tier)
					require.Equal(t, evidence.ProcessedSegments, count.Calls)
				} else {
					require.Equal(t, types.ModelTierLight, count.Tier)
				}
			}
		})
	}
}

func TestP3ContextQuoteMustBelongToItsSegmentAndFailedCallsAreVisible(t *testing.T) {
	db, ctx, svc := p3SegmentFixture(t)
	body := strings.Repeat("# Earlier chapter\n\nBackground text. ", 1000) + "\n# Last chapter\nAcme has 10 staff."
	message := p2ContextMessage(t, db, "bad-quote", "energy-one", body)
	model := &p3SegmentChat{reply: func(context.Context, []chat.Message, *chat.ChatOptions) (string, error) {
		return `{"entities":[{"mention":"Acme","canonical_name":"Acme","entity_type":"company","quote":"Acme has 10 staff."}],"facts":[],"events":[]}`, nil
	}}
	svc.models = knowledgeContextTestModelService{model: model}
	require.ErrorContains(t, svc.ProcessMessage(ctx, &message, body), "segment 1")
	var evidence types.KnowledgeEvidence
	require.NoError(t, db.Where("knowledge_id = ?", message.ID).First(&evidence).Error)
	require.Equal(t, types.KnowledgeExtractionFailed, evidence.ExtractionStatus)
	var recorded []types.ModelCallCount
	require.NoError(t, json.Unmarshal(evidence.ModelCalls, &recorded))
	require.Len(t, recorded, 1)
	require.Equal(t, 1, recorded[0].Calls)
	var objects int64
	require.NoError(t, db.Model(&types.KnowledgeEntity{}).Count(&objects).Error)
	require.Zero(t, objects)
}

func TestP3FailedSecondSegmentRetainsProcessedCoverageAndCallBudget(t *testing.T) {
	db, ctx, svc := p3SegmentFixture(t)
	body := strings.Repeat("# Chapter\n\nAcme has 10 staff. ", 1000)
	message := p2ContextMessage(t, db, "second-segment-failed", "energy-one", body)
	model := &p3SegmentChat{}
	model.reply = func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (string, error) {
		if model.calls["context_extraction"] == 2 {
			return "", fmt.Errorf("scripted provider failure")
		}
		return p3SegmentReply(ctx, messages, opts)
	}
	svc.models = knowledgeContextTestModelService{model: model}
	require.ErrorContains(t, svc.ProcessMessage(ctx, &message, body), "scripted provider failure")
	var evidence types.KnowledgeEvidence
	require.NoError(t, db.Where("knowledge_id = ?", message.ID).First(&evidence).Error)
	require.Equal(t, "partial", evidence.ExtractionCoverage)
	require.Equal(t, 1, evidence.ProcessedSegments)
	require.Greater(t, evidence.TotalSegments, 1)
	var recorded []types.ModelCallCount
	require.NoError(t, json.Unmarshal(evidence.ModelCalls, &recorded))
	require.Len(t, recorded, 1)
	require.Equal(t, 2, recorded[0].Calls)
}

func TestP3SourceAboveFormerByteLimitIsPartiallyExtracted(t *testing.T) {
	t.Setenv("RELIQUARY_CONTEXT_MAX_SEGMENTS", "2")
	db, ctx, svc := p3SegmentFixture(t)
	body := "# Large source\n\n" + strings.Repeat("Acme has 10 staff. ", 45000)
	require.Greater(t, len(body), 800000)
	message := p2ContextMessage(t, db, "above-former-cap", "energy-one", body)
	svc.models = knowledgeContextTestModelService{model: &p3SegmentChat{reply: p3SegmentReply}}
	require.NoError(t, svc.ProcessMessage(ctx, &message, body))
	var evidence types.KnowledgeEvidence
	require.NoError(t, db.Where("knowledge_id = ?", message.ID).First(&evidence).Error)
	require.Equal(t, body, evidence.Body)
	require.Equal(t, "partial", evidence.ExtractionCoverage)
	require.Equal(t, 2, evidence.ProcessedSegments)
	require.Greater(t, evidence.TotalSegments, 2)
}

func TestP3ConcurrentMessagesCreateOneObjectWithoutOrphanReferences(t *testing.T) {
	db, ctx, svc := p3SegmentFixture(t)
	body := "Acme has 10 staff."
	left := p2ContextMessage(t, db, "left", "energy-one", body)
	right := p2ContextMessage(t, db, "right", "energy-two", body)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	model := &p3SegmentChat{reply: func(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (string, error) {
		stage, _ := types.LLMCallMetadataFromContext(ctx)
		if stage == "context_entity_judgment" {
			arrived <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return p3SegmentReply(ctx, messages, options)
	}}
	svc.models = knowledgeContextTestModelService{model: model}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	errors := make(chan error, 2)
	for _, message := range []*types.Knowledge{&left, &right} {
		go func(k *types.Knowledge) { errors <- svc.ProcessMessage(ctx, k, body) }(message)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("both messages must reach model resolution concurrently")
		}
	}
	// Both provider calls are paused. An exclusive maintenance lock is still
	// immediately obtainable, proving model latency does not hold context locks.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		var acquired bool
		if err := tx.Raw("SELECT pg_try_advisory_xact_lock(hashtextextended(?, 0))", "reliquary:knowledge-context:team:9").Scan(&acquired).Error; err != nil {
			return err
		}
		require.True(t, acquired)
		return nil
	}))
	close(release)
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)
	var objects []types.KnowledgeEntity
	var aliases []types.KnowledgeEntityAlias
	var facts []types.KnowledgeFact
	require.NoError(t, db.Find(&objects).Error)
	require.NoError(t, db.Find(&aliases).Error)
	require.NoError(t, db.Find(&facts).Error)
	require.Len(t, objects, 1)
	require.Len(t, aliases, 1)
	require.Len(t, facts, 2)
	for _, fact := range facts {
		require.Equal(t, objects[0].ID, fact.SubjectEntityID)
	}
	var orphans int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM knowledge_context_entity_aliases a LEFT JOIN knowledge_context_entities e ON e.id=a.entity_id AND e.tenant_id=a.tenant_id WHERE e.id IS NULL`).Scan(&orphans).Error)
	require.Zero(t, orphans)
}

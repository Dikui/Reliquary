package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/middleware/asynqdl"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
)

type contextBackfillQueue struct {
	tasks  []*asynq.Task
	queues []string
	fail   bool
}

func (q *contextBackfillQueue) Enqueue(task *asynq.Task, options ...asynq.Option) (*asynq.TaskInfo, error) {
	if q.fail {
		return nil, errors.New("queue unavailable")
	}
	q.tasks = append(q.tasks, task)
	for _, option := range options {
		if option.Type() == asynq.QueueOpt {
			q.queues = append(q.queues, option.Value().(string))
		}
	}
	return &asynq.TaskInfo{}, nil
}

func backfillFixture(t *testing.T) (*gorm.DB, context.Context, *KnowledgeContextService, *KnowledgeContextBackfillService, *contextBackfillQueue, *p3SegmentChat) {
	t.Helper()
	db, ctx, contextService, model := onDemandFixture(t)
	queue := &contextBackfillQueue{}
	backfill := NewKnowledgeContextBackfillService(db, contextService, queue)
	return db, ctx, contextService, backfill, queue, model
}

func TestContextBackfillOnlySupplementPreservesTemplateCorrectionsAndAliases(t *testing.T) {
	db, ctx, svc, backfill, queue, model := backfillFixture(t)
	message := p3Message(t, db, "backfill-source", onDemandBody)
	require.NoError(t, svc.ProcessMessage(ctx, message, onDemandBody))
	evidence := onDemandEvidence(t, db, message.ID)
	var original types.KnowledgeFact
	require.NoError(t, db.Where("evidence_id=? AND predicate='license'", evidence.ID).First(&original).Error)
	_, err := svc.CorrectFact(ctx, "", original.ID, types.KnowledgeFactCorrection{Value: types.JSON(`"Apache"`), ValueType: "text", Quote: original.Quote, CorrectionNote: "Reviewed source label"})
	require.NoError(t, err)
	var before []types.KnowledgeFact
	require.NoError(t, db.Where("evidence_id=?", evidence.ID).Order("id").Find(&before).Error)
	var aliasesBefore []types.KnowledgeEntityAlias
	require.NoError(t, db.Order("id").Find(&aliasesBefore).Error)
	require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: "historical-alias", TenantID: 1, EntityID: "acme", EntityType: "company", Alias: "ACME International", NormalizedAlias: "acme international"}).Error)
	require.NoError(t, db.Order("id").Find(&aliasesBefore).Error)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	estimate, err := backfill.Estimate(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
	require.NoError(t, err)
	require.Equal(t, 1, estimate.TemplateOnlyMessageCount)
	require.Equal(t, 1, estimate.PendingMessageCount)
	require.Equal(t, 2, estimate.EstimatedModelCalls)
	model.reply = func(callCtx context.Context, messages []chat.Message, _ *chat.ChatOptions) (string, error) {
		stage, _ := types.LLMCallMetadataFromContext(callCtx)
		if stage == "context_event_judgment" {
			return `{"relation":"UNRELATED","confidence":1}`, nil
		}
		require.Equal(t, "context_backfill_extraction", stage)
		require.Contains(t, messages[0].Content, "Object recognition and template facts are already complete")
		var input struct {
			Entities []contextExtractedEntity `json:"verified_entities"`
		}
		require.NoError(t, json.Unmarshal([]byte(messages[1].Content), &input))
		require.Len(t, input.Entities, 2)
		require.Equal(t, "Acme", input.Entities[0].Canonical)
		require.Contains(t, input.Entities[0].Aliases, "Acme Inc")
		require.Contains(t, input.Entities[0].Aliases, "ACME International")
		return onDemandOutput, nil
	}
	batch, err := backfill.Start(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
	require.NoError(t, err)
	require.Equal(t, 1, batch.MessageCount)
	require.Equal(t, []string{types.QueueMaintenance}, queue.queues)
	require.Equal(t, types.TypeKnowledgeContextBackfill, queue.tasks[0].Type())
	require.NoError(t, backfill.Handle(ctx, queue.tasks[0]))
	require.NoError(t, backfill.Handle(ctx, queue.tasks[0]), "repeated task delivery is idempotent")
	stored := onDemandEvidence(t, db, message.ID)
	require.Equal(t, types.KnowledgeExtractionFull, stored.ExtractionMode)
	require.NotNil(t, stored.BackfilledAt)
	require.NotNil(t, stored.BackfillTemplateUpdatedAt)
	var domain types.KnowledgeDomain
	require.NoError(t, db.First(&domain, "id='p3-domain'").Error)
	require.WithinDuration(t, domain.UpdatedAt, *stored.BackfillTemplateUpdatedAt, time.Microsecond)
	var after []types.KnowledgeFact
	require.NoError(t, db.Where("id IN ?", contextBackfillFactIDs(before)).Order("id").Find(&after).Error)
	require.Equal(t, before, after)
	var aliasesAfter []types.KnowledgeEntityAlias
	require.NoError(t, db.Order("id").Find(&aliasesAfter).Error)
	require.Equal(t, aliasesBefore, aliasesAfter)
	var openFacts, events int64
	require.NoError(t, db.Model(&types.KnowledgeFact{}).Where("evidence_id=? AND predicate='staff'", evidence.ID).Count(&openFacts).Error)
	require.EqualValues(t, 1, openFacts)
	require.NoError(t, db.Model(&types.KnowledgeEventEvidence{}).Where("evidence_id=?", evidence.ID).Count(&events).Error)
	require.EqualValues(t, 1, events)
	batch, err = backfill.Batch(ctx, batch.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", batch.Status)
	require.Equal(t, 1, batch.SuccessCount)
	require.Zero(t, batch.FailureCount)
	require.Equal(t, 1, model.calls["context_backfill_extraction"])
	second, err := backfill.Start(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
	require.NoError(t, err)
	require.Zero(t, second.MessageCount, "already full evidence is not selected")
	list, err := backfill.Batches(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
	require.NoError(t, err)
	require.Len(t, list, 2)
}

func contextBackfillFactIDs(facts []types.KnowledgeFact) []string {
	ids := make([]string, len(facts))
	for i := range facts {
		ids[i] = facts[i].ID
	}
	return ids
}

func TestContextBackfillFailuresAreAtomicAndDeadLettered(t *testing.T) {
	for _, failure := range []string{"model", "persistence", "processing", "processing_during_model"} {
		t.Run(failure, func(t *testing.T) {
			db, ctx, svc, backfill, queue, model := backfillFixture(t)
			message := p3Message(t, db, "failed-backfill", onDemandBody)
			require.NoError(t, svc.ProcessMessage(ctx, message, onDemandBody))
			require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
			evidence := onDemandEvidence(t, db, message.ID)
			var before []types.KnowledgeFact
			require.NoError(t, db.Order("id").Find(&before).Error)
			batch, err := backfill.Start(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
			require.NoError(t, err)
			if failure == "model" {
				model.reply = func(context.Context, []chat.Message, *chat.ChatOptions) (string, error) {
					return "", errors.New("model unavailable")
				}
			} else if failure == "persistence" {
				require.NoError(t, db.Exec(`ALTER TABLE knowledge_context_event_evidence ADD CONSTRAINT reject_backfill CHECK (source_title <> 'Acme product release')`).Error)
			} else if failure == "processing" {
				require.NoError(t, updateKnowledgeContextState(ctx, db, message, "processing", ""))
			} else {
				model.reply = func(callCtx context.Context, _ []chat.Message, _ *chat.ChatOptions) (string, error) {
					stage, _ := types.LLMCallMetadataFromContext(callCtx)
					if stage == "context_event_judgment" {
						return `{"relation":"UNRELATED","confidence":1}`, nil
					}
					require.NoError(t, updateKnowledgeContextState(ctx, db, message, "processing", ""))
					return onDemandOutput, nil
				}
			}
			err = asynqdl.Middleware(repository.NewTaskDeadLetterRepository(db))(asynq.HandlerFunc(backfill.Handle)).ProcessTask(ctx, queue.tasks[0])
			if strings.HasPrefix(failure, "processing") {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			stored := onDemandEvidence(t, db, message.ID)
			require.Equal(t, evidence, stored, "failure leaves the immutable snapshot and extraction metadata unchanged")
			var after []types.KnowledgeFact
			require.NoError(t, db.Order("id").Find(&after).Error)
			require.Equal(t, before, after)
			var count int64
			require.NoError(t, db.Model(&types.KnowledgeEvent{}).Count(&count).Error)
			require.Zero(t, count, "even facts written before a failed event attachment roll back")
			batch, err = backfill.Batch(ctx, batch.ID)
			require.NoError(t, err)
			require.Equal(t, "failed", batch.Status)
			require.Equal(t, 1, batch.FailureCount)
			require.Zero(t, batch.SuccessCount)
			require.NotEmpty(t, batch.Items[0].Error)
			if !strings.HasPrefix(failure, "processing") {
				var dead types.TaskDeadLetter
				require.NoError(t, db.Where("task_type=?", types.TypeKnowledgeContextBackfill).First(&dead).Error)
				require.Equal(t, message.ID, dead.RelatedID)
			} else if failure == "processing" {
				require.Zero(t, model.calls["context_backfill_extraction"])
			}
		})
	}
}

func TestContextBackfillDeadLetterRequeueRecoversBatch(t *testing.T) {
	db, ctx, svc, backfill, queue, model := backfillFixture(t)
	message := p3Message(t, db, "retry-backfill", onDemandBody)
	require.NoError(t, svc.ProcessMessage(ctx, message, onDemandBody))
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	batch, err := backfill.Start(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
	require.NoError(t, err)
	originalReply := model.reply
	model.reply = func(context.Context, []chat.Message, *chat.ChatOptions) (string, error) {
		return "", errors.New("failed attempt")
	}
	require.Error(t, backfill.Handle(ctx, queue.tasks[0]))
	batch, err = backfill.Batch(ctx, batch.ID)
	require.NoError(t, err)
	require.Equal(t, 1, batch.FailureCount)
	model.reply = originalReply
	require.NoError(t, backfill.Handle(ctx, queue.tasks[0]))
	batch, err = backfill.Batch(ctx, batch.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", batch.Status)
	require.Equal(t, 1, batch.SuccessCount)
	require.Zero(t, batch.FailureCount)
	require.NoError(t, backfill.Handle(ctx, queue.tasks[0]))
	require.Equal(t, 2, model.calls["context_backfill_extraction"])
}

func TestContextBackfillSelectsLatest200RelatedCompletedTemplateOnly(t *testing.T) {
	db, ctx, svc, backfill, queue, _ := backfillFixture(t)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	var evidence []types.KnowledgeEvidence
	for i := 0; i < 205; i++ {
		message := p3Message(t, db, fmt.Sprintf("history-%03d", i), onDemandBody)
		published := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		item := types.KnowledgeEvidence{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: message.KnowledgeBaseID, KnowledgeID: message.ID, Version: 1, BodyHash: strings.Repeat("a", 64), Body: onDemandBody, PublishedAt: published, Status: types.KnowledgeEvidenceCurrent, ExtractionStatus: types.KnowledgeExtractionComplete, ExtractionMode: types.KnowledgeExtractionTemplateOnly, CreatedAt: time.Now()}
		evidence = append(evidence, item)
	}
	require.NoError(t, db.Create(&evidence).Error)
	for i, item := range evidence {
		entityID := "acme"
		if i == 204 {
			entityID = "maker"
		}
		require.NoError(t, db.Create(&types.KnowledgeEvidenceEntity{TenantID: 1, EvidenceID: item.ID, EntityID: entityID}).Error)
	}
	require.NoError(t, db.Model(&evidence[203]).Update("extraction_mode", types.KnowledgeExtractionFull).Error)
	require.NoError(t, db.Model(&evidence[202]).Update("extraction_status", types.KnowledgeExtractionPending).Error)
	estimate, err := backfill.Estimate(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
	require.NoError(t, err)
	require.Equal(t, 203, estimate.TemplateOnlyMessageCount)
	require.Equal(t, 202, estimate.PendingMessageCount)
	require.Equal(t, 200, estimate.BatchMessageCount)
	require.Equal(t, 2, estimate.RemainingMessageCount)
	batch, err := backfill.Start(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
	require.NoError(t, err)
	require.Equal(t, 200, batch.MessageCount)
	require.Len(t, queue.tasks, 200)
	selected := map[string]bool{}
	for _, item := range batch.Items {
		selected[item.KnowledgeID] = true
	}
	require.False(t, selected["history-000"])
	require.False(t, selected["history-001"])
	require.True(t, selected["history-002"])
	require.True(t, selected["history-201"])
	require.False(t, selected["history-202"])
	require.False(t, selected["history-203"])
	require.False(t, selected["history-204"])
	second, err := backfill.Start(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
	require.NoError(t, err)
	require.Equal(t, 2, second.MessageCount, "an overlapping batch excludes all pending messages")
}

func TestContextBackfillTopicAndBatchMixedResults(t *testing.T) {
	db, ctx, svc, backfill, queue, model := backfillFixture(t)
	tag := types.KnowledgeTag{ID: "history-topic", TenantID: 1, KnowledgeBaseID: "p3-kb", Name: "Historical launches", Kind: types.TagKindTopic}
	require.NoError(t, db.Create(&tag).Error)
	for i := 0; i < 3; i++ {
		message := p3Message(t, db, fmt.Sprintf("topic-message-%d", i), onDemandBody)
		require.NoError(t, svc.ProcessMessage(ctx, message, onDemandBody))
		if i < 2 {
			require.NoError(t, db.Create(&types.KnowledgeTagRelation{KnowledgeID: message.ID, TagID: tag.ID}).Error)
		}
	}
	_, err := backfill.Start(ctx, types.KnowledgeContextBackfillTarget{TopicID: tag.ID})
	require.Error(t, err, "backfill requires a manually starred topic")
	require.NoError(t, svc.SetTopicStar(ctx, tag.ID, true))
	batch, err := backfill.Start(ctx, types.KnowledgeContextBackfillTarget{TopicID: tag.ID})
	require.NoError(t, err)
	require.Equal(t, 2, batch.MessageCount)
	require.NoError(t, backfill.Handle(ctx, queue.tasks[0]))
	model.reply = func(context.Context, []chat.Message, *chat.ChatOptions) (string, error) {
		return "", errors.New("temporary model failure")
	}
	require.Error(t, backfill.Handle(ctx, queue.tasks[1]))
	batch, err = backfill.Batch(ctx, batch.ID)
	require.NoError(t, err)
	require.Equal(t, "completed_with_failures", batch.Status)
	require.Equal(t, 1, batch.SuccessCount)
	require.Equal(t, 1, batch.FailureCount)
	require.NotNil(t, batch.CompletedAt)
	require.NoError(t, svc.SetTopicStar(ctx, tag.ID, false))
	var full int64
	require.NoError(t, db.Model(&types.KnowledgeEvidence{}).Where("extraction_mode=?", types.KnowledgeExtractionFull).Count(&full).Error)
	require.EqualValues(t, 1, full)
	_, err = backfill.Estimate(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme", TopicID: tag.ID})
	require.Error(t, err)
}

func TestContextBackfillBudgetIncludesAllRestoredNames(t *testing.T) {
	db, ctx, svc, backfill, queue, model := backfillFixture(t)
	body := strings.Repeat(onDemandBody+"\n", 60)
	message := p3Message(t, db, "budget-backfill", body)
	require.NoError(t, svc.ProcessMessage(ctx, message, body))
	for i := 0; i < 16; i++ {
		name := fmt.Sprintf("Acme historical legal name %d %s", i, strings.Repeat("Long Name ", 12))
		require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: uuid.NewString(), TenantID: 1, EntityID: "acme", EntityType: "company", Alias: name, NormalizedAlias: normalizeContextName(name)}).Error)
	}
	evidence := onDemandEvidence(t, db, message.ID)
	domain, err := NewKnowledgeDomainService(db).GetDomainForKnowledgeBase(ctx, 1, "p3-kb")
	require.NoError(t, err)
	entities, _, err := backfill.restoredEntities(ctx, &evidence)
	require.NoError(t, err)
	metadata, _ := json.Marshal(map[string]any{"title": evidence.Title, "published_at": evidence.PublishedAt, "author": evidence.Author, "publisher": evidence.Publisher, "original_url": evidence.OriginalURL, "body": "", "segment": 1, "total_segments": defaultContextMaxSegments, "verified_entities": contextSupplementObjects(entities)})
	// Leave a small body allowance after the entire restored identity payload.
	window := contextTokenEstimate(contextExtractionPrompt(domain, types.KnowledgeExtractionFull)+string(metadata)) + 512 + 256 + 300
	require.NoError(t, db.Create(&types.Model{ID: "p3-chat", TenantID: 1, Name: "bounded", Type: types.ModelTypeKnowledgeQA, Source: types.ModelSourceRemote, Status: types.ModelStatusActive, Parameters: types.ModelParameters{ContextWindow: window, MaxOutputTokens: 512}}).Error)
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	estimate, err := backfill.Estimate(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
	require.NoError(t, err)
	require.Greater(t, estimate.EstimatedExtractionCalls, 1)
	model.reply = func(callCtx context.Context, messages []chat.Message, options *chat.ChatOptions) (string, error) {
		stage, _ := types.LLMCallMetadataFromContext(callCtx)
		require.Equal(t, "context_backfill_extraction", stage)
		require.LessOrEqual(t, contextTokenEstimate(messages[0].Content+messages[1].Content)+options.MaxTokens+256, window)
		require.Contains(t, messages[1].Content, "historical legal name 15")
		return `{"facts":[],"events":[]}`, nil
	}
	_, err = backfill.Start(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
	require.NoError(t, err)
	require.NoError(t, backfill.Handle(ctx, queue.tasks[0]))
	require.Equal(t, estimate.EstimatedExtractionCalls, model.calls["context_backfill_extraction"])
}

func TestContextBackfillDuplicateDeliveriesSerializeAndKeepBatchCounts(t *testing.T) {
	for _, firstFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("first_fails_%v", firstFails), func(t *testing.T) {
			db, ctx, svc, backfill, queue, model := backfillFixture(t)
			message := p3Message(t, db, "concurrent-backfill", onDemandBody)
			require.NoError(t, svc.ProcessMessage(ctx, message, onDemandBody))
			require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
			batch, err := backfill.Start(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
			require.NoError(t, err)
			entered, release := make(chan struct{}), make(chan struct{})
			var extractionCalls atomic.Int32
			model.reply = func(callCtx context.Context, _ []chat.Message, _ *chat.ChatOptions) (string, error) {
				stage, _ := types.LLMCallMetadataFromContext(callCtx)
				if stage == "context_event_judgment" {
					return `{"relation":"UNRELATED","confidence":1}`, nil
				}
				if extractionCalls.Add(1) == 1 {
					close(entered)
					<-release
					if firstFails {
						return "", errors.New("first delivery failed")
					}
				}
				return onDemandOutput, nil
			}
			firstResult, secondResult := make(chan error, 1), make(chan error, 1)
			go func() { firstResult <- backfill.Handle(ctx, queue.tasks[0]) }()
			<-entered
			go func() { secondResult <- backfill.Handle(ctx, queue.tasks[0]) }()
			var payload types.KnowledgeContextBackfillPayload
			require.NoError(t, json.Unmarshal(queue.tasks[0].Payload(), &payload))
			lockKey := fmt.Sprintf("reliquary:context-backfill:%d:%s", payload.TenantID, payload.ItemID)
			// Confirm the second delivery is blocked on the same item lock.
			require.Eventually(t, func() bool {
				var waiting int64
				err := db.Raw(`SELECT COUNT(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted AND classid=(hashtextextended(?,0) >> 32)::bit(32)::bigint AND objid=hashtextextended(?,0)::bit(32)::bigint`, lockKey, lockKey).Scan(&waiting).Error
				return err == nil && waiting > 0
			}, 3*time.Second, 10*time.Millisecond)
			require.EqualValues(t, 1, extractionCalls.Load())
			close(release)
			if firstFails {
				require.Error(t, <-firstResult)
			} else {
				require.NoError(t, <-firstResult)
			}
			require.NoError(t, <-secondResult)
			batch, err = backfill.Batch(ctx, batch.ID)
			require.NoError(t, err)
			require.Equal(t, "completed", batch.Status)
			require.Equal(t, 1, batch.SuccessCount)
			require.Zero(t, batch.FailureCount)
			require.Empty(t, batch.Items[0].Error)
			expectedCalls := int32(1)
			if firstFails {
				expectedCalls = 2
			}
			require.Equal(t, expectedCalls, extractionCalls.Load())
			require.NoError(t, backfill.Handle(ctx, queue.tasks[0]), "a late completed delivery cannot reopen its item")
			require.Equal(t, expectedCalls, extractionCalls.Load())
			var facts, events int64
			require.NoError(t, db.Model(&types.KnowledgeFact{}).Where("predicate='staff'").Count(&facts).Error)
			require.NoError(t, db.Model(&types.KnowledgeEventEvidence{}).Count(&events).Error)
			require.EqualValues(t, 1, facts)
			require.EqualValues(t, 1, events)
		})
	}
}

func TestContextBackfillAmbiguousAliasFailsWithoutBindingWrongObject(t *testing.T) {
	db, ctx, svc, backfill, queue, model := backfillFixture(t)
	body := onDemandBody + " Shared name has 50 staff."
	message := p3Message(t, db, "ambiguous-backfill", body)
	require.NoError(t, svc.ProcessMessage(ctx, message, body))
	for i, entityID := range []string{"acme", "maker"} {
		require.NoError(t, db.Create(&types.KnowledgeEntityAlias{ID: uuid.NewString(), TenantID: 1, EntityID: entityID, EntityType: fmt.Sprint(i), Alias: "Shared name", NormalizedAlias: "shared name"}).Error)
	}
	require.NoError(t, svc.SetEntityStar(ctx, "acme", true))
	evidence := onDemandEvidence(t, db, message.ID)
	model.reply = func(context.Context, []chat.Message, *chat.ChatOptions) (string, error) {
		return `{"facts":[{"kind":"attribute","subject":"Shared name","predicate":"staff","value":50,"value_type":"number","quote":"Shared name has 50 staff."}],"events":[]}`, nil
	}
	batch, err := backfill.Start(ctx, types.KnowledgeContextBackfillTarget{EntityID: "acme"})
	require.NoError(t, err)
	require.ErrorContains(t, backfill.Handle(ctx, queue.tasks[0]), "subject could not be resolved")
	require.Equal(t, evidence, onDemandEvidence(t, db, message.ID))
	batch, err = backfill.Batch(ctx, batch.ID)
	require.NoError(t, err)
	require.Equal(t, 1, batch.FailureCount)
	var open int64
	require.NoError(t, db.Model(&types.KnowledgeFact{}).Where("predicate='staff'").Count(&open).Error)
	require.Zero(t, open)
}

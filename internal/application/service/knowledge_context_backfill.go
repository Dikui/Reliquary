package service

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

const contextBackfillBatchLimit = 200

var errContextBackfillSkipped = errors.New("message is being reprocessed or its evidence is no longer eligible")

// Backfill only enriches an existing immutable snapshot. It deliberately does
// not invoke source parsing, entity resolution, alias persistence or attention.
type KnowledgeContextBackfillService struct {
	db      *gorm.DB
	context *KnowledgeContextService
	tasks   interfaces.TaskEnqueuer
}

func NewKnowledgeContextBackfillService(db *gorm.DB, contextService *KnowledgeContextService, tasks interfaces.TaskEnqueuer) *KnowledgeContextBackfillService {
	return &KnowledgeContextBackfillService{db: db, context: contextService, tasks: tasks}
}

func (s *KnowledgeContextBackfillService) targetScope(ctx context.Context, target types.KnowledgeContextBackfillTarget, requireStar bool) (*contextQueryScope, error) {
	if (target.EntityID == "") == (target.TopicID == "") {
		return nil, werrors.NewBadRequestError("exactly one entity_id or topic_id is required")
	}
	scope, err := s.context.resolveContextQueryScope(ctx, "", types.KnowledgeContextQuery{Scope: "all"})
	if err != nil {
		return nil, err
	}
	var count int64
	if target.EntityID != "" {
		if err := s.db.WithContext(ctx).Model(&types.KnowledgeEntity{}).Where("tenant_id=? AND id=? AND merged_into_id=''", scope.tenantID, target.EntityID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, werrors.NewNotFoundError("knowledge entity not found")
		}
		if requireStar {
			if err := s.db.WithContext(ctx).Model(&types.KnowledgeEntityStar{}).Where("tenant_id=? AND entity_id=?", scope.tenantID, target.EntityID).Count(&count).Error; err != nil {
				return nil, err
			}
		}
	} else {
		var tag types.KnowledgeTag
		if err := s.db.WithContext(ctx).Where("tenant_id=? AND id=? AND deleted_at IS NULL", scope.tenantID, target.TopicID).First(&tag).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, werrors.NewNotFoundError("knowledge topic not found")
			}
			return nil, err
		}
		if !concreteContextTopic(tag.Kind) {
			return nil, werrors.NewBadRequestError("only concrete referent tags can be backfilled")
		}
		count = 1
		if requireStar {
			if err := s.db.WithContext(ctx).Model(&types.KnowledgeTopicStar{}).Where("tenant_id=? AND tag_id=?", scope.tenantID, target.TopicID).Count(&count).Error; err != nil {
				return nil, err
			}
		}
	}
	if requireStar && count == 0 {
		return nil, werrors.NewBadRequestError("star the object or topic before requesting backfill")
	}
	return scope, nil
}

func (s *KnowledgeContextBackfillService) relatedQuery(db *gorm.DB, scope *contextQueryScope, target types.KnowledgeContextBackfillTarget) *gorm.DB {
	query := db.Model(&types.KnowledgeEvidence{}).Where("tenant_id=? AND knowledge_base_id IN ? AND status=? AND extraction_mode=?", scope.tenantID, scope.kbIDs, types.KnowledgeEvidenceCurrent, types.KnowledgeExtractionTemplateOnly).
		Where("version = (SELECT MAX(newer.version) FROM knowledge_context_evidence newer WHERE newer.tenant_id=knowledge_context_evidence.tenant_id AND newer.knowledge_id=knowledge_context_evidence.knowledge_id)")
	if target.EntityID != "" {
		query = query.Where("id IN (?)", db.Model(&types.KnowledgeEvidenceEntity{}).Select("evidence_id").Where("tenant_id=? AND entity_id=?", scope.tenantID, target.EntityID))
	} else {
		query = query.Where("knowledge_id IN (?)", db.Model(&types.KnowledgeTagRelation{}).Select("knowledge_id").Where("tag_id=?", target.TopicID))
	}
	return query
}

func (s *KnowledgeContextBackfillService) candidates(ctx context.Context, db *gorm.DB, scope *contextQueryScope, target types.KnowledgeContextBackfillTarget) ([]types.KnowledgeEvidence, int, error) {
	var evidence []types.KnowledgeEvidence
	if err := s.relatedQuery(db.WithContext(ctx), scope, target).Find(&evidence).Error; err != nil {
		return nil, 0, err
	}
	// A completed snapshot stays selectable while its message is reprocessing.
	// The worker records that case as a skipped failure without reading models.
	var sources []types.Knowledge
	var sourceIDs []string
	for _, item := range evidence {
		sourceIDs = append(sourceIDs, item.KnowledgeID)
	}
	if len(sourceIDs) > 0 {
		if err := db.WithContext(ctx).Where("tenant_id=? AND id IN ?", scope.tenantID, sourceIDs).Find(&sources).Error; err != nil {
			return nil, 0, err
		}
	}
	allowed := map[string]bool{}
	for _, source := range sources {
		if source.IsArchivedSource() || source.IsSourceUpdateCandidate() || source.EnableStatus == "disabled" {
			continue
		}
		intake, err := source.IntakeMetadata()
		if err != nil || (intake != nil && intake.Decision != "approved") {
			continue
		}
		allowed[source.ID] = true
	}
	visible := make([]types.KnowledgeEvidence, 0, len(evidence))
	for _, item := range evidence {
		if allowed[item.KnowledgeID] {
			visible = append(visible, item)
		}
	}
	total := len(visible)
	var active []string
	if err := db.WithContext(ctx).Model(&types.KnowledgeContextBackfillItem{}).Where("tenant_id=? AND status IN ?", scope.tenantID, []string{"pending", "processing"}).Pluck("evidence_id", &active).Error; err != nil {
		return nil, 0, err
	}
	busy := map[string]bool{}
	for _, id := range active {
		busy[id] = true
	}
	eligible := make([]types.KnowledgeEvidence, 0, len(visible))
	for _, item := range visible {
		if item.ExtractionStatus == types.KnowledgeExtractionComplete && !busy[item.ID] {
			eligible = append(eligible, item)
		}
	}
	sort.Slice(eligible, func(i, j int) bool {
		left, right := parseContextTime(eligible[i].PublishedAt), parseContextTime(eligible[j].PublishedAt)
		if left != nil && right != nil && !left.Equal(*right) {
			return left.After(*right)
		}
		if (left != nil) != (right != nil) {
			return left != nil
		}
		if !eligible[i].CreatedAt.Equal(eligible[j].CreatedAt) {
			return eligible[i].CreatedAt.After(eligible[j].CreatedAt)
		}
		return eligible[i].ID > eligible[j].ID
	})
	return eligible, total, nil
}

func (s *KnowledgeContextBackfillService) supplementPlan(ctx context.Context, evidence *types.KnowledgeEvidence, domain *types.KnowledgeDomain, restored ...[]contextExtractedEntity) (contextSegmentPlan, error) {
	var kb types.KnowledgeBase
	if err := s.db.WithContext(ctx).Where("tenant_id=? AND id=?", evidence.TenantID, evidence.KnowledgeBaseID).First(&kb).Error; err != nil {
		return contextSegmentPlan{}, err
	}
	modelID, err := ResolveModelTier(ctx, s.db, &kb, types.ModelTierExtraction)
	if err != nil {
		return contextSegmentPlan{}, err
	}
	window, output, err := s.context.contextExtractionLimits(ctx, modelID)
	if err != nil {
		return contextSegmentPlan{}, err
	}
	var entities []contextExtractedEntity
	if len(restored) > 0 {
		entities = restored[0]
	} else {
		var err error
		entities, _, err = s.restoredEntities(ctx, evidence)
		if err != nil {
			return contextSegmentPlan{}, err
		}
	}
	metadata, _ := json.Marshal(map[string]any{"title": evidence.Title, "published_at": evidence.PublishedAt, "author": evidence.Author, "publisher": evidence.Publisher, "original_url": evidence.OriginalURL, "body": "", "segment": 1, "total_segments": defaultContextMaxSegments, "verified_entities": contextSupplementObjects(entities)})
	return planContextSegments(evidence.Body, contextExtractionPrompt(domain, types.KnowledgeExtractionFull)+string(metadata), window, output, contextConfiguredPositive("RELIQUARY_CONTEXT_MAX_SEGMENTS", defaultContextMaxSegments))
}

func (s *KnowledgeContextBackfillService) Estimate(ctx context.Context, target types.KnowledgeContextBackfillTarget) (*types.KnowledgeContextBackfillEstimate, error) {
	scope, err := s.targetScope(ctx, target, false)
	if err != nil {
		return nil, err
	}
	candidates, total, err := s.candidates(ctx, s.db, scope, target)
	if err != nil {
		return nil, err
	}
	count := min(len(candidates), contextBackfillBatchLimit)
	result := &types.KnowledgeContextBackfillEstimate{TemplateOnlyMessageCount: total, PendingMessageCount: len(candidates), BatchMessageCount: count, RemainingMessageCount: len(candidates) - count,
		EstimatedEventCalls: count, EstimateNote: "Event judgments are estimated at one per message; actual calls depend on extracted events."}
	for i := 0; i < count; i++ {
		plan, err := s.supplementPlan(ctx, &candidates[i], scope.domains[candidates[i].KnowledgeBaseID])
		if err != nil {
			return nil, err
		}
		result.EstimatedExtractionCalls += len(plan.Segments)
	}
	result.EstimatedModelCalls = result.EstimatedExtractionCalls + result.EstimatedEventCalls
	return result, nil
}

func (s *KnowledgeContextBackfillService) Start(ctx context.Context, target types.KnowledgeContextBackfillTarget) (*types.KnowledgeContextBackfillBatch, error) {
	if s.tasks == nil {
		return nil, werrors.NewInternalServerError("backfill task queue unavailable")
	}
	scope, err := s.targetScope(ctx, target, true)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	batch := &types.KnowledgeContextBackfillBatch{ID: uuid.NewString(), TenantID: scope.tenantID, EntityID: target.EntityID, TopicID: target.TopicID, Status: "pending", CreatedAt: now, UpdatedAt: now, Items: []types.KnowledgeContextBackfillItem{}}
	var selected []types.KnowledgeEvidence
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContextTeam(tx, scope.tenantID); err != nil {
			return err
		}
		eligible, _, err := s.candidates(ctx, tx, scope, target)
		if err != nil {
			return err
		}
		selected = eligible[:min(len(eligible), contextBackfillBatchLimit)]
		batch.MessageCount = len(selected)
		if len(selected) == 0 {
			batch.Status = "completed"
			batch.CompletedAt = &now
		}
		if err := tx.Create(batch).Error; err != nil {
			return err
		}
		for _, evidence := range selected {
			item := types.KnowledgeContextBackfillItem{ID: uuid.NewString(), TenantID: scope.tenantID, BatchID: batch.ID, KnowledgeID: evidence.KnowledgeID, EvidenceID: evidence.ID, Status: "pending", CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			batch.Items = append(batch.Items, item)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	for i, evidence := range selected {
		item := batch.Items[i]
		payload, _ := json.Marshal(types.KnowledgeContextBackfillPayload{TenantID: scope.tenantID, KnowledgeBaseID: evidence.KnowledgeBaseID, KnowledgeID: evidence.KnowledgeID, BatchID: batch.ID, ItemID: item.ID, EvidenceID: evidence.ID})
		_, queueErr := s.tasks.Enqueue(asynq.NewTask(types.TypeKnowledgeContextBackfill, payload), asynq.Queue(types.QueueMaintenance), asynq.MaxRetry(3), asynq.Timeout(10*time.Minute), asynq.TaskID("context-backfill:"+item.ID))
		if queueErr != nil {
			if err := s.finishItem(ctx, item.ID, scope.tenantID, "failed", "enqueue failed: "+queueErr.Error()); err != nil {
				return nil, errors.Join(queueErr, err)
			}
		}
	}
	return s.Batch(ctx, batch.ID)
}

func (s *KnowledgeContextBackfillService) Batches(ctx context.Context, target types.KnowledgeContextBackfillTarget) ([]types.KnowledgeContextBackfillBatch, error) {
	scope, err := s.targetScope(ctx, target, false)
	if err != nil {
		return nil, err
	}
	items := []types.KnowledgeContextBackfillBatch{}
	query := s.db.WithContext(ctx).Where("tenant_id=?", scope.tenantID)
	if target.EntityID != "" {
		query = query.Where("entity_id=?", target.EntityID)
	} else {
		query = query.Where("topic_id=?", target.TopicID)
	}
	if err := query.Order("created_at DESC,id DESC").Limit(50).Find(&items).Error; err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Items = []types.KnowledgeContextBackfillItem{}
	}
	return items, nil
}

func (s *KnowledgeContextBackfillService) Batch(ctx context.Context, id string) (*types.KnowledgeContextBackfillBatch, error) {
	tenantID, err := s.context.requireTenant(ctx)
	if err != nil {
		return nil, err
	}
	var batch types.KnowledgeContextBackfillBatch
	if err := s.db.WithContext(ctx).Where("tenant_id=? AND id=?", tenantID, id).First(&batch).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, werrors.NewNotFoundError("backfill batch not found")
		}
		return nil, err
	}
	batch.Items = []types.KnowledgeContextBackfillItem{}
	if err := s.db.WithContext(ctx).Where("tenant_id=? AND batch_id=?", tenantID, id).Order("created_at,id").Find(&batch.Items).Error; err != nil {
		return nil, err
	}
	return &batch, nil
}

func (s *KnowledgeContextBackfillService) finishItem(ctx context.Context, id string, tenantID uint64, status, detail string) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.db.WithContext(persistCtx).Transaction(func(tx *gorm.DB) error { return finishContextBackfillItem(tx, id, tenantID, status, detail) })
}

func finishContextBackfillItem(tx *gorm.DB, id string, tenantID uint64, status, detail string) error {
	var item types.KnowledgeContextBackfillItem
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", tenantID, id).First(&item).Error; err != nil {
		return err
	}
	if item.Status == "completed" || (item.Status == "failed" && status != "completed") {
		return nil
	}
	previousStatus := item.Status
	now := time.Now().UTC()
	if len([]rune(detail)) > 1000 {
		detail = string([]rune(detail)[:1000])
	}
	if err := tx.Model(&item).Updates(map[string]any{"status": status, "error": detail, "updated_at": now}).Error; err != nil {
		return err
	}
	var batch types.KnowledgeContextBackfillBatch
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", tenantID, item.BatchID).First(&batch).Error; err != nil {
		return err
	}
	if status == "completed" {
		batch.SuccessCount++
		if previousStatus == "failed" {
			batch.FailureCount = max(0, batch.FailureCount-1)
		}
	} else {
		batch.FailureCount++
	}
	batch.Status = "processing"
	if batch.SuccessCount+batch.FailureCount == batch.MessageCount {
		batch.Status = "completed"
		if batch.FailureCount > 0 {
			batch.Status = "completed_with_failures"
		}
		if batch.FailureCount == batch.MessageCount {
			batch.Status = "failed"
		}
		batch.CompletedAt = &now
	}
	return tx.Model(&batch).Updates(map[string]any{"success_count": batch.SuccessCount, "failure_count": batch.FailureCount, "status": batch.Status, "completed_at": batch.CompletedAt, "updated_at": now}).Error
}

func (s *KnowledgeContextBackfillService) Handle(ctx context.Context, task *asynq.Task) error {
	var payload types.KnowledgeContextBackfillPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("backfill task: %w", err)
	}
	if payload.TenantID == 0 || payload.ItemID == "" || payload.EvidenceID == "" || payload.BatchID == "" {
		return errors.New("invalid backfill task scope")
	}
	ctx = context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID)
	release, err := s.lockTask(ctx, payload.TenantID, payload.ItemID)
	if err != nil {
		return err
	}
	defer release()
	var item types.KnowledgeContextBackfillItem
	done := false
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=? AND batch_id=? AND evidence_id=? AND knowledge_id=?", payload.TenantID, payload.ItemID, payload.BatchID, payload.EvidenceID, payload.KnowledgeID).First(&item).Error; err != nil {
			return err
		}
		if item.Status == "completed" {
			done = true
			return nil
		}
		var batch types.KnowledgeContextBackfillBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", payload.TenantID, payload.BatchID).First(&batch).Error; err != nil {
			return err
		}
		// Generic dead-letter requeue reuses the original payload. Reopen its
		// failed item so a manual retry can recover the same batch accurately.
		if item.Status == "failed" {
			batch.FailureCount = max(0, batch.FailureCount-1)
		}
		if err := tx.Model(&item).Updates(map[string]any{"status": "processing", "error": "", "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		return tx.Model(&batch).Updates(map[string]any{"status": "processing", "failure_count": batch.FailureCount, "completed_at": nil, "updated_at": time.Now()}).Error
	}); err != nil {
		return err
	}
	if done {
		return nil
	}
	err = s.process(ctx, payload)
	if errors.Is(err, errContextBackfillSkipped) {
		return s.finishItem(ctx, item.ID, payload.TenantID, "failed", err.Error())
	}
	if err == nil {
		return nil
	}
	retried, retryOK := asynq.GetRetryCount(ctx)
	maxRetry, maxOK := asynq.GetMaxRetry(ctx)
	if !retryOK || !maxOK || retried >= maxRetry {
		if persistErr := s.finishItem(ctx, item.ID, payload.TenantID, "failed", err.Error()); persistErr != nil {
			return errors.Join(err, persistErr)
		}
	} else {
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if persistErr := s.db.WithContext(persistCtx).Model(&item).Where("status='processing'").Update("error", err.Error()).Error; persistErr != nil {
			return errors.Join(err, persistErr)
		}
	}
	return err
}

// A session lock serializes duplicate deliveries through model work without
// holding a database transaction. Requeues use the same item identity.
func (s *KnowledgeContextBackfillService) lockTask(ctx context.Context, tenantID uint64, itemID string) (func(), error) {
	database, err := s.db.DB()
	if err != nil {
		return nil, err
	}
	connection, err := database.Conn(ctx)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("reliquary:context-backfill:%d:%s", tenantID, itemID)
	if _, err := connection.ExecContext(ctx, "SELECT pg_advisory_lock(hashtextextended($1, 0))", key); err != nil {
		// Cancellation can race the server acquiring a session lock. Discard the
		// connection instead of returning any uncertain lock to the pool.
		_ = connection.Raw(func(any) error { return driver.ErrBadConn })
		_ = connection.Close()
		return nil, err
	}
	return func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := connection.ExecContext(unlockCtx, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", key); err != nil {
			_ = connection.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = connection.Close()
	}, nil
}

func contextBackfillMessageEligible(message *types.Knowledge) bool {
	if message.ParseStatus != types.ParseStatusCompleted || message.IsArchivedSource() || message.IsSourceUpdateCandidate() || message.EnableStatus == "disabled" {
		return false
	}
	var metadata struct {
		Context struct {
			Status string `json:"status"`
		} `json:"knowledge_context"`
	}
	if len(message.Metadata) > 0 && json.Unmarshal(message.Metadata, &metadata) != nil {
		return false
	}
	intake, err := message.IntakeMetadata()
	if err != nil || (intake != nil && intake.Decision != "approved") {
		return false
	}
	return metadata.Context.Status != "processing"
}

func (s *KnowledgeContextBackfillService) restoredEntities(ctx context.Context, evidence *types.KnowledgeEvidence) ([]contextExtractedEntity, map[string]types.KnowledgeEntity, error) {
	var entities []types.KnowledgeEntity
	if err := s.db.WithContext(ctx).Where("tenant_id=? AND id IN (?) AND merged_into_id=''", evidence.TenantID,
		s.db.Model(&types.KnowledgeEvidenceEntity{}).Select("entity_id").Where("tenant_id=? AND evidence_id=?", evidence.TenantID, evidence.ID)).Order("id").Find(&entities).Error; err != nil {
		return nil, nil, err
	}
	mentions := map[string]types.KnowledgeEntity{}
	ambiguous := map[string]bool{}
	result := []contextExtractedEntity{}
	for _, entity := range entities {
		var aliases []string
		if err := s.db.WithContext(ctx).Model(&types.KnowledgeEntityAlias{}).Where("tenant_id=? AND entity_id=?", evidence.TenantID, entity.ID).Order("alias,id").Pluck("alias", &aliases).Error; err != nil {
			return nil, nil, err
		}
		result = append(result, contextExtractedEntity{Mention: entity.CanonicalName, Canonical: entity.CanonicalName, EntityType: entity.EntityType, Description: entity.Description, Aliases: aliases})
		for _, name := range append([]string{entity.CanonicalName}, aliases...) {
			key := normalizeContextName(name)
			if existing, ok := mentions[key]; ok && existing.ID != entity.ID {
				ambiguous[key] = true
			}
			mentions[key] = entity
		}
	}
	// Names remain in the model input, but an ambiguous endpoint cannot silently
	// resolve to whichever linked object happened to be loaded last.
	for key := range ambiguous {
		delete(mentions, key)
	}
	return result, mentions, nil
}

func (s *KnowledgeContextBackfillService) process(ctx context.Context, payload types.KnowledgeContextBackfillPayload) error {
	var evidence types.KnowledgeEvidence
	if err := s.db.WithContext(ctx).Where("tenant_id=? AND id=? AND knowledge_id=? AND knowledge_base_id=?", payload.TenantID, payload.EvidenceID, payload.KnowledgeID, payload.KnowledgeBaseID).First(&evidence).Error; err != nil {
		return err
	}
	if evidence.Status == types.KnowledgeEvidenceCurrent && evidence.ExtractionStatus == types.KnowledgeExtractionComplete && evidence.ExtractionMode == types.KnowledgeExtractionFull {
		return s.finishItem(ctx, payload.ItemID, payload.TenantID, "completed", "")
	}
	if evidence.Status != types.KnowledgeEvidenceCurrent || evidence.ExtractionStatus != types.KnowledgeExtractionComplete || evidence.ExtractionMode != types.KnowledgeExtractionTemplateOnly {
		return errContextBackfillSkipped
	}
	var message types.Knowledge
	if err := s.db.WithContext(ctx).Where("tenant_id=? AND id=?", payload.TenantID, payload.KnowledgeID).First(&message).Error; err != nil {
		return err
	}
	if !contextBackfillMessageEligible(&message) {
		return errContextBackfillSkipped
	}
	if s.context.models == nil {
		return errors.New("model service is unavailable")
	}
	var kb types.KnowledgeBase
	if err := s.db.WithContext(ctx).Where("tenant_id=? AND id=?", payload.TenantID, payload.KnowledgeBaseID).First(&kb).Error; err != nil {
		return err
	}
	domain, err := NewKnowledgeDomainService(s.db).GetDomainForKnowledgeBase(ctx, payload.TenantID, payload.KnowledgeBaseID)
	if err != nil {
		return err
	}
	entities, mentions, err := s.restoredEntities(ctx, &evidence)
	if err != nil {
		return err
	}
	plan, err := s.supplementPlan(ctx, &evidence, domain, entities)
	if err != nil {
		return err
	}
	model, err := GetTierChatModel(ctx, s.db, s.context.models, &kb, types.ModelTierExtraction)
	if err != nil {
		return err
	}
	light, err := GetTierChatModel(ctx, s.db, s.context.models, &kb, types.ModelTierLight)
	if err != nil {
		return err
	}
	ctx, calls := types.WithModelCallCollector(ctx)
	ctx = withContextResolutionEvidence(ctx, &evidence)
	extracted, _, err := extractContextSupplementSegmentsWithPlan(types.WithLLMCallMetadata(ctx, "context_backfill_extraction", ""), model, &evidence, domain, plan, entities)
	if err != nil {
		return err
	}
	if evidence.SourceKind == types.KnowledgeEvidenceSourceKindRoundup {
		extracted.SourceKind = evidence.SourceKind
		extracted.Events = nil
	}
	facts, err := materializeContextFacts(extracted.Facts, mentions, &evidence, domain)
	if err != nil {
		return err
	}
	events, err := s.context.resolveContextEvents(types.WithLLMCallMetadata(ctx, "context_event_resolution", ""), light, payload.TenantID, payload.KnowledgeBaseID, extracted.Events, mentions)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContextTeamShared(tx, payload.TenantID); err != nil {
			return err
		}
		var currentMessage types.Knowledge
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", payload.TenantID, payload.KnowledgeID).First(&currentMessage).Error; err != nil {
			return err
		}
		if !contextBackfillMessageEligible(&currentMessage) {
			return errContextBackfillSkipped
		}
		var current types.KnowledgeEvidence
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", payload.TenantID, evidence.ID).First(&current).Error; err != nil {
			return err
		}
		if current.ExtractionMode == types.KnowledgeExtractionFull {
			return finishContextBackfillItem(tx, payload.ItemID, payload.TenantID, "completed", "")
		}
		if current.Status != types.KnowledgeEvidenceCurrent || current.ExtractionStatus != types.KnowledgeExtractionComplete {
			return errContextBackfillSkipped
		}
		// Names and aliases may change during model work. Endpoint identity must
		// still exist; a merge invalidates this attempt rather than recreating it.
		for _, entity := range mentions {
			var count int64
			if err := tx.Model(&types.KnowledgeEntity{}).Where("tenant_id=? AND id=? AND merged_into_id=''", payload.TenantID, entity.ID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return errors.New("backfill object identity changed while extracting")
			}
		}
		for _, fact := range facts {
			if err := tx.Create(&fact).Error; err != nil {
				return err
			}
		}
		if err := persistContextBackfillEvents(tx, payload.TenantID, payload.KnowledgeBaseID, &evidence, extracted.Events, events); err != nil {
			return err
		}
		var previous []types.ModelCallCount
		if err := json.Unmarshal(current.ModelCalls, &previous); err != nil {
			return err
		}
		encoded, _ := json.Marshal(append(previous, calls.Snapshot()...))
		now := time.Now().UTC()
		updates := map[string]any{"extraction_mode": types.KnowledgeExtractionFull, "backfilled_at": now, "backfill_template_updated_at": domain.UpdatedAt, "model_calls": types.JSON(encoded)}
		if extracted.SourceKind == types.KnowledgeEvidenceSourceKindRoundup {
			updates["source_kind"] = extracted.SourceKind
		}
		if err := tx.Model(&current).Updates(updates).Error; err != nil {
			return err
		}
		return finishContextBackfillItem(tx, payload.ItemID, payload.TenantID, "completed", "")
	})
}

func persistContextBackfillEvents(tx *gorm.DB, tenantID uint64, kbID string, evidence *types.KnowledgeEvidence, sources []contextExtractedEvent, events []contextEventResolution) error {
	var ids []string
	for _, resolution := range events {
		ids = append(ids, resolution.Event.ID)
		if resolution.RelatedEventID != "" {
			ids = append(ids, resolution.RelatedEventID)
		}
	}
	if len(ids) > 0 {
		var locked []types.KnowledgeEvent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("tenant_id=? AND id IN ?", tenantID, ids).Order("id").Find(&locked).Error; err != nil {
			return err
		}
	}
	for i := range events {
		resolution := &events[i]
		event := &resolution.Event
		var existing types.KnowledgeEvent
		err := tx.Where("tenant_id=? AND id=?", tenantID, event.ID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			source := sources[i]
			event.KnowledgeBaseID = kbID
			event.Title, event.EventType, event.Description = strings.TrimSpace(source.Title), strings.TrimSpace(source.EventType), strings.TrimSpace(source.Description)
			event.OccurredAt = verifiedContextTime(source.OccurredAt, source.Quote)
			event.EvidenceIDs = types.StringArray{}
			event.CreatedAt, event.UpdatedAt = time.Now(), time.Now()
			if err := tx.Create(event).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if existing.OccurredAt == nil {
				existing.OccurredAt = event.OccurredAt
			}
			if err := tx.Model(&existing).Updates(map[string]any{"occurred_at": existing.OccurredAt, "updated_at": time.Now()}).Error; err != nil {
				return err
			}
			sourceIDs := event.SourceEntityIDs
			event = &existing
			event.SourceEntityIDs = sourceIDs
		}
		if resolution.RelatedEventID != "" {
			var count int64
			if err := tx.Model(&types.KnowledgeEvent{}).Where("tenant_id=? AND id=?", tenantID, resolution.RelatedEventID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				resolution.RelatedEventID, resolution.Relation = "", ""
			}
		}
		if err := attachContextEventEvidence(tx, tenantID, kbID, event, evidence, sources[i], resolution.Relation, resolution.RelatedEventID); err != nil {
			return err
		}
	}
	return nil
}

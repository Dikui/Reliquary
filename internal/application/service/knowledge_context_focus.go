package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func concreteContextTopic(kind string) bool {
	return kind == types.TagKindTopic || kind == types.TagKindObject || kind == types.TagKindEvent
}

// SetEntityStar only changes future intake focus. It never enqueues extraction
// or changes facts, events, or evidence that have already been recorded.
func (s *KnowledgeContextService) SetEntityStar(ctx context.Context, entityID string, starred bool) error {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return err
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContextTeam(tx, tenantID); err != nil {
			return err
		}
		var entity types.KnowledgeEntity
		if err := tx.Where("tenant_id = ? AND id = ? AND merged_into_id = ''", tenantID, entityID).First(&entity).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("knowledge entity not found")
			}
			return err
		}
		if !starred {
			return tx.Where("tenant_id = ? AND entity_id = ?", tenantID, entityID).Delete(&types.KnowledgeEntityStar{}).Error
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&types.KnowledgeEntityStar{TenantID: tenantID, EntityID: entityID, CreatedAt: time.Now()}).Error
	})
}

func (s *KnowledgeContextService) SetTopicStar(ctx context.Context, tagID string, starred bool) error {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tag types.KnowledgeTag
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, tagID).First(&tag).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return werrors.NewNotFoundError("knowledge topic not found")
			}
			return err
		}
		if !concreteContextTopic(tag.Kind) {
			return werrors.NewBadRequestError("only concrete referent tags can be starred")
		}
		if _, err := s.scopedKnowledgeBase(ctx, tag.KnowledgeBaseID); err != nil {
			return err
		}
		if !starred {
			return tx.Where("tenant_id = ? AND tag_id = ?", tenantID, tagID).Delete(&types.KnowledgeTopicStar{}).Error
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&types.KnowledgeTopicStar{TenantID: tenantID, TagID: tagID, CreatedAt: time.Now()}).Error
	})
}

// FocusScope scopes topics and workflow queries by knowledge base/domain/team.
// Object stars apply globally and remain visible across these display scopes,
// subject to API key identity visibility. Only explicit object conditions in
// enabled fact_query steps become tracking focus.
func (s *KnowledgeContextService) FocusScope(ctx context.Context, kbID string, q types.KnowledgeContextQuery) (*types.KnowledgeFocusScope, error) {
	scope, err := s.resolveContextQueryScope(ctx, kbID, q)
	if err != nil {
		return nil, err
	}
	result := &types.KnowledgeFocusScope{
		StarredEntities: []types.KnowledgeEntity{}, StarredTopics: []types.KnowledgeFocusTopic{},
		TrackedEntities: []types.KnowledgeFocusEntity{}, TrackedTopics: []types.KnowledgeFocusTopic{}, AvailableTopics: []types.KnowledgeFocusTopic{},
	}
	query := s.db.WithContext(ctx).Model(&types.KnowledgeEntity{}).
		Joins("JOIN knowledge_context_entity_stars stars ON stars.tenant_id = knowledge_context_entities.tenant_id AND stars.entity_id = knowledge_context_entities.id").
		Where("knowledge_context_entities.tenant_id = ? AND knowledge_context_entities.merged_into_id = ''", scope.tenantID)

	if err := query.Select("knowledge_context_entities.*").Order("canonical_name, knowledge_context_entities.id").Find(&result.StarredEntities).Error; err != nil {
		return nil, err
	}
	if len(scope.kbIDs) > 0 {
		if err := s.db.WithContext(ctx).Model(&types.KnowledgeTag{}).Where("tenant_id = ? AND knowledge_base_id IN ? AND deleted_at IS NULL AND kind IN ?", scope.tenantID, scope.kbIDs,
			[]string{types.TagKindTopic, types.TagKindObject, types.TagKindEvent}).Order("name, id").Find(&result.AvailableTopics).Error; err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).Model(&types.KnowledgeTag{}).
			Joins("JOIN knowledge_context_topic_stars stars ON stars.tenant_id = knowledge_tags.tenant_id AND stars.tag_id = knowledge_tags.id").
			Where("knowledge_tags.tenant_id = ? AND knowledge_tags.knowledge_base_id IN ? AND knowledge_tags.deleted_at IS NULL AND knowledge_tags.kind IN ?", scope.tenantID, scope.kbIDs,
				[]string{types.TagKindTopic, types.TagKindObject, types.TagKindEvent}).Select("knowledge_tags.*").Order("name, knowledge_tags.id").Find(&result.StarredTopics).Error; err != nil {
			return nil, err
		}
	}
	for i := range result.StarredTopics {
		result.StarredTopics[i].KnowledgeBaseName = scope.names[result.StarredTopics[i].KnowledgeBaseID]
	}
	for i := range result.AvailableTopics {
		result.AvailableTopics[i].KnowledgeBaseName = scope.names[result.AvailableTopics[i].KnowledgeBaseID]
	}
	result.TrackedEntities, err = s.contextTrackedEntities(ctx, scope)
	if err != nil {
		return nil, err
	}

	// Current workflows have no typed topic/tag condition. Search strings are
	// not treated as tags; tracked_topics is reserved for an explicit condition.
	return result, nil
}

func (s *KnowledgeContextService) contextTrackedEntities(ctx context.Context, scope *contextQueryScope, resolvedPlans ...[]types.KnowledgeEntity) ([]types.KnowledgeFocusEntity, error) {
	result := []types.KnowledgeFocusEntity{}
	var workflows []types.KnowledgeWorkflow
	workflowQuery := s.db.WithContext(ctx).Model(&types.KnowledgeWorkflow{}).
		Joins("JOIN knowledge_bases bases ON bases.id = knowledge_workflows.knowledge_base_id AND bases.tenant_id = knowledge_workflows.tenant_id AND bases.deleted_at IS NULL").
		Where("knowledge_workflows.tenant_id = ? AND knowledge_workflows.enabled", scope.tenantID)

	if err := workflowQuery.Select("knowledge_workflows.*").Order("knowledge_workflows.id").Find(&workflows).Error; err != nil {
		return nil, err
	}
	if len(workflows) == 0 || len(scope.kbIDs) == 0 {
		return result, nil
	}
	var entities []types.KnowledgeEntity
	if err := s.db.WithContext(ctx).Select("id", "tenant_id", "knowledge_base_id", "canonical_name", "normalized_name", "entity_type", "description", "merged_into_id").Where("tenant_id = ?", scope.tenantID).Find(&entities).Error; err != nil {
		return nil, err
	}
	byID := map[string]types.KnowledgeEntity{}
	for _, entity := range entities {
		byID[entity.ID] = entity
	}
	// Intake object plans have not been persisted yet. Include them when
	// interpreting name conditions so the first appearance can match a task.
	for _, plans := range resolvedPlans {
		for _, entity := range plans {
			if _, exists := byID[entity.ID]; !exists {
				entities = append(entities, entity)
				byID[entity.ID] = entity
			}
		}
	}
	var aliases []types.KnowledgeEntityAlias
	if err := s.db.WithContext(ctx).Select("entity_id", "normalized_alias").Where("tenant_id = ?", scope.tenantID).Find(&aliases).Error; err != nil {
		return nil, err
	}
	for _, plans := range resolvedPlans {
		for _, entity := range plans {
			for _, name := range entity.Aliases {
				aliases = append(aliases, types.KnowledgeEntityAlias{EntityID: entity.ID, NormalizedAlias: normalizeContextName(name)})
			}
		}
	}
	canonicalID := func(id string) string {
		seen := map[string]bool{}
		for id != "" && !seen[id] {
			seen[id] = true
			entity, exists := byID[id]
			if !exists {
				return ""
			}
			if entity.MergedIntoID == "" {
				return id
			}
			id = entity.MergedIntoID
		}
		return ""
	}
	tracked := map[string]*types.KnowledgeFocusEntity{}
	for _, workflow := range workflows {
		for _, step := range workflow.Steps {
			if step.Type != types.WorkflowStepFactQuery {
				continue
			}
			cfg, err := decodeWorkflowConfig[workflowFactConfig](step.Config)
			if err != nil {
				return nil, err
			}
			if cfg.EntityID == "" && cfg.ObjectEntityID == "" && strings.TrimSpace(cfg.Entity) == "" {
				continue
			}
			queryScope, err := s.resolveContextQueryScope(ctx, workflow.KnowledgeBaseID, types.KnowledgeContextQuery{Scope: cfg.Scope, DomainID: cfg.DomainID, KnowledgeBaseID: cfg.KnowledgeBaseID})
			if err != nil {
				// A removed query target no longer defines intake focus.
				var appErr *werrors.AppError
				if errors.As(err, &appErr) && appErr.Code == werrors.ErrNotFound {
					continue
				}
				return nil, err
			}
			kbIDs := intersectFocusIDs(scope.kbIDs, queryScope.kbIDs)
			if len(kbIDs) == 0 {
				continue
			}
			ids := []string{canonicalID(cfg.EntityID), canonicalID(cfg.ObjectEntityID)}
			if cfg.EntityID == "" && strings.TrimSpace(cfg.Entity) != "" {
				name := normalizeContextName(cfg.Entity)
				for _, entity := range entities {
					if entity.MergedIntoID == "" && strings.Contains(entity.NormalizedName, name) {
						ids = append(ids, entity.ID)
					}
				}
				for _, alias := range aliases {
					if strings.Contains(alias.NormalizedAlias, name) {
						ids = append(ids, canonicalID(alias.EntityID))
					}
				}
			}
			for _, id := range uniqueContextIDs(ids) {
				if id == "" {
					continue
				}
				item := tracked[id]
				if item == nil {
					item = &types.KnowledgeFocusEntity{KnowledgeEntity: byID[id]}
					tracked[id] = item
				}
				item.WorkflowIDs = uniqueContextIDs(append(item.WorkflowIDs, workflow.ID))
				item.KnowledgeBaseIDs = uniqueContextIDs(append(item.KnowledgeBaseIDs, kbIDs...))
			}
		}
	}
	for _, item := range tracked {
		sort.Strings(item.WorkflowIDs)
		sort.Strings(item.KnowledgeBaseIDs)
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CanonicalName != result[j].CanonicalName {
			return result[i].CanonicalName < result[j].CanonicalName
		}
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func intersectFocusIDs(left, right []string) []string {
	selected := map[string]bool{}
	for _, id := range left {
		selected[id] = true
	}
	result := []string{}
	for _, id := range right {
		if selected[id] {
			result = append(result, id)
		}
	}
	return uniqueContextIDs(result)
}

// MatchesFocus runs after P3 object resolution. It reads current stars and
// workflow definitions without making any additional model calls.
func (s *KnowledgeContextService) MatchesFocus(ctx context.Context, k *types.Knowledge, entityIDs []string, resolvedPlans ...[]types.KnowledgeEntity) (bool, error) {
	tenantID, err := s.scopedKnowledgeBase(ctx, k.KnowledgeBaseID)
	if err != nil {
		return false, err
	}
	if tenantID != k.TenantID {
		return false, werrors.NewForbiddenError("message is outside this team")
	}
	var count int64
	if len(entityIDs) > 0 {
		if err := s.db.WithContext(ctx).Model(&types.KnowledgeEntityStar{}).Where("tenant_id = ? AND entity_id IN ?", tenantID, entityIDs).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	topics := s.db.WithContext(ctx).Model(&types.KnowledgeTag{}).
		Joins("JOIN knowledge_context_topic_stars stars ON stars.tenant_id = knowledge_tags.tenant_id AND stars.tag_id = knowledge_tags.id").
		Where("knowledge_tags.tenant_id = ? AND knowledge_tags.knowledge_base_id = ? AND knowledge_tags.deleted_at IS NULL AND knowledge_tags.kind IN ?", tenantID, k.KnowledgeBaseID,
			[]string{types.TagKindTopic, types.TagKindObject, types.TagKindEvent})
	var starredTopics []types.KnowledgeTag
	if err := topics.Select("knowledge_tags.*").Find(&starredTopics).Error; err != nil {
		return false, err
	}
	if len(starredTopics) > 0 {
		ids := []string{}
		for _, tag := range starredTopics {
			ids = append(ids, tag.ID)
		}
		if err := s.db.WithContext(ctx).Model(&types.KnowledgeTagRelation{}).Where("knowledge_id = ? AND tag_id IN ?", k.ID, ids).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
		// Intake classification is already accepted before the tagging worker
		// materializes its relations. Explicit user tag decisions take priority.
		intake, err := k.IntakeMetadata()
		if err != nil {
			return false, err
		}
		if intake != nil {
			var decisions []types.KnowledgeTagManualDecision
			if err := s.db.WithContext(ctx).Where("knowledge_id = ? AND tag_id IN ?", k.ID, ids).Find(&decisions).Error; err != nil {
				return false, err
			}
			excluded := map[string]bool{}
			for _, decision := range decisions {
				if !decision.Selected {
					excluded[decision.TagID] = true
				}
			}
			for _, label := range intake.Classification {
				for _, tag := range starredTopics {
					if !excluded[tag.ID] && label.Kind == tag.Kind && label.Name == tag.Name {
						return true, nil
					}
				}
			}
		}
	}
	if len(entityIDs) == 0 {
		return false, nil
	}
	scope, err := s.resolveContextQueryScope(ctx, k.KnowledgeBaseID, types.KnowledgeContextQuery{})
	if err != nil {
		return false, err
	}
	tracked, err := s.contextTrackedEntities(ctx, scope, resolvedPlans...)
	if err != nil {
		return false, err
	}
	matched := map[string]bool{}
	for _, id := range entityIDs {
		matched[id] = true
	}
	for _, item := range tracked {
		if matched[item.ID] {
			return true, nil
		}
	}
	return false, nil
}

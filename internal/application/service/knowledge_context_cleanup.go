package service

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"reliquary/internal/types"
)

// DeleteMessages removes derived context only when the owning messages are
// explicitly deleted. Source updates must use SupersedeMessage so citations
// and historical evidence remain available.
func (s *KnowledgeContextService) DeleteMessages(ctx context.Context, kbID string, knowledgeIDs []string) error {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, knowledgeID := range uniqueContextIDs(knowledgeIDs) {
			if err := s.purgeKnowledgeTx(tx, tenantID, kbID, knowledgeID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *KnowledgeContextService) purgeKnowledgeTx(tx *gorm.DB, tenantID uint64, kbID, knowledgeID string) error {
	if knowledgeID == "" {
		return errors.New("knowledge ID is required")
	}
	var evidence []types.KnowledgeEvidence
	if err := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND knowledge_id = ?", tenantID, kbID, knowledgeID).Find(&evidence).Error; err != nil {
		return err
	}
	if len(evidence) == 0 {
		return nil
	}
	evidenceIDs := make([]string, 0, len(evidence))
	for _, item := range evidence {
		evidenceIDs = append(evidenceIDs, item.ID)
	}
	var eventIDs []string
	if err := tx.Model(&types.KnowledgeEventEvidence{}).Where("tenant_id = ? AND knowledge_base_id = ? AND evidence_id IN ?", tenantID, kbID, evidenceIDs).Distinct().Pluck("event_id", &eventIDs).Error; err != nil {
		return err
	}
	for _, model := range []any{&types.KnowledgeAttention{}, &types.KnowledgeEventEvidence{}, &types.KnowledgeFact{}, &types.KnowledgeEvidence{}} {
		column := "evidence_id"
		if _, ok := model.(*types.KnowledgeEvidence); ok {
			column = "id"
		}
		if err := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND "+column+" IN ?", tenantID, kbID, evidenceIDs).Delete(model).Error; err != nil {
			return err
		}
	}
	for _, eventID := range eventIDs {
		var event types.KnowledgeEvent
		if err := tx.Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?", eventID, tenantID, kbID).First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		var count int64
		if err := tx.Model(&types.KnowledgeEventEvidence{}).Where("tenant_id = ? AND knowledge_base_id = ? AND event_id = ?", tenantID, kbID, eventID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			if err := clearContextEventRelationsTo(tx, tenantID, kbID, []string{eventID}); err != nil {
				return err
			}
			if err := tx.Delete(&event).Error; err != nil {
				return err
			}
		} else if err := updateContextEventHeat(tx, tenantID, kbID, &event); err != nil {
			return err
		}
	}
	return s.pruneContextEntities(tx, tenantID, kbID)
}

// DeleteKnowledgeBase removes all derived context before an explicitly deleted
// knowledge base is removed from the source tables.
func (s *KnowledgeContextService) DeleteKnowledgeBase(ctx context.Context, kbID string) error {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return err
	}
	if kbID == "" {
		return errors.New("knowledge base ID is required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, model := range []any{&types.KnowledgeAttention{}, &types.KnowledgeEventEvidence{}, &types.KnowledgeEvent{},
			&types.KnowledgeFact{}, &types.KnowledgeEvidence{}, &types.KnowledgeEntityAlias{}, &types.KnowledgeEntity{}} {
			if err := tx.Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).Delete(model).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *KnowledgeContextService) pruneContextEntities(tx *gorm.DB, tenantID uint64, kbID string) error {
	used := make(map[string]bool)
	var subjectIDs, objectIDs []string
	if err := tx.Model(&types.KnowledgeFact{}).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).Pluck("subject_entity_id", &subjectIDs).Error; err != nil {
		return err
	}
	if err := tx.Model(&types.KnowledgeFact{}).Where("tenant_id = ? AND knowledge_base_id = ? AND object_entity_id <> ''", tenantID, kbID).Pluck("object_entity_id", &objectIDs).Error; err != nil {
		return err
	}
	for _, id := range append(subjectIDs, objectIDs...) {
		used[id] = true
	}
	var events []types.KnowledgeEvent
	if err := tx.Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).Find(&events).Error; err != nil {
		return err
	}
	for _, event := range events {
		for _, id := range event.EntityIDs {
			used[id] = true
		}
	}
	var entities []types.KnowledgeEntity
	if err := tx.Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).Find(&entities).Error; err != nil {
		return err
	}
	for _, entity := range entities {
		if used[entity.ID] {
			continue
		}
		if err := tx.Where("tenant_id = ? AND knowledge_base_id = ? AND entity_id = ?", tenantID, kbID, entity.ID).Delete(&types.KnowledgeEntityAlias{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&entity).Error; err != nil {
			return err
		}
	}
	return nil
}

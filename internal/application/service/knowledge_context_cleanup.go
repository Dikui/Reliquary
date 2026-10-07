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
		if err := lockContextTeam(tx, tenantID); err != nil {
			return err
		}
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
		if err := tx.Where("id = ? AND tenant_id = ?", eventID, tenantID).First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}
		var count int64
		if err := tx.Model(&types.KnowledgeEventEvidence{}).Where("tenant_id = ? AND event_id = ?", tenantID, eventID).Count(&count).Error; err != nil {
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
		if err := lockContextTeam(tx, tenantID); err != nil {
			return err
		}
		var eventIDs []string
		if err := tx.Model(&types.KnowledgeEventEvidence{}).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).Distinct().Pluck("event_id", &eventIDs).Error; err != nil {
			return err
		}
		var sourceEventIDs []string
		if err := tx.Model(&types.KnowledgeEvent{}).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).Pluck("id", &sourceEventIDs).Error; err != nil {
			return err
		}
		eventIDs = uniqueContextIDs(append(eventIDs, sourceEventIDs...))
		for _, model := range []any{&types.KnowledgeAttention{}, &types.KnowledgeEventEvidence{}, &types.KnowledgeFact{}, &types.KnowledgeEvidence{}} {
			if err := tx.Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).Delete(model).Error; err != nil {
				return err
			}
		}
		for _, eventID := range eventIDs {
			var event types.KnowledgeEvent
			if err := tx.Where("id = ? AND tenant_id = ?", eventID, tenantID).First(&event).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}
			var links []types.KnowledgeEventEvidence
			if err := tx.Where("tenant_id = ? AND event_id = ?", tenantID, eventID).Order("created_at ASC, id ASC").Find(&links).Error; err != nil {
				return err
			}
			if len(links) == 0 {
				if err := clearContextEventRelationsTo(tx, tenantID, kbID, []string{eventID}); err != nil {
					return err
				}
				if err := tx.Delete(&event).Error; err != nil {
					return err
				}
			} else {
				if event.KnowledgeBaseID == kbID {
					if err := tx.Model(&event).Update("knowledge_base_id", links[0].KnowledgeBaseID).Error; err != nil {
						return err
					}
				}
				if err := updateContextEventHeat(tx, tenantID, kbID, &event); err != nil {
					return err
				}
			}
		}
		// First-source metadata does not own a global identity.
		for _, model := range []any{&types.KnowledgeEntity{}, &types.KnowledgeEntityAlias{}} {
			if err := tx.Model(model).Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).Update("knowledge_base_id", nil).Error; err != nil {
				return err
			}
		}
		return s.pruneContextEntities(tx, tenantID, kbID)
	})
}

func (s *KnowledgeContextService) pruneContextEntities(tx *gorm.DB, tenantID uint64, kbID string) error {
	if err := lockContextTeam(tx, tenantID); err != nil {
		return err
	}
	used := make(map[string]bool)
	var facts []types.KnowledgeFact
	// Both current and historical evidence preserve an identity.
	evidenceIDs := tx.Model(&types.KnowledgeEvidence{}).Select("id").Where("tenant_id = ?", tenantID)
	if err := tx.Where("tenant_id = ? AND evidence_id IN (?)", tenantID, evidenceIDs).Find(&facts).Error; err != nil {
		return err
	}
	for _, fact := range facts {
		used[fact.SubjectEntityID] = true
		used[fact.ObjectEntityID] = true
	}
	var links []types.KnowledgeEventEvidence
	if err := tx.Where("tenant_id = ? AND evidence_id IN (?)", tenantID, evidenceIDs).Find(&links).Error; err != nil {
		return err
	}
	for _, link := range links {
		for _, id := range link.EntityIDs {
			used[id] = true
		}
	}
	var entities []types.KnowledgeEntity
	if err := tx.Where("tenant_id = ?", tenantID).Find(&entities).Error; err != nil {
		return err
	}
	for _, entity := range entities {
		if used[entity.ID] {
			continue
		}
		if err := tx.Where("tenant_id = ? AND entity_id = ?", tenantID, entity.ID).Delete(&types.KnowledgeEntityAlias{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&entity).Error; err != nil {
			return err
		}
	}
	return nil
}

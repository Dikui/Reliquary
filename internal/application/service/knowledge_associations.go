package service

import (
	"context"

	"reliquary/internal/types"
)

// loadKnowledgeAssociations batches entity and topic display data for article responses.
func (s *knowledgeService) loadKnowledgeAssociations(ctx context.Context, tenantID uint64, articles []*types.Knowledge) error {
	byID := make(map[string]*types.Knowledge, len(articles))
	ids := make([]string, 0, len(articles))
	for _, article := range articles {
		if article == nil {
			continue
		}
		article.Entities = []types.Entity{}
		article.Topics = []types.Topic{}
		byID[article.ID] = article
		ids = append(ids, article.ID)
	}
	if s.db == nil || len(ids) == 0 {
		return nil
	}
	var entityRows []struct {
		KnowledgeID string       `gorm:"column:knowledge_id"`
		Entity      types.Entity `gorm:"embedded"`
	}
	if err := s.db.WithContext(ctx).Table("knowledge_entities AS ke").
		Select("ke.knowledge_id, e.*").Joins("JOIN entities AS e ON e.tenant_id=ke.tenant_id AND e.id=ke.entity_id").
		Where("ke.tenant_id=? AND ke.knowledge_id IN ? AND e.merged_into_id=''", tenantID, ids).
		Order("e.canonical_name,e.id").Find(&entityRows).Error; err != nil {
		return err
	}
	for _, row := range entityRows {
		if article := byID[row.KnowledgeID]; article != nil {
			article.Entities = append(article.Entities, row.Entity)
		}
	}
	var topicRows []struct {
		KnowledgeID string      `gorm:"column:knowledge_id"`
		Topic       types.Topic `gorm:"embedded"`
	}
	if err := s.db.WithContext(ctx).Table("knowledge_topics AS kt").
		Select("kt.knowledge_id, t.*").Joins("JOIN topics AS t ON t.tenant_id=kt.tenant_id AND t.id=kt.topic_id").
		Where("kt.tenant_id=? AND kt.knowledge_id IN ?", tenantID, ids).
		Order("t.name,t.id").Find(&topicRows).Error; err != nil {
		return err
	}
	for _, row := range topicRows {
		if article := byID[row.KnowledgeID]; article != nil {
			article.Topics = append(article.Topics, row.Topic)
		}
	}
	return nil
}

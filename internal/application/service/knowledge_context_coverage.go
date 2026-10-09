package service

import (
	"context"

	"reliquary/internal/types"
)

const contextEventsCoverageNotice = "只包含收录时命中关注范围的消息"

// Coverage uses the latest evidence even when assertions include history.
// Unextracted values cannot establish whether a source meets a field filter.
func (s *KnowledgeContextService) templateOnlyMessageCount(ctx context.Context, scope *contextQueryScope, q types.KnowledgeContextQuery, entityIDs []string) (int, error) {
	query := s.db.WithContext(ctx).Model(&types.KnowledgeEvidence{}).
		Where("tenant_id = ? AND knowledge_base_id IN ? AND extraction_mode = ? AND status = ?", scope.tenantID, scope.kbIDs, types.KnowledgeExtractionTemplateOnly, types.KnowledgeEvidenceCurrent).
		Where("version = (SELECT MAX(newer.version) FROM knowledge_context_evidence newer WHERE newer.tenant_id = knowledge_context_evidence.tenant_id AND newer.knowledge_id = knowledge_context_evidence.knowledge_id)")
	if len(q.KnowledgeIDs) > 0 {
		query = query.Where("knowledge_id IN ?", uniqueContextIDs(q.KnowledgeIDs))
	}
	if q.Entity != "" && len(entityIDs) == 0 {
		return 0, nil
	}
	if q.EventID != "" {
		// Only objects supported by visible sources of the selected event qualify.
		eventEntities, err := s.contextCoverageEventEntities(ctx, scope, q)
		if err != nil {
			return 0, err
		}
		if len(eventEntities) == 0 {
			return 0, nil
		}
		query = query.Where("id IN (?)", s.db.Model(&types.KnowledgeEvidenceEntity{}).Select("evidence_id").Where("tenant_id = ? AND entity_id IN ?", scope.tenantID, eventEntities))
	}
	if len(entityIDs) > 0 {
		links := s.db.Model(&types.KnowledgeEvidenceEntity{}).Select("evidence_id").Where("tenant_id = ? AND entity_id IN ?", scope.tenantID, entityIDs)
		query = query.Where("id IN (?)", links)
	}
	if q.ObjectEntityID != "" {
		query = query.Where("id IN (?)", s.db.Model(&types.KnowledgeEvidenceEntity{}).Select("evidence_id").Where("tenant_id = ? AND entity_id = ?", scope.tenantID, q.ObjectEntityID))
	}
	var evidence []types.KnowledgeEvidence
	if err := query.Find(&evidence).Error; err != nil {
		return 0, err
	}
	evidence, err := s.visibleContextEvidence(ctx, scope.tenantID, "", evidence, false)
	if err != nil {
		return 0, err
	}
	messages := map[string]bool{}
	for _, source := range evidence {
		// A source's publication date scopes coverage; missing dates are excluded
		// from a time-bounded count, without guessing unextracted fact/event dates.
		if contextFactInTimeRange(types.KnowledgeFact{}, source, q) {
			messages[source.KnowledgeID] = true
		}
	}
	return len(messages), nil
}

func (s *KnowledgeContextService) contextCoverageEventEntities(ctx context.Context, scope *contextQueryScope, q types.KnowledgeContextQuery) ([]string, error) {
	linksQuery := s.db.WithContext(ctx).Model(&types.KnowledgeEventEvidence{}).Select("evidence_id").
		Where("tenant_id = ? AND knowledge_base_id IN ? AND event_id = ?", scope.tenantID, scope.kbIDs, q.EventID)
	sourcesQuery := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id IN ? AND id IN (?)", scope.tenantID, scope.kbIDs, linksQuery)
	if !q.IncludeHistory {
		sourcesQuery = sourcesQuery.Where("status = ?", types.KnowledgeEvidenceCurrent)
	}
	if len(q.KnowledgeIDs) > 0 {
		sourcesQuery = sourcesQuery.Where("knowledge_id IN ?", uniqueContextIDs(q.KnowledgeIDs))
	}
	var sources []types.KnowledgeEvidence
	if err := sourcesQuery.Find(&sources).Error; err != nil {
		return nil, err
	}
	sources, err := s.visibleContextEvidence(ctx, scope.tenantID, "", sources, q.IncludeHistory)
	if err != nil || len(sources) == 0 {
		return nil, err
	}
	allowed := map[string]types.KnowledgeEvidence{}
	for _, source := range sources {
		allowed[source.ID] = source
	}
	var links []types.KnowledgeEventEvidence
	if err := s.db.WithContext(ctx).Where("tenant_id = ? AND event_id = ? AND evidence_id IN ?", scope.tenantID, q.EventID, sortedEvidenceIDs(allowed)).Find(&links).Error; err != nil {
		return nil, err
	}
	event := types.KnowledgeEvent{OccurredAt: contextVisibleEventOccurredAt(links, allowed)}
	if !contextEventInTimeRange(event, sources, q) {
		return nil, nil
	}
	var entityIDs []string
	err = s.db.WithContext(ctx).Model(&types.KnowledgeEventEvidenceEntity{}).
		Where("tenant_id = ? AND event_id = ? AND evidence_id IN ?", scope.tenantID, q.EventID, sortedEvidenceIDs(allowed)).
		Distinct().Pluck("entity_id", &entityIDs).Error
	return entityIDs, err
}

// Pure object mentions remain queryable even when no template fact was found.
func (s *KnowledgeContextService) contextEvidenceObjects(ctx context.Context, tenantID uint64, evidence []types.KnowledgeEvidence, q types.KnowledgeContextQuery, candidates []string) ([]types.KnowledgeEvidenceEntity, error) {
	if q.Predicate != "" || q.Relation != "" || q.ObjectEntityID != "" || q.EventID != "" {
		return nil, nil
	}
	var evidenceIDs []string
	for _, source := range evidence {
		if contextFactInTimeRange(types.KnowledgeFact{}, source, q) {
			evidenceIDs = append(evidenceIDs, source.ID)
		}
	}
	if len(evidenceIDs) == 0 {
		return nil, nil
	}
	query := s.db.WithContext(ctx).Where("tenant_id = ? AND evidence_id IN ?", tenantID, evidenceIDs)
	if len(candidates) > 0 {
		query = query.Where("entity_id IN ?", candidates)
	}
	var links []types.KnowledgeEvidenceEntity
	if err := query.Find(&links).Error; err != nil {
		return nil, err
	}
	return links, nil
}

package service

import (
	"context"
	"sort"
	"strings"

	"gorm.io/gorm"
	"reliquary/internal/types"
)

// projectVisibleEventRelations adds source-backed classifications. Directed
// same-story links require visible evidence at both ends. Destination titles
// come from a visible event-evidence summary, never the aggregate event.
func (s *KnowledgeContextService) projectVisibleEventRelations(
	ctx context.Context,
	tenantID uint64,
	kbIDs []string,
	events []types.KnowledgeEvent,
	visibleEvidence map[string]types.KnowledgeEvidence,
	q types.KnowledgeContextQuery,
) error {
	if len(events) == 0 || len(visibleEvidence) == 0 {
		return nil
	}

	eventByID := make(map[string]int, len(events))
	eventIDs := make([]string, 0, len(events))
	for i := range events {
		events[i].Relations = nil
		eventByID[events[i].ID] = i
		eventIDs = append(eventIDs, events[i].ID)
	}
	visibleEvidenceIDs := sortedEvidenceIDs(visibleEvidence)
	var sourceLinks []types.KnowledgeEventEvidence
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id IN ? AND event_id IN ? AND evidence_id IN ? AND relation IN ?",
			tenantID, kbIDs, eventIDs, visibleEvidenceIDs, []string{
				types.KnowledgeEventRelationSameOccurrence, types.KnowledgeEventRelationSameStory,
				types.KnowledgeEventRelationUnrelated,
			}).
		Order("created_at ASC, id ASC").Find(&sourceLinks).Error; err != nil {
		return err
	}
	if len(sourceLinks) == 0 {
		return nil
	}

	relatedIDs := make([]string, 0, len(sourceLinks))
	relatedSeen := make(map[string]bool, len(sourceLinks))
	for _, link := range sourceLinks {
		index, ok := eventByID[link.EventID]
		if !ok || link.Relation != types.KnowledgeEventRelationSameStory || link.RelatedEventID == "" ||
			link.RelatedEventID == link.EventID || !contextStringContains(events[index].EvidenceIDs, link.EvidenceID) {
			continue
		}
		if !relatedSeen[link.RelatedEventID] {
			relatedSeen[link.RelatedEventID] = true
			relatedIDs = append(relatedIDs, link.RelatedEventID)
		}
	}
	if len(relatedIDs) == 0 {
		return nil
	}

	permittedEvidenceQuery := s.db.WithContext(ctx).Model(&types.KnowledgeEvidence{}).
		Select("id").Where("tenant_id = ? AND knowledge_base_id IN ?", tenantID, kbIDs)
	if !q.IncludeHistory {
		permittedEvidenceQuery = permittedEvidenceQuery.Where("status = ?", types.KnowledgeEvidenceCurrent)
	}
	if knowledgeIDs := uniqueContextIDs(q.KnowledgeIDs); len(knowledgeIDs) > 0 {
		permittedEvidenceQuery = permittedEvidenceQuery.Where("knowledge_id IN ?", knowledgeIDs)
	}
	var targetLinks []types.KnowledgeEventEvidence
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id IN ? AND event_id IN ? AND evidence_id IN (?)",
			tenantID, kbIDs, relatedIDs, permittedEvidenceQuery).
		Order("created_at DESC, id DESC").Find(&targetLinks).Error; err != nil {
		return err
	}
	if len(targetLinks) == 0 {
		return nil
	}
	targetEvidenceIDs := make([]string, 0, len(targetLinks))
	targetEvidenceSeen := make(map[string]bool, len(targetLinks))
	for _, link := range targetLinks {
		if !targetEvidenceSeen[link.EvidenceID] {
			targetEvidenceSeen[link.EvidenceID] = true
			targetEvidenceIDs = append(targetEvidenceIDs, link.EvidenceID)
		}
	}
	var targetEvidence []types.KnowledgeEvidence
	if err := s.db.WithContext(ctx).
		Select("id", "knowledge_id", "title", "published_at", "status").
		Where("tenant_id = ? AND knowledge_base_id IN ? AND id IN ?", tenantID, kbIDs, targetEvidenceIDs).
		Find(&targetEvidence).Error; err != nil {
		return err
	}
	visibleTargets, err := s.visibleContextEvidence(ctx, tenantID, "", targetEvidence, q.IncludeHistory)
	if err != nil {
		return err
	}
	visibleTargetByID := make(map[string]types.KnowledgeEvidence, len(visibleTargets))
	for _, evidence := range visibleTargets {
		visibleTargetByID[evidence.ID] = evidence
	}

	bestTargetLink := make(map[string]types.KnowledgeEventEvidence)
	targetEvidenceByID := make(map[string]types.KnowledgeEvidence, len(visibleTargets))
	for _, evidence := range visibleTargets {
		targetEvidenceByID[evidence.ID] = evidence
	}
	for _, link := range targetLinks {
		if _, ok := visibleTargetByID[link.EvidenceID]; !ok {
			continue
		}
		selected, ok := bestTargetLink[link.EventID]
		if !ok || contextEventLinkNewer(link, selected, targetEvidenceByID) {
			bestTargetLink[link.EventID] = link
		}
	}
	targetTitles := make(map[string]string, len(bestTargetLink))
	for eventID, link := range bestTargetLink {
		title := strings.TrimSpace(link.SourceTitle)
		if title == "" {
			title = strings.TrimSpace(targetEvidenceByID[link.EvidenceID].Title)
		}
		targetTitles[eventID] = title
	}

	for _, link := range sourceLinks {
		index, sourceVisible := eventByID[link.EventID]
		if !sourceVisible || !contextStringContains(events[index].EvidenceIDs, link.EvidenceID) {
			continue
		}
		relation := types.KnowledgeEventRelation{Relation: link.Relation, EvidenceID: link.EvidenceID}
		if link.Relation == types.KnowledgeEventRelationSameStory {
			if link.RelatedEventID == "" || link.RelatedEventID == link.EventID {
				continue
			}
			if _, targetVisible := bestTargetLink[link.RelatedEventID]; !targetVisible {
				continue
			}
			relation.RelatedEventID = link.RelatedEventID
			relation.RelatedTitle = targetTitles[link.RelatedEventID]
		} else if link.Relation != types.KnowledgeEventRelationSameOccurrence && link.Relation != types.KnowledgeEventRelationUnrelated {
			continue
		}
		events[index].Relations = append(events[index].Relations, relation)
	}
	for i := range events {
		sort.Slice(events[i].Relations, func(a, b int) bool {
			if events[i].Relations[a].RelatedEventID == events[i].Relations[b].RelatedEventID {
				return events[i].Relations[a].EvidenceID < events[i].Relations[b].EvidenceID
			}
			return events[i].Relations[a].RelatedEventID < events[i].Relations[b].RelatedEventID
		})
	}
	return nil
}

func contextStringContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// clearContextEventRelationsTo removes target references before an event is
// deleted. The evidence itself remains attached to its source event.
func clearContextEventRelationsTo(tx *gorm.DB, tenantID uint64, kbID string, eventIDs []string) error {
	eventIDs = uniqueContextIDs(eventIDs)
	if len(eventIDs) == 0 {
		return nil
	}
	return tx.Model(&types.KnowledgeEventEvidence{}).
		Where("tenant_id = ? AND related_event_id IN ?", tenantID, eventIDs).
		Updates(map[string]any{"relation": "", "related_event_id": ""}).Error
}

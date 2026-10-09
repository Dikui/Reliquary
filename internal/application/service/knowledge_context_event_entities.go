package service

import (
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/types"
)

// Load only the requested events; EntityIDs is an API projection, not a column.
func loadContextEventEntityIDs(db *gorm.DB, tenantID uint64, events []types.KnowledgeEvent) error {
	if len(events) == 0 {
		return nil
	}
	ids := make([]string, 0, len(events))
	for i := range events {
		events[i].EntityIDs = types.StringArray{}
		ids = append(ids, events[i].ID)
	}
	var rows []types.KnowledgeEventEntity
	if err := db.Where("tenant_id = ? AND event_id IN ?", tenantID, ids).Order("event_id, entity_id").Find(&rows).Error; err != nil {
		return err
	}
	byEvent := make(map[string]types.StringArray, len(events))
	for _, row := range rows {
		byEvent[row.EventID] = append(byEvent[row.EventID], row.EntityID)
	}
	for i := range events {
		if values := byEvent[events[i].ID]; values != nil {
			events[i].EntityIDs = values
		}
	}
	return nil
}

// Preserve each source's contribution when projecting a query's visible scope.
func loadContextEventEvidenceEntityIDs(db *gorm.DB, tenantID uint64, links []types.KnowledgeEventEvidence) error {
	if len(links) == 0 {
		return nil
	}
	type pair struct{ event, evidence string }
	byPair := make(map[pair][]int, len(links))
	pairs := make([][]any, 0, len(links))
	for i := range links {
		links[i].EntityIDs = types.StringArray{}
		key := pair{links[i].EventID, links[i].EvidenceID}
		if _, ok := byPair[key]; !ok {
			pairs = append(pairs, []any{key.event, key.evidence})
		}
		byPair[key] = append(byPair[key], i)
	}
	var rows []types.KnowledgeEventEvidenceEntity
	if err := db.Where("tenant_id = ? AND (event_id, evidence_id) IN ?", tenantID, pairs).
		Order("event_id, evidence_id, entity_id").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		for _, i := range byPair[pair{row.EventID, row.EvidenceID}] {
			links[i].EntityIDs = append(links[i].EntityIDs, row.EntityID)
		}
	}
	return nil
}

func attachContextEventEntities(tx *gorm.DB, tenantID uint64, eventID, evidenceID string, entityIDs types.StringArray) error {
	ids := uniqueContextIDs(entityIDs)
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil
	}
	events := make([]types.KnowledgeEventEntity, 0, len(ids))
	sources := make([]types.KnowledgeEventEvidenceEntity, 0, len(ids))
	for _, id := range ids {
		events = append(events, types.KnowledgeEventEntity{TenantID: tenantID, EventID: eventID, EntityID: id})
		sources = append(sources, types.KnowledgeEventEvidenceEntity{TenantID: tenantID, EventID: eventID, EvidenceID: evidenceID, EntityID: id})
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&events).Error; err != nil {
		return err
	}
	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&sources).Error
}

// Source deletion holds the space's exclusive maintenance lock. A pair is
// retracted only after its final supporting source is gone.
func pruneContextEventEntities(tx *gorm.DB, tenantID uint64, eventID string) error {
	return tx.Exec(`DELETE FROM knowledge_context_event_entities AS objects
WHERE objects.tenant_id = ? AND objects.event_id = ? AND NOT EXISTS (
    SELECT 1 FROM knowledge_context_event_evidence_entities AS support
    WHERE support.tenant_id = objects.tenant_id AND support.event_id = objects.event_id
      AND support.entity_id = objects.entity_id
)`, tenantID, eventID).Error
}

func mergeContextEventEntities(tx *gorm.DB, tenantID uint64, keepID, mergeID string) error {
	if err := tx.Exec(`INSERT INTO knowledge_context_event_entities (tenant_id, event_id, entity_id)
SELECT tenant_id, event_id, ? FROM knowledge_context_event_entities
WHERE tenant_id = ? AND entity_id = ? ON CONFLICT DO NOTHING`, keepID, tenantID, mergeID).Error; err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO knowledge_context_event_evidence_entities (tenant_id, event_id, evidence_id, entity_id)
SELECT tenant_id, event_id, evidence_id, ? FROM knowledge_context_event_evidence_entities
WHERE tenant_id = ? AND entity_id = ? ON CONFLICT DO NOTHING`, keepID, tenantID, mergeID).Error; err != nil {
		return err
	}
	if err := tx.Where("tenant_id = ? AND entity_id = ?", tenantID, mergeID).Delete(&types.KnowledgeEventEvidenceEntity{}).Error; err != nil {
		return err
	}
	return tx.Where("tenant_id = ? AND entity_id = ?", tenantID, mergeID).Delete(&types.KnowledgeEventEntity{}).Error
}

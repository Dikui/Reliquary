package service

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// contextQueryCandidates reads identifiers and object names, not source bodies.
// Event candidates expand to every source of that event, preserving aggregates.
func (s *KnowledgeContextService) contextQueryCandidates(ctx context.Context, scope *contextQueryScope, q types.KnowledgeContextQuery) ([]string, []string, bool, error) {
	tenantID, kbIDs := scope.tenantID, scope.kbIDs
	if q.Entity == "" && q.EntityID == "" && q.EventID == "" && q.Predicate == "" && q.Relation == "" {
		return nil, nil, false, nil
	}
	var entityIDs []string
	if q.EntityID != "" {
		entityIDs = []string{q.EntityID}
	} else if q.Entity != "" {
		var entities []types.KnowledgeEntity
		if err := s.db.WithContext(ctx).Select("id", "canonical_name").Where("tenant_id = ? AND merged_into_id = ''", tenantID).Find(&entities).Error; err != nil {
			return nil, nil, true, err
		}
		matched := map[string]bool{}
		name := normalizeContextName(q.Entity)
		for _, entity := range entities {
			if strings.Contains(normalizeContextName(entity.CanonicalName), name) {
				matched[entity.ID] = true
			}
		}
		var aliases []types.KnowledgeEntityAlias
		if err := s.db.WithContext(ctx).Select("entity_id", "alias").Where("tenant_id = ?", tenantID).Find(&aliases).Error; err != nil {
			return nil, nil, true, err
		}
		limitedAliases := len(q.KnowledgeIDs) > 0
		var aliasEvidence []types.KnowledgeEvidence
		if limitedAliases {
			var err error
			aliasEvidence, err = s.listEvidenceCandidates(ctx, "", q)
			if err != nil {
				return nil, nil, true, err
			}
		}
		for _, alias := range aliases {
			if (!limitedAliases || contextAliasVisible(alias.Alias, aliasEvidence)) && strings.Contains(normalizeContextName(alias.Alias), name) {
				matched[alias.EntityID] = true
			}
		}
		for id := range matched {
			entityIDs = append(entityIDs, id)
		}
		sort.Strings(entityIDs)
		if len(entityIDs) == 0 {
			return nil, nil, true, nil
		}
	}
	var evidenceIDs []string
	if len(entityIDs) > 0 || q.Predicate != "" || q.Relation != "" {
		facts := s.db.WithContext(ctx).Model(&types.KnowledgeFact{}).Where("tenant_id = ? AND knowledge_base_id IN ?", tenantID, kbIDs)
		if len(entityIDs) > 0 {
			facts = facts.Where("(subject_entity_id IN ? OR object_entity_id IN ?)", entityIDs, entityIDs)
		}
		if !q.IncludeHistory {
			facts = facts.Where("status = ?", types.KnowledgeFactCurrent)
		}
		facts = scope.factFields(facts, q)
		if q.ObjectEntityID != "" {
			facts = facts.Where("object_entity_id = ?", q.ObjectEntityID)
		}
		if len(entityIDs) == 0 && q.EventID == "" {
			var values []types.KnowledgeFact
			if err := facts.Select("evidence_id", "knowledge_base_id", "kind", "value_type", "value", "unit").Find(&values).Error; err != nil {
				return nil, nil, true, err
			}
			for _, fact := range values {
				if contextFactMatchesValue(scope.domains[fact.KnowledgeBaseID], fact, q) {
					evidenceIDs = append(evidenceIDs, fact.EvidenceID)
				}
			}
			return uniqueContextIDs(evidenceIDs), nil, true, nil
		}
		if err := facts.Distinct("evidence_id").Pluck("evidence_id", &evidenceIDs).Error; err != nil {
			return nil, nil, true, err
		}
	}
	links := s.db.WithContext(ctx).Model(&types.KnowledgeEventEvidence{}).Where("tenant_id = ? AND knowledge_base_id IN ?", tenantID, kbIDs)
	if q.EventID != "" {
		links = links.Where("event_id = ?", q.EventID)
	} else {
		linkedEvents := s.db.WithContext(ctx).Model(&types.KnowledgeEventEvidenceEntity{}).
			Select("knowledge_context_event_evidence_entities.event_id").
			Joins("JOIN knowledge_context_event_evidence links ON links.tenant_id = knowledge_context_event_evidence_entities.tenant_id AND links.event_id = knowledge_context_event_evidence_entities.event_id AND links.evidence_id = knowledge_context_event_evidence_entities.evidence_id").
			Where("knowledge_context_event_evidence_entities.tenant_id = ? AND links.knowledge_base_id IN ? AND knowledge_context_event_evidence_entities.entity_id IN ?", tenantID, kbIDs, entityIDs)
		links = s.db.WithContext(ctx).Model(&types.KnowledgeEventEvidence{}).Where("tenant_id = ? AND knowledge_base_id IN ? AND event_id IN (?)", tenantID, kbIDs, linkedEvents)
	}
	var eventEvidenceIDs []string
	if err := links.Distinct("evidence_id").Pluck("evidence_id", &eventEvidenceIDs).Error; err != nil {
		return nil, nil, true, err
	}
	if q.EventID != "" {
		return uniqueContextIDs(eventEvidenceIDs), entityIDs, true, nil
	}
	if len(entityIDs) > 0 && q.Predicate == "" && q.Relation == "" && q.ObjectEntityID == "" {
		var objectEvidenceIDs []string
		if err := s.db.WithContext(ctx).Model(&types.KnowledgeEvidenceEntity{}).
			Joins("JOIN knowledge_context_evidence sources ON sources.tenant_id = knowledge_context_evidence_entities.tenant_id AND sources.id = knowledge_context_evidence_entities.evidence_id").
			Where("sources.tenant_id = ? AND sources.knowledge_base_id IN ? AND knowledge_context_evidence_entities.entity_id IN ?", tenantID, kbIDs, entityIDs).
			Distinct().Pluck("knowledge_context_evidence_entities.evidence_id", &objectEvidenceIDs).Error; err != nil {
			return nil, nil, true, err
		}
		evidenceIDs = append(evidenceIDs, objectEvidenceIDs...)
	}
	return uniqueContextIDs(append(evidenceIDs, eventEvidenceIDs...)), entityIDs, true, nil
}

func (s *KnowledgeContextService) requireTenant(ctx context.Context) (uint64, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return 0, werrors.NewUnauthorizedError("workspace required")
	}
	if s.db == nil {
		return 0, errors.New("knowledge context database is unavailable")
	}
	return tenantID, nil
}

func (s *KnowledgeContextService) scopedKnowledgeBase(ctx context.Context, kbID string) (uint64, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return 0, err
	}

	if strings.TrimSpace(kbID) == "" {
		return 0, werrors.NewBadRequestError("knowledge base ID is required")
	}
	var kb types.KnowledgeBase
	if err := s.db.WithContext(ctx).Select("id", "tenant_id").Where("id = ? AND tenant_id = ?", kbID, tenantID).First(&kb).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, werrors.NewNotFoundError("knowledge base not found")
		}
		return 0, err
	}
	return tenantID, nil
}

func (s *KnowledgeContextService) Query(ctx context.Context, kbID string, q types.KnowledgeContextQuery) (*types.KnowledgeContextResult, error) {
	return s.queryContext(ctx, kbID, q, true)
}

// EntityDetail includes team history by default, without list pagination.
func (s *KnowledgeContextService) EntityDetail(ctx context.Context, entityID string, history ...bool) (*types.KnowledgeContextResult, error) {
	_, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}

	includeHistory := true
	if len(history) > 0 {
		includeHistory = history[0]
	}
	result, err := s.queryContext(ctx, "", types.KnowledgeContextQuery{Scope: "team", EntityID: entityID, IncludeHistory: includeHistory}, false)
	if err != nil {
		return nil, err
	}
	if len(result.Entities) == 0 {
		// An identity remains visible when all its assertions belong to history.
		tenantID, err := s.requireTenant(ctx)
		if err != nil {
			return nil, err
		}
		var entity types.KnowledgeEntity
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id = ? AND merged_into_id = ''", tenantID, entityID).First(&entity).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, werrors.NewNotFoundError("knowledge entity not found")
			}
			return nil, err
		}
		var aliases []types.KnowledgeEntityAlias
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND entity_id = ?", tenantID, entityID).Order("normalized_alias ASC").Find(&aliases).Error; err != nil {
			return nil, err
		}

		for _, alias := range aliases {
			entity.Aliases = append(entity.Aliases, alias.Alias)
		}

		result.Entities = []types.KnowledgeEntity{entity}
	}
	return result, nil
}

func (s *KnowledgeContextService) queryContext(ctx context.Context, kbID string, q types.KnowledgeContextQuery, paginated bool) (*types.KnowledgeContextResult, error) {
	scope, err := s.resolveContextQueryScope(ctx, kbID, q)
	if err != nil {
		return nil, err
	}
	tenantID, kbIDs := scope.tenantID, scope.kbIDs
	limitedEntities := len(q.KnowledgeIDs) > 0
	projectEventSources := len(q.KnowledgeIDs) > 0 || scope.kind != "team"
	result := &types.KnowledgeContextResult{
		Entities: []types.KnowledgeEntity{}, Facts: []types.KnowledgeFact{},
		Events: []types.KnowledgeEvent{}, Evidence: []types.KnowledgeEvidence{}, Attention: []types.KnowledgeAttention{},
	}
	if q.MinValue != nil && (math.IsNaN(*q.MinValue) || math.IsInf(*q.MinValue, 0)) ||
		q.MaxValue != nil && (math.IsNaN(*q.MaxValue) || math.IsInf(*q.MaxValue, 0)) ||
		q.MinValue != nil && q.MaxValue != nil && *q.MinValue > *q.MaxValue {
		return nil, werrors.NewBadRequestError("invalid numeric range")
	}
	if q.Since != nil && q.Until != nil && q.Since.After(*q.Until) {
		return nil, werrors.NewBadRequestError("invalid time range")
	}
	if (q.MinValue != nil || q.MaxValue != nil) && strings.TrimSpace(q.Predicate) == "" {
		return nil, werrors.NewBadRequestError("numeric comparison requires an attribute predicate")
	}
	if len(kbIDs) == 0 {
		return result, nil
	}
	// Resolve names using small object records, then fetch bodies only for the
	// linked evidence. Authorization still runs before any result is returned.
	candidateEvidenceIDs, candidateEntityIDs, narrowed, err := s.contextQueryCandidates(ctx, scope, q)
	if err != nil {
		return nil, err
	}
	result.TemplateOnlyMessageCount, err = s.templateOnlyMessageCount(ctx, scope, q, candidateEntityIDs)
	if err != nil {
		return nil, err
	}
	if narrowed && len(candidateEvidenceIDs) == 0 {
		return result, nil
	}

	evidenceQuery := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id IN ?", tenantID, kbIDs)
	if !q.IncludeHistory {
		evidenceQuery = evidenceQuery.Where("status = ?", types.KnowledgeEvidenceCurrent)
	}
	if len(q.KnowledgeIDs) > 0 {
		evidenceQuery = evidenceQuery.Where("knowledge_id IN ?", uniqueContextIDs(q.KnowledgeIDs))
	}
	if narrowed {
		evidenceQuery = evidenceQuery.Where("id IN ?", candidateEvidenceIDs)
	}
	var evidence []types.KnowledgeEvidence
	if err := evidenceQuery.Order("created_at DESC, id DESC").Find(&evidence).Error; err != nil {
		return nil, err
	}
	evidence, err = s.visibleContextEvidence(ctx, tenantID, "", evidence, q.IncludeHistory)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]types.KnowledgeEvidence, len(evidence))
	for _, item := range evidence {
		allowed[item.ID] = item
	}
	if len(allowed) == 0 {
		return result, nil
	}

	if q.EventID != "" {
		var links []types.KnowledgeEventEvidence
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id IN ? AND event_id = ?", tenantID, kbIDs, q.EventID).Find(&links).Error; err != nil {
			return nil, err
		}
		linked := make(map[string]bool, len(links))
		for _, link := range links {
			linked[link.EvidenceID] = true
		}
		for id := range allowed {
			if !linked[id] {
				delete(allowed, id)
			}
		}
		if len(allowed) == 0 {
			return result, nil
		}
	}

	evidenceIDs := sortedEvidenceIDs(allowed)
	missingSources := contextMissingPublicationSources(allowed)
	result.ExcludedMissingPublishedAtCount = len(missingSources)
	result.PublishedAtMissingSourceIDs = []string{}
	if q.Since != nil || q.Until != nil {
		result.PublishedAtMissingSourceIDs = missingSources
	}
	factQuery := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id IN ? AND evidence_id IN ?", tenantID, kbIDs, evidenceIDs)
	if !q.IncludeHistory {
		factQuery = factQuery.Where("status = ?", types.KnowledgeFactCurrent)
	}
	factQuery = scope.factFields(factQuery, q)
	if q.EntityID != "" {
		factQuery = factQuery.Where("(subject_entity_id = ? OR object_entity_id = ?)", q.EntityID, q.EntityID)
	}
	if len(candidateEntityIDs) > 0 {
		factQuery = factQuery.Where("(subject_entity_id IN ? OR object_entity_id IN ?)", candidateEntityIDs, candidateEntityIDs)
	}
	if q.ObjectEntityID != "" {
		factQuery = factQuery.Where("object_entity_id = ?", q.ObjectEntityID)
	}
	var facts []types.KnowledgeFact
	if err := factQuery.Order("created_at DESC, id DESC").Find(&facts).Error; err != nil {
		return nil, err
	}

	objectLinks, err := s.contextEvidenceObjects(ctx, tenantID, evidence, q, candidateEntityIDs)
	if err != nil {
		return nil, err
	}
	entityIDs := make(map[string]bool)
	for _, link := range objectLinks {
		entityIDs[link.EntityID] = true
	}
	filteredFacts := make([]types.KnowledgeFact, 0, len(facts))
	for _, fact := range facts {
		source := allowed[fact.EvidenceID]
		if !contextFactInTimeRange(fact, source, q) || !contextFactMatchesValue(scope.domains[fact.KnowledgeBaseID], fact, q) {
			continue
		}
		if fact.OriginalPredicate == "" {
			fact.OriginalPredicate = fact.Predicate
		}
		fact.Predicate = canonicalContextField(scope.domains[fact.KnowledgeBaseID], fact.Predicate)
		fact.KnowledgeBaseName = scope.names[fact.KnowledgeBaseID]
		filteredFacts = append(filteredFacts, fact)
		entityIDs[fact.SubjectEntityID] = true
		if fact.ObjectEntityID != "" {
			entityIDs[fact.ObjectEntityID] = true
		}
	}
	facts = filteredFacts

	permittedEvents := s.db.WithContext(ctx).Model(&types.KnowledgeEventEvidence{}).
		Select("event_id").Where("tenant_id = ? AND knowledge_base_id IN ? AND evidence_id IN ?", tenantID, kbIDs, evidenceIDs)
	eventQuery := s.db.WithContext(ctx).Where("tenant_id = ? AND id IN (?)", tenantID, permittedEvents)
	if q.EventID != "" {
		eventQuery = eventQuery.Where("id = ?", q.EventID)
	}
	eventOrder := "occurred_at DESC, updated_at DESC, id DESC"
	if projectEventSources {
		// Aggregate timestamps may come from sources outside this query's scope.
		// Use a stable ID order here; the final order is based on the projected
		// dates and evidence from visible sources only.
		eventOrder = "id ASC"
	}
	var events []types.KnowledgeEvent
	if err := eventQuery.Order(eventOrder).Find(&events).Error; err != nil {
		return nil, err
	}
	linksByEvent := make(map[string][]types.KnowledgeEventEvidence, len(events))
	if len(events) > 0 {
		eventIDs := make([]string, 0, len(events))
		for _, event := range events {
			eventIDs = append(eventIDs, event.ID)
		}
		var links []types.KnowledgeEventEvidence
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id IN ? AND event_id IN ? AND evidence_id IN ?", tenantID, kbIDs, eventIDs, evidenceIDs).Find(&links).Error; err != nil {
			return nil, err
		}
		if err := loadContextEventEvidenceEntityIDs(s.db.WithContext(ctx), tenantID, links); err != nil {
			return nil, err
		}
		for _, link := range links {
			linksByEvent[link.EventID] = append(linksByEvent[link.EventID], link)
		}
	}
	selectedEvents := make([]types.KnowledgeEvent, 0, len(events))
	for _, event := range events {
		links := linksByEvent[event.ID]
		visibleIDs := types.StringArray{}
		visibleEntityIDs := types.StringArray{}
		visibleEvidence := make([]types.KnowledgeEvidence, 0, len(links))
		var visibleSummary *types.KnowledgeEventEvidence
		for _, link := range links {
			if item, ok := allowed[link.EvidenceID]; ok {
				visibleIDs = appendUniqueContextString(visibleIDs, link.EvidenceID)
				visibleEntityIDs = unionStringArrays(visibleEntityIDs, link.EntityIDs)
				visibleEvidence = append(visibleEvidence, item)
				found := false
				for _, source := range event.Sources {
					if source.KnowledgeBaseID == item.KnowledgeBaseID {
						found = true
						break
					}
				}
				if !found {
					event.Sources = append(event.Sources, types.KnowledgeContextSource{KnowledgeBaseID: item.KnowledgeBaseID, KnowledgeBaseName: scope.names[item.KnowledgeBaseID]})
				}
				if visibleSummary == nil || contextEventLinkNewer(link, *visibleSummary, allowed) {
					copyLink := link
					visibleSummary = &copyLink
				}
			}
		}
		if len(visibleIDs) == 0 {
			continue
		}
		if q.EntityID != "" && !containsString(visibleEntityIDs, q.EntityID) {
			continue
		}
		event.EvidenceIDs = visibleIDs
		event.EntityIDs = visibleEntityIDs

		if projectEventSources {
			if visibleSummary == nil {
				continue
			}
			event.Title = visibleSummary.SourceTitle
			if event.Title == "" {
				event.Title = allowed[visibleSummary.EvidenceID].Title
			}
			event.EventType = visibleSummary.SourceEventType
			event.Description = visibleSummary.SourceDescription
			event.OccurredAt = contextVisibleEventOccurredAt(links, allowed)
		} else if visibleSummary != nil {
			if visibleSummary.SourceTitle != "" {
				event.Title = visibleSummary.SourceTitle
			}
			if visibleSummary.SourceEventType != "" {
				event.EventType = visibleSummary.SourceEventType
			}
			if visibleSummary.SourceDescription != "" {
				event.Description = visibleSummary.SourceDescription
			}
			if visibleSummary.SourceOccurredAt != nil {
				event.OccurredAt = visibleSummary.SourceOccurredAt
			}
		}
		currentEvidence := make([]types.KnowledgeEvidence, 0, len(visibleEvidence))
		for _, item := range visibleEvidence {
			if item.Status == types.KnowledgeEvidenceCurrent {
				currentEvidence = append(currentEvidence, item)
			}
		}
		if !contextEventInTimeRange(event, visibleEvidence, q) {
			continue
		}
		for _, item := range currentEvidence {
			event.RepostSources = append(event.RepostSources, item.RepostSources...)
		}
		event.SourceCount, event.Heat = contextHeatWithReposts(currentEvidence, event.RepostSources)
		currentByID := make(map[string]types.KnowledgeEvidence, len(currentEvidence))
		for _, item := range currentEvidence {
			currentByID[item.ID] = item
		}
		event.ExcludedMissingPublishedAtCount = len(contextMissingPublicationSources(currentByID))
		selectedEvents = append(selectedEvents, event)
		for _, id := range event.EntityIDs {
			entityIDs[id] = true
		}
	}
	events = selectedEvents
	if err := s.projectVisibleEventRelations(ctx, tenantID, kbIDs, events, allowed, q); err != nil {
		return nil, err
	}

	if q.Entity != "" {
		candidates := make([]string, 0, len(entityIDs))
		for id := range entityIDs {
			candidates = append(candidates, id)
		}
		if len(candidates) == 0 {
			return result, nil
		}
		var entities []types.KnowledgeEntity
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id IN ? AND merged_into_id = ''", tenantID, candidates).Find(&entities).Error; err != nil {
			return nil, err
		}
		matched := make(map[string]bool)
		for _, entity := range entities {
			if strings.Contains(normalizeContextName(entity.CanonicalName), normalizeContextName(q.Entity)) {
				matched[entity.ID] = true
			}
		}
		var aliases []types.KnowledgeEntityAlias
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND entity_id IN ?", tenantID, candidates).Find(&aliases).Error; err != nil {
			return nil, err
		}
		for _, alias := range aliases {
			if (!limitedEntities || contextAliasVisible(alias.Alias, evidence)) && strings.Contains(normalizeContextName(alias.Alias), normalizeContextName(q.Entity)) {
				matched[alias.EntityID] = true
			}
		}
		facts = filterContextFactsByEntity(facts, matched)
		events = filterContextEventsByEntity(events, matched)
		entityIDs = matched
	}
	if projectEventSources {
		sort.SliceStable(events, func(i, j int) bool {
			leftTime := contextEventVisibleSortTime(events[i], allowed)
			rightTime := contextEventVisibleSortTime(events[j], allowed)
			if leftTime != nil && rightTime == nil {
				return true
			}
			if leftTime == nil && rightTime != nil {
				return false
			}
			if leftTime != nil && rightTime != nil && !leftTime.Equal(*rightTime) {
				return leftTime.After(*rightTime)
			}
			return events[i].ID < events[j].ID
		})
	}

	if len(facts) == 0 && len(events) == 0 && len(entityIDs) == 0 {
		return result, nil
	}
	if len(entityIDs) > 0 {
		ids := make([]string, 0, len(entityIDs))
		for id := range entityIDs {
			ids = append(ids, id)
		}
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND id IN ? AND merged_into_id = ''", tenantID, ids).Order("canonical_name ASC, id ASC").Find(&result.Entities).Error; err != nil {
			return nil, err
		}
		var aliases []types.KnowledgeEntityAlias
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND entity_id IN ?", tenantID, ids).Order("normalized_alias ASC").Find(&aliases).Error; err != nil {
			return nil, err
		}
		byID := make(map[string]*types.KnowledgeEntity, len(result.Entities))
		for i := range result.Entities {
			if limitedEntities {
				result.Entities[i].Description = ""

			}
			byID[result.Entities[i].ID] = &result.Entities[i]
		}
		for _, alias := range aliases {
			if entity := byID[alias.EntityID]; entity != nil && (!limitedEntities || contextAliasVisible(alias.Alias, evidence)) {
				entity.Aliases = append(entity.Aliases, alias.Alias)
				if limitedEntities {
					entity.Description = ""
				}
			}
		}
	}
	// Entity pagination is independent of the fact/event pages. The entity set
	// above is derived from all matching facts and events before their pages are
	// sliced, so offset-based entity loading doesn't skip an object merely
	// because its fact fell on another page.
	result.Facts = facts
	result.Events = events
	if paginated {
		result.Entities = paginateContext(result.Entities, q)
		result.Facts = paginateContextFacts(facts, q)
		result.Events = paginateContextEvents(events, q)
	}
	pageEntityIDs := make(map[string]bool, len(result.Entities))
	for _, entity := range result.Entities {
		pageEntityIDs[entity.ID] = true
	}
	visibleFactEvidence := make(map[string]bool)
	for _, fact := range result.Facts {
		visibleFactEvidence[fact.EvidenceID] = true
	}
	for _, fact := range facts {
		if pageEntityIDs[fact.SubjectEntityID] || pageEntityIDs[fact.ObjectEntityID] {
			visibleFactEvidence[fact.EvidenceID] = true
		}
	}
	visibleEventEvidence := make(map[string]bool)
	for _, event := range result.Events {
		for _, id := range event.EvidenceIDs {
			visibleEventEvidence[id] = true
		}
	}
	for _, event := range events {
		if intersectsVisibleIDs(event.EntityIDs, pageEntityIDs) {
			for _, id := range event.EvidenceIDs {
				visibleEventEvidence[id] = true
			}
		}
	}
	visibleEvidenceIDs := make(map[string]bool)
	for _, link := range objectLinks {
		if pageEntityIDs[link.EntityID] {
			visibleEvidenceIDs[link.EvidenceID] = true
		}
	}
	for id := range visibleFactEvidence {
		visibleEvidenceIDs[id] = true
	}
	for id := range visibleEventEvidence {
		visibleEvidenceIDs[id] = true
	}
	for _, item := range evidence {
		if visibleEvidenceIDs[item.ID] {
			item.KnowledgeBaseName = scope.names[item.KnowledgeBaseID]
			result.Evidence = append(result.Evidence, item)
		}
	}
	if len(visibleEvidenceIDs) > 0 {
		ids := make([]string, 0, len(visibleEvidenceIDs))
		for id := range visibleEvidenceIDs {
			ids = append(ids, id)
		}
		if err := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id IN ? AND evidence_id IN ?", tenantID, kbIDs, ids).Order("score DESC, created_at DESC").Find(&result.Attention).Error; err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *KnowledgeContextService) ListEntities(ctx context.Context, kbID string, q types.KnowledgeContextQuery) ([]types.KnowledgeEntity, error) {
	result, err := s.Query(ctx, kbID, q)
	if err != nil {
		return nil, err
	}
	return result.Entities, nil
}
func (s *KnowledgeContextService) ListEvents(ctx context.Context, kbID string, q types.KnowledgeContextQuery) ([]types.KnowledgeEvent, error) {
	result, err := s.Query(ctx, kbID, q)
	if err != nil {
		return nil, err
	}
	return result.Events, nil
}
func (s *KnowledgeContextService) ListEvidence(ctx context.Context, kbID string, q types.KnowledgeContextQuery) ([]types.KnowledgeEvidence, error) {
	items, err := s.listEvidenceCandidates(ctx, kbID, q)
	if err != nil {
		return nil, err
	}
	return paginateContext(items, q), nil
}

// ListEvidenceWithPublicationStats counts eligible sources before pagination.
func (s *KnowledgeContextService) ListEvidenceWithPublicationStats(ctx context.Context, kbID string, q types.KnowledgeContextQuery) ([]types.KnowledgeEvidence, int, error) {
	items, err := s.listEvidenceCandidates(ctx, kbID, q)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[string]types.KnowledgeEvidence, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	return paginateContext(items, q), len(contextMissingPublicationSources(byID)), nil
}

func (s *KnowledgeContextService) listEvidenceCandidates(ctx context.Context, kbID string, q types.KnowledgeContextQuery) ([]types.KnowledgeEvidence, error) {
	scope, err := s.resolveContextQueryScope(ctx, kbID, q)
	if err != nil {
		return nil, err
	}
	if len(scope.kbIDs) == 0 {
		return []types.KnowledgeEvidence{}, nil
	}
	tenantID := scope.tenantID
	query := s.db.WithContext(ctx).Where("tenant_id = ? AND knowledge_base_id IN ?", tenantID, scope.kbIDs)
	if !q.IncludeHistory {
		query = query.Where("status = ?", types.KnowledgeEvidenceCurrent)
	}
	if len(q.KnowledgeIDs) > 0 {
		query = query.Where("knowledge_id IN ?", uniqueContextIDs(q.KnowledgeIDs))
	}
	var items []types.KnowledgeEvidence
	if err := query.Order("created_at DESC, id DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	items, err = s.visibleContextEvidence(ctx, tenantID, kbID, items, q.IncludeHistory)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].KnowledgeBaseName = scope.names[items[i].KnowledgeBaseID]
	}
	return items, nil
}

func (s *KnowledgeContextService) GetEvidence(ctx context.Context, kbID, evidenceID string) (*types.KnowledgeEvidence, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}
	if kbID != "" {
		if _, err := s.scopedKnowledgeBase(ctx, kbID); err != nil {
			return nil, err
		}
	}
	var evidence types.KnowledgeEvidence
	if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", evidenceID, tenantID).First(&evidence).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, werrors.NewNotFoundError("knowledge evidence not found")
		}
		return nil, err
	}

	if kbID != "" && evidence.KnowledgeBaseID != kbID {
		return nil, werrors.NewNotFoundError("knowledge evidence not found")
	}
	visible, err := s.visibleContextEvidence(ctx, tenantID, kbID, []types.KnowledgeEvidence{evidence}, true)
	if err != nil {
		return nil, err
	}
	if len(visible) == 0 {
		return nil, werrors.NewNotFoundError("knowledge evidence not found")
	}
	var base types.KnowledgeBase
	if err := s.db.WithContext(ctx).Select("name").Where("tenant_id = ? AND id = ?", tenantID, evidence.KnowledgeBaseID).First(&base).Error; err != nil {
		return nil, err
	}
	evidence.KnowledgeBaseName = base.Name
	return &evidence, nil
}

func uniqueContextIDs(input []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, id := range input {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}
func sortedEvidenceIDs(evidence map[string]types.KnowledgeEvidence) []string {
	ids := make([]string, 0, len(evidence))
	for id := range evidence {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func contextFactInTimeRange(fact types.KnowledgeFact, evidence types.KnowledgeEvidence, q types.KnowledgeContextQuery) bool {
	from, to := fact.EffectiveFrom, fact.EffectiveTo
	// A partially specified validity interval is open at its missing endpoint.
	// Only facts without either endpoint fall back to a publication-time point.
	if from == nil && to == nil {
		from = parseContextTime(evidence.PublishedAt)
		to = from
		if from == nil {
			return q.Since == nil && q.Until == nil
		}
	}
	if q.Since != nil && to != nil && to.Before(*q.Since) {
		return false
	}
	if q.Until != nil && from != nil && from.After(*q.Until) {
		return false
	}
	return true
}
func contextEventInTimeRange(event types.KnowledgeEvent, evidence []types.KnowledgeEvidence, q types.KnowledgeContextQuery) bool {
	value := contextEventEffectiveTime(event, evidence)
	if q.Since != nil && (value == nil || value.Before(*q.Since)) {
		return false
	}
	if q.Until != nil && (value == nil || value.After(*q.Until)) {
		return false
	}
	return true
}

func contextEventEffectiveTime(event types.KnowledgeEvent, evidence []types.KnowledgeEvidence) *time.Time {
	value := event.OccurredAt
	if value == nil {
		for _, item := range evidence {
			parsed := parseContextTime(item.PublishedAt)
			if parsed != nil && (value == nil || parsed.After(*value)) {
				value = parsed
			}
		}
	}
	return value
}

func contextVisibleEventOccurredAt(links []types.KnowledgeEventEvidence, allowed map[string]types.KnowledgeEvidence) *time.Time {
	var selected *types.KnowledgeEventEvidence
	for i := range links {
		link := &links[i]
		if _, ok := allowed[link.EvidenceID]; !ok || link.SourceOccurredAt == nil {
			continue
		}
		if selected == nil || contextEventLinkNewer(*link, *selected, allowed) {
			selected = link
		}
	}
	if selected == nil {
		return nil
	}
	return selected.SourceOccurredAt
}

func contextEventVisibleSortTime(event types.KnowledgeEvent, allowed map[string]types.KnowledgeEvidence) *time.Time {
	evidence := make([]types.KnowledgeEvidence, 0, len(event.EvidenceIDs))
	for _, id := range event.EvidenceIDs {
		if item, ok := allowed[id]; ok {
			evidence = append(evidence, item)
		}
	}
	return contextEventEffectiveTime(event, evidence)
}

func contextAliasVisible(alias string, evidence []types.KnowledgeEvidence) bool {
	wanted := strings.ToLower(strings.TrimSpace(alias))
	if wanted == "" {
		return false
	}
	for _, item := range evidence {
		if verbatimQuote(strings.ToLower(item.Body), wanted) {
			return true
		}
	}
	return false
}

func contextEventLinkNewer(candidate, selected types.KnowledgeEventEvidence, evidence map[string]types.KnowledgeEvidence) bool {
	candidateTime := parseContextTime(evidence[candidate.EvidenceID].PublishedAt)
	selectedTime := parseContextTime(evidence[selected.EvidenceID].PublishedAt)
	if candidateTime != nil && selectedTime == nil {
		return true
	}
	if candidateTime == nil && selectedTime != nil {
		return false
	}
	if candidateTime != nil && selectedTime != nil && !candidateTime.Equal(*selectedTime) {
		return candidateTime.After(*selectedTime)
	}
	return candidate.CreatedAt.After(selected.CreatedAt)
}

func (s *KnowledgeContextService) visibleContextEvidence(ctx context.Context, tenantID uint64, kbID string, evidence []types.KnowledgeEvidence, includeHistory bool) ([]types.KnowledgeEvidence, error) {
	if len(evidence) == 0 {
		return evidence, nil
	}
	ids := make([]string, 0, len(evidence))
	for _, item := range evidence {
		ids = append(ids, item.KnowledgeID)
	}
	var sources []types.Knowledge
	if err := s.db.WithContext(ctx).Select("id", "tenant_id", "knowledge_base_id", "parse_status", "enable_status", "metadata").
		Where("tenant_id = ? AND id IN ?", tenantID, uniqueContextIDs(ids)).Find(&sources).Error; err != nil {
		return nil, err
	}
	byID := make(map[string]types.Knowledge, len(sources))
	for _, source := range sources {
		if source.ParseStatus != types.ParseStatusFinalizing && source.ParseStatus != types.ParseStatusCompleted {
			continue
		}
		if source.IsSourceUpdateCandidate() {
			continue
		}
		archived := source.IsArchivedSource()
		if archived && !includeHistory {
			continue
		}
		if source.EnableStatus == "disabled" && !archived {
			continue
		}
		intake, err := source.IntakeMetadata()
		if err != nil {
			continue
		}
		if intake != nil && intake.Decision != "approved" {
			continue
		}
		byID[source.ID] = source
	}
	visible := make([]types.KnowledgeEvidence, 0, len(evidence))
	for _, item := range evidence {
		if _, ok := byID[item.KnowledgeID]; ok {
			visible = append(visible, item)
		}
	}
	visibleKnowledgeIDs := make([]string, 0, len(visible))
	visibleKBIDs := make([]string, 0, len(visible))
	for _, item := range visible {
		visibleKnowledgeIDs = append(visibleKnowledgeIDs, item.KnowledgeID)
		visibleKBIDs = append(visibleKBIDs, item.KnowledgeBaseID)
	}
	reposts, err := loadKnowledgeRepostSources(s.db.WithContext(ctx), tenantID, visibleKnowledgeIDs, uniqueContextIDs(visibleKBIDs))
	if err != nil {
		return nil, err
	}
	byKnowledge := make(map[string][]types.KnowledgeRepostSource)
	for _, source := range reposts {
		byKnowledge[source.KnowledgeID] = append(byKnowledge[source.KnowledgeID], source)
	}
	for i := range visible {
		visible[i].RepostSources = byKnowledge[visible[i].KnowledgeID]
	}
	return visible, nil
}
func filterContextFactsByEntity(facts []types.KnowledgeFact, ids map[string]bool) []types.KnowledgeFact {
	out := facts[:0]
	for _, item := range facts {
		if ids[item.SubjectEntityID] || ids[item.ObjectEntityID] {
			out = append(out, item)
		}
	}
	return out
}
func filterContextEventsByEntity(events []types.KnowledgeEvent, ids map[string]bool) []types.KnowledgeEvent {
	out := events[:0]
	for _, item := range events {
		if intersectsVisibleIDs(item.EntityIDs, ids) {
			out = append(out, item)
		}
	}
	return out
}
func intersectsVisibleIDs(values []string, ids map[string]bool) bool {
	for _, value := range values {
		if ids[value] {
			return true
		}
	}
	return false
}
func paginateContextFacts(items []types.KnowledgeFact, q types.KnowledgeContextQuery) []types.KnowledgeFact {
	return paginateContext(items, q)
}
func paginateContextEvents(items []types.KnowledgeEvent, q types.KnowledgeContextQuery) []types.KnowledgeEvent {
	return paginateContext(items, q)
}
func paginateContext[T any](items []T, q types.KnowledgeContextQuery) []T {
	limit := contextQueryLimit(q.Limit)
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []T{}
	}
	end := min(offset+limit, len(items))
	return items[offset:end]
}

func contextQueryLimit(limit int) int {
	if limit <= 0 || limit > 200 {
		return 50
	}
	return limit
}

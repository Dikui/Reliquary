package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/common"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

type contextResolutionEvidenceKey struct{}

func withContextResolutionEvidence(ctx context.Context, evidence *types.KnowledgeEvidence) context.Context {
	return context.WithValue(ctx, contextResolutionEvidenceKey{}, evidence)
}
func contextResolutionEvidence(ctx context.Context) *types.KnowledgeEvidence {
	evidence, _ := ctx.Value(contextResolutionEvidenceKey{}).(*types.KnowledgeEvidence)
	return evidence
}
func contextRecallLimit(variable string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(variable))
	if err != nil || value < 1 || value > 100 {
		return fallback
	}
	return value
}

// Types with the same identity semantics share exact names and recall candidates.
// Other domain types remain distinct, even when their names happen to coincide.
func compatibleContextEntityTypes(entityType string) []string {
	key := normalizeContextName(entityType)
	for _, group := range [][]string{
		{"company", "organization", "organisation", "org", "公司", "组织", "机构"},
		{"model", "llm", "模型", "大模型"},
		{"person", "individual", "人物", "个人"},
		{"product", "产品"},
	} {
		for _, value := range group {
			if value == key {
				return group
			}
		}
	}
	return []string{key}
}
func compatibleContextEntityType(left, right string) bool {
	return containsString(compatibleContextEntityTypes(left), normalizeContextName(right))
}
func contextEntityNameKey(entityType, name string) string {
	return fmt.Sprintf("%q:%q", compatibleContextEntityTypes(entityType)[0], normalizeContextName(name))
}

var contextVersionNumber = regexp.MustCompile(`\d+(?:\.\d+)*`)

func contextIdentityVersionsCompatible(left, right string) bool {
	a, b := contextVersionNumber.FindAllString(left, -1), contextVersionNumber.FindAllString(right, -1)
	return len(a) == 0 || len(b) == 0 || strings.Join(a, ":") == strings.Join(b, ":")
}

func (s *KnowledgeContextService) exactContextEntity(ctx context.Context, tenantID uint64, item contextExtractedEntity) (types.KnowledgeEntity, error) {
	var entity types.KnowledgeEntity
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND merged_into_id = '' AND lower(entity_type) IN ? AND normalized_name = ?", tenantID, compatibleContextEntityTypes(item.EntityType), normalizeContextName(item.Mention)).Order("id").First(&entity).Error
	if err == nil {
		return entity, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return entity, err
	}
	err = s.db.WithContext(ctx).Model(&types.KnowledgeEntity{}).
		Joins("JOIN knowledge_context_entity_aliases a ON a.tenant_id = knowledge_context_entities.tenant_id AND a.entity_id = knowledge_context_entities.id").
		Where("knowledge_context_entities.tenant_id = ? AND knowledge_context_entities.merged_into_id = '' AND lower(a.entity_type) IN ? AND a.normalized_alias = ?", tenantID, compatibleContextEntityTypes(item.EntityType), normalizeContextName(item.Mention)).
		Order("knowledge_context_entities.id").First(&entity).Error
	return entity, err
}

type contextScoredEntity struct {
	types.KnowledgeEntity
	RecallScore float64 `gorm:"column:recall_score"`
}

func (s *KnowledgeContextService) recallContextEntities(ctx context.Context, tenantID uint64, item contextExtractedEntity, vector types.ContextNameEmbedding, modelID string, limit int) ([]types.KnowledgeEntity, error) {
	names := []string{normalizeContextName(item.Mention), normalizeContextName(item.Canonical)}
	var namesResult []contextScoredEntity
	err := s.db.WithContext(ctx).Raw(`WITH scores AS (
 SELECT id AS entity_id, GREATEST(similarity(normalized_name, ?), similarity(normalized_name, ?)) AS score
 FROM knowledge_context_entities WHERE tenant_id=? AND merged_into_id='' AND lower(entity_type) IN ? AND (normalized_name % ? OR normalized_name % ?)
 UNION ALL
 SELECT a.entity_id, GREATEST(similarity(a.normalized_alias, ?), similarity(a.normalized_alias, ?)) AS score
 FROM knowledge_context_entity_aliases a JOIN knowledge_context_entities matched ON matched.tenant_id=a.tenant_id AND matched.id=a.entity_id
 WHERE a.tenant_id=? AND matched.merged_into_id='' AND lower(matched.entity_type) IN ? AND (a.normalized_alias % ? OR a.normalized_alias % ?))
 SELECT e.*, MAX(scores.score) AS recall_score FROM scores JOIN knowledge_context_entities e ON e.id=scores.entity_id AND e.tenant_id=?
 GROUP BY e.id ORDER BY recall_score DESC,e.id LIMIT ?`, names[0], names[1], tenantID, compatibleContextEntityTypes(item.EntityType), names[0], names[1], names[0], names[1], tenantID, compatibleContextEntityTypes(item.EntityType), names[0], names[1], tenantID, limit).Scan(&namesResult).Error
	if err != nil {
		return nil, err
	}
	scores := map[string]contextScoredEntity{}
	for _, candidate := range namesResult {
		scores[candidate.ID] = candidate
	}
	if len(vector) > 0 && modelID != "" {
		var vectorResult []contextScoredEntity
		encoded, _ := json.Marshal(vector)
		// MATERIALIZED isolates valid dimensions before evaluating pgvector distance.
		err = s.db.WithContext(ctx).Raw(`WITH compatible AS MATERIALIZED (
   SELECT * FROM knowledge_context_entities WHERE tenant_id=? AND merged_into_id='' AND lower(entity_type) IN ?
    AND embedding_model_id=? AND embedding_dimension=? AND vector_dims(name_embedding)=? AND NOT embedding_needs_rebuild)
   SELECT compatible.*, 1 - (name_embedding <=> ?::vector) AS recall_score FROM compatible
   ORDER BY name_embedding <=> ?::vector,id LIMIT ?`, tenantID, compatibleContextEntityTypes(item.EntityType), modelID, len(vector), len(vector), string(encoded), string(encoded), limit).Scan(&vectorResult).Error
		if err != nil {
			return nil, err
		}
		for _, candidate := range vectorResult {
			if old, ok := scores[candidate.ID]; !ok || candidate.RecallScore > old.RecallScore {
				scores[candidate.ID] = candidate
			}
		}
	}
	merged := make([]contextScoredEntity, 0, len(scores))
	for _, candidate := range scores {
		merged = append(merged, candidate)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].RecallScore == merged[j].RecallScore {
			return merged[i].ID < merged[j].ID
		}
		return merged[i].RecallScore > merged[j].RecallScore
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}
	candidates := make([]types.KnowledgeEntity, 0, len(merged))
	ids := []string{}
	for _, candidate := range merged {
		candidates = append(candidates, candidate.KnowledgeEntity)
		ids = append(ids, candidate.ID)
	}
	if len(ids) > 0 {
		var aliases []types.KnowledgeEntityAlias
		if err := s.db.WithContext(ctx).Where("tenant_id=? AND entity_id IN ?", tenantID, ids).Find(&aliases).Error; err != nil {
			return nil, err
		}
		for i := range candidates {
			for _, alias := range aliases {
				if alias.EntityID == candidates[i].ID {
					candidates[i].Aliases = append(candidates[i].Aliases, alias.Alias)
				}
			}
		}
	}
	return candidates, nil
}

func contextNameEmbeddingText(canonical, entityType, description string) string {
	return strings.Join([]string{strings.TrimSpace(canonical), normalizeContextName(entityType), strings.TrimSpace(description)}, "\n")
}

func (s *KnowledgeContextService) contextNameEmbedder(ctx context.Context, tenantID uint64) (embedding.Embedder, string, string, error) {
	if s.models == nil {
		return nil, "", "", nil
	}
	profile, err := NewContextNameEmbeddingRebuilder(s.db, s.models, nil).profile(ctx, tenantID)
	if err != nil {
		return nil, "", "", nil
	}
	return profile.embedder, profile.modelID, profile.hash, nil
}

func (s *KnowledgeContextService) resolveContextEntities(ctx context.Context, model chat.Chat, tenantID uint64, kbID string, input []contextExtractedEntity, templates ...*types.KnowledgeDomain) ([]types.KnowledgeEntity, map[string]types.KnowledgeEntity, error) {
	embedder, embeddingModelID, embeddingHash, err := s.contextNameEmbedder(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	touched := map[string]types.KnowledgeEntity{}
	localNames := map[string]string{}
	mentions := map[string]types.KnowledgeEntity{}
	limit := contextRecallLimit("RELIQUARY_CONTEXT_ENTITY_TOP_K", 20)
	evidence := contextResolutionEvidence(ctx)
	for _, item := range input {
		mentionKey := normalizeContextName(item.Mention)
		if mentionKey == "" {
			continue
		}
		entity := types.KnowledgeEntity{}
		method := "exact"
		if id, ok := localNames[contextEntityNameKey(item.EntityType, item.Mention)]; ok {
			entity = touched[id]
		}
		if entity.ID == "" {
			entity, err = s.exactContextEntity(ctx, tenantID, item)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil, err
			}
		}
		if entity.ID == "" {
			var vector types.ContextNameEmbedding
			if embedder != nil {
				embedCtx := types.WithLLMCallMetadata(types.WithModelTier(ctx, types.ModelTierEmbedding), "context_entity_embedding", "")
				types.RecordModelCall(embedCtx, embeddingModelID)
				value, embedErr := embedder.Embed(embedCtx, contextNameEmbeddingText(item.Canonical, item.EntityType, item.Description))
				if embedErr == nil && len(value) == embedder.GetDimensions() {
					vector = value
				}
			}
			candidates, recallErr := s.recallContextEntities(ctx, tenantID, item, vector, embeddingModelID, limit)
			if recallErr != nil {
				return nil, nil, recallErr
			}
			decision, judgeErr := s.judgeContextEntity(ctx, model, item, candidates, templates...)
			if judgeErr != nil {
				return nil, nil, judgeErr
			}
			method = "model"
			if decision.Decision == "match" {
				for _, candidate := range candidates {
					if candidate.ID == decision.EntityID {
						entity = candidate
						break
					}
				}
			}
			if entity.ID == "" {
				canonical := strings.TrimSpace(item.Canonical)
				if !contextIdentityVersionsCompatible(item.Mention, canonical) {
					canonical = strings.TrimSpace(item.Mention)
				}
				for _, candidate := range candidates {
					if candidate.NormalizedName == normalizeContextName(canonical) {
						canonical = strings.TrimSpace(item.Mention)
						break
					}
				}
				if embedder != nil && canonical != strings.TrimSpace(item.Canonical) {
					embedCtx := types.WithLLMCallMetadata(types.WithModelTier(ctx, types.ModelTierEmbedding), "context_entity_embedding", "")
					types.RecordModelCall(embedCtx, embeddingModelID)
					value, embedErr := embedder.Embed(embedCtx, contextNameEmbeddingText(canonical, item.EntityType, item.Description))
					vector = nil
					if embedErr == nil && len(value) == embedder.GetDimensions() {
						vector = value
					}
				}
				entity = types.KnowledgeEntity{ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kbID, CanonicalName: canonical, NormalizedName: normalizeContextName(canonical), EntityType: normalizeContextName(item.EntityType), Description: strings.TrimSpace(item.Description), CreatedAt: time.Now(), UpdatedAt: time.Now(), NameEmbedding: vector, NameEmbeddingProfileHash: embeddingHash, EmbeddingModelID: embeddingModelID, EmbeddingDimension: len(vector), EmbeddingNeedsRebuild: len(vector) == 0}
				// A distinct source spelling still receives its own judgment. Equal
				// canonical identities in this message share one creation plan afterward.
				if id, ok := localNames[contextEntityNameKey(item.EntityType, canonical)]; ok && decision.Decision == "new" && contextIdentityVersionsCompatible(item.Mention, canonical) {
					entity = touched[id]
				}
				if decision.Decision == "uncertain" {
					candidateIDs := decision.CandidateIDs
					if decision.EntityID != "" {
						candidateIDs = appendUniqueContextString(candidateIDs, decision.EntityID)
					}
					if len(candidateIDs) == 0 {
						for _, candidate := range candidates {
							candidateIDs = append(candidateIDs, candidate.ID)
						}
					}
					for _, candidate := range candidates {
						if !containsString(candidateIDs, candidate.ID) {
							continue
						}
						suggestion := types.KnowledgeEntityMergeSuggestion{ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kbID, NewEntityID: entity.ID, CandidateEntityID: candidate.ID, Reason: decision.Reason, Quote: item.Quote, Status: "pending", CreatedAt: time.Now()}
						if suggestion.Reason == "" {
							suggestion.Reason = "Object identity remains uncertain after candidate judgment."
						}
						if evidence != nil {
							suggestion.EvidenceID = evidence.ID
						}
						entity.MergeSuggestions = append(entity.MergeSuggestions, suggestion)
					}
				}
			}
		}
		if previous, ok := touched[entity.ID]; ok {
			entity.AliasResolutions = previous.AliasResolutions
			suggestions := append([]types.KnowledgeEntityMergeSuggestion{}, previous.MergeSuggestions...)
			seen := make(map[string]bool, len(suggestions))
			for _, suggestion := range suggestions {
				seen[suggestion.ID] = true
			}
			for _, suggestion := range entity.MergeSuggestions {
				if !seen[suggestion.ID] {
					suggestions = append(suggestions, suggestion)
					seen[suggestion.ID] = true
				}
			}
			entity.MergeSuggestions = suggestions
			entity.Aliases = unionStringArrays(previous.Aliases, entity.Aliases)
		}
		aliases := []string{item.Mention}
		for _, alias := range item.Aliases {
			if strings.Contains(item.Quote, alias) {
				aliases = append(aliases, alias)
			}
		}
		for _, aliasText := range aliases {
			alias := types.KnowledgeEntityAlias{Alias: strings.TrimSpace(aliasText), NormalizedAlias: normalizeContextName(aliasText), Quote: item.Quote, ResolutionMethod: method, KnowledgeBaseID: kbID}
			if evidence != nil {
				alias.EvidenceID = evidence.ID
			}
			entity.AliasResolutions = append(entity.AliasResolutions, alias)
			entity.Aliases = appendUniqueContextString(entity.Aliases, aliasText)
			localNames[contextEntityNameKey(entity.EntityType, aliasText)] = entity.ID
		}
		localNames[contextEntityNameKey(entity.EntityType, entity.CanonicalName)] = entity.ID
		mentions[mentionKey] = entity
		touched[entity.ID] = entity
	}
	resolved := make([]types.KnowledgeEntity, 0, len(touched))
	for _, entity := range touched {
		resolved = append(resolved, entity)
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].ID < resolved[j].ID })
	return resolved, mentions, nil
}

func (s *KnowledgeContextService) judgeContextEntity(ctx context.Context, model chat.Chat, item contextExtractedEntity, candidates []types.KnowledgeEntity, templates ...*types.KnowledgeDomain) (contextEntityMatch, error) {
	ctx = types.WithLLMCallMetadata(ctx, "context_entity_judgment", "")
	if limit := contextRecallLimit("RELIQUARY_CONTEXT_ENTITY_TOP_K", 20); len(candidates) > limit {
		candidates = candidates[:limit]
	}
	if model == nil {
		return contextEntityMatch{}, errors.New("entity judgment model is unavailable")
	}
	hints := ""
	if len(templates) > 0 && templates[0] != nil {
		hints = "\n" + templates[0].ExtractionHints
	}
	payload := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		payload = append(payload, map[string]any{"id": candidate.ID, "name": candidate.CanonicalName, "type": candidate.EntityType, "description": candidate.Description, "aliases": candidate.Aliases})
	}
	encoded, _ := json.Marshal(map[string]any{"mention": item.Mention, "source_quote": item.Quote, "canonical_name": item.Canonical, "entity_type": item.EntityType, "candidates": payload})
	response, err := model.Chat(ctx, []chat.Message{{Role: "system", Content: `Decide whether the source mention is the exact same real-world object as one candidate. Similar names, different versions, or a shared topic do not prove identity. Return {"decision":"match|new|uncertain","entity_id":"listed ID for match, otherwise empty","candidate_ids":["listed IDs relevant to uncertainty"],"confidence":0.0,"reason":"brief evidence-based reason"}. Match requires confidence at least 0.9. Empty candidates still require one decision and normally mean new.` + hints}, {Role: "user", Content: string(encoded)}}, &chat.ChatOptions{Temperature: 0})
	if err != nil || response == nil {
		if err == nil {
			err = errors.New("empty entity resolution response")
		}
		return contextEntityMatch{}, err
	}
	var result contextEntityMatch
	if err := common.ParseLLMJsonResponse(response.Content, &result); err != nil {
		return result, fmt.Errorf("invalid entity resolution JSON: %w", err)
	}
	result.Decision = strings.ToLower(strings.TrimSpace(result.Decision))
	if result.Decision == "" {
		if result.EntityID != "" {
			result.Decision = "match"
		} else {
			result.Decision = "new"
		}
	}
	if result.Decision != "match" && result.Decision != "new" && result.Decision != "uncertain" {
		return result, errors.New("invalid entity resolution decision")
	}
	if result.Decision == "match" {
		valid := false
		for _, candidate := range candidates {
			if candidate.ID == result.EntityID {
				valid = true
				break
			}
		}
		if !valid || result.Confidence < 0.9 || result.Confidence > 1 {
			result.Decision = "uncertain"
		}
	}
	return result, nil
}
func (s *KnowledgeContextService) matchContextEntity(ctx context.Context, model chat.Chat, item contextExtractedEntity, candidates []types.KnowledgeEntity, templates ...*types.KnowledgeDomain) (types.KnowledgeEntity, error) {
	result, err := s.judgeContextEntity(ctx, model, item, candidates, templates...)
	if err != nil {
		return types.KnowledgeEntity{}, err
	}
	if result.Decision == "match" {
		for _, candidate := range candidates {
			if candidate.ID == result.EntityID {
				return candidate, nil
			}
		}
	}
	return types.KnowledgeEntity{}, nil
}

func persistContextEntities(tx *gorm.DB, entities []types.KnowledgeEntity, firstSourceKB ...string) (map[string]string, error) {
	if len(entities) > 0 {
		if err := lockContextTeamShared(tx, entities[0].TenantID); err != nil {
			return nil, err
		}
		keys := []string{}
		for _, entity := range entities {
			for _, name := range append([]string{entity.CanonicalName}, entity.Aliases...) {
				keys = append(keys, contextEntityNameKey(entity.EntityType, name))
			}
		}
		if err := lockContextIdentities(tx, entities[0].TenantID, keys); err != nil {
			return nil, err
		}
	}
	ids := map[string]string{}
	for _, planned := range entities {
		entity := planned
		var existing types.KnowledgeEntity
		err := tx.Where("id=? AND tenant_id=?", entity.ID, entity.TenantID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = tx.Where("tenant_id=? AND lower(entity_type) IN ? AND normalized_name=?", entity.TenantID, compatibleContextEntityTypes(entity.EntityType), entity.NormalizedName).Order("id").First(&existing).Error
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var alias types.KnowledgeEntityAlias
			names := []string{entity.NormalizedName}
			for _, name := range entity.Aliases {
				names = appendUniqueContextString(names, normalizeContextName(name))
			}
			aliasErr := tx.Where("tenant_id=? AND lower(entity_type) IN ? AND normalized_alias IN ?", entity.TenantID, compatibleContextEntityTypes(entity.EntityType), names).Order("id").First(&alias).Error
			if aliasErr == nil {
				err = tx.Where("tenant_id=? AND id=?", entity.TenantID, alias.EntityID).First(&existing).Error
			} else if !errors.Is(aliasErr, gorm.ErrRecordNotFound) {
				return nil, aliasErr
			}
		}
		if err == nil {
			for existing.MergedIntoID != "" {
				next := existing.MergedIntoID
				existing = types.KnowledgeEntity{}
				if err := tx.Where("tenant_id=? AND id=?", entity.TenantID, next).First(&existing).Error; err != nil {
					return nil, err
				}
			}
			entity = existing
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		} else {
			if len(firstSourceKB) > 0 {
				entity.KnowledgeBaseID = firstSourceKB[0]
			}
			if len(entity.NameEmbedding) > 0 {
				rebuilder := NewContextNameEmbeddingRebuilder(tx, nil, nil)
				profile := contextEmbeddingProfile{modelID: entity.EmbeddingModelID, dimension: entity.EmbeddingDimension, hash: entity.NameEmbeddingProfileHash}
				if entity.NameEmbeddingProfileHash == "" || rebuilder.checkProfile(tx, entity.TenantID, profile) != nil {
					entity.EmbeddingNeedsRebuild = true
				}
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "entity_type"}, {Name: "normalized_name"}}, DoNothing: true}).Create(&entity).Error; err != nil {
				return nil, err
			}
			if err := tx.Where("tenant_id=? AND entity_type=? AND normalized_name=?", entity.TenantID, entity.EntityType, entity.NormalizedName).First(&existing).Error; err != nil {
				return nil, err
			}
			entity = existing
		}
		ids[planned.ID] = entity.ID
		resolutions := map[string]types.KnowledgeEntityAlias{}
		for _, alias := range planned.AliasResolutions {
			resolutions[alias.NormalizedAlias] = alias
		}
		seen := map[string]bool{}
		for _, name := range append([]string{planned.CanonicalName}, planned.Aliases...) {
			key := normalizeContextName(name)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			var collision types.KnowledgeEntityAlias
			found := tx.Where("tenant_id=? AND lower(entity_type) IN ? AND normalized_alias=?", entity.TenantID, compatibleContextEntityTypes(entity.EntityType), key).First(&collision).Error
			if found == nil {
				continue
			}
			if !errors.Is(found, gorm.ErrRecordNotFound) {
				return nil, found
			}
			alias := resolutions[key]
			alias.ID = uuid.NewString()
			alias.TenantID = entity.TenantID
			alias.EntityType = entity.EntityType
			alias.EntityID = entity.ID
			alias.Alias = strings.TrimSpace(name)
			alias.NormalizedAlias = key
			alias.CreatedAt = time.Now()
			if alias.KnowledgeBaseID == "" {
				alias.KnowledgeBaseID = entity.KnowledgeBaseID
			}
			if alias.ResolutionMethod == "" {
				alias.ResolutionMethod = "exact"
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "entity_type"}, {Name: "normalized_alias"}}, DoNothing: true}).Create(&alias).Error; err != nil {
				return nil, err
			}
		}
	}
	for _, entity := range entities {
		for _, suggestion := range entity.MergeSuggestions {
			if id, ok := ids[suggestion.NewEntityID]; ok {
				suggestion.NewEntityID = id
			}
			if id, ok := ids[suggestion.CandidateEntityID]; ok {
				suggestion.CandidateEntityID = id
			}
			if suggestion.NewEntityID == suggestion.CandidateEntityID {
				continue
			}
			var candidate types.KnowledgeEntity
			if err := tx.Where("tenant_id=? AND id=?", suggestion.TenantID, suggestion.CandidateEntityID).First(&candidate).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			} else if err != nil {
				return nil, err
			}
			for candidate.MergedIntoID != "" {
				next := candidate.MergedIntoID
				if err := tx.Where("tenant_id=? AND id=?", suggestion.TenantID, next).First(&candidate).Error; err != nil {
					return nil, err
				}
			}
			suggestion.CandidateEntityID = candidate.ID
			if suggestion.NewEntityID == suggestion.CandidateEntityID {
				continue
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "tenant_id"}, {Name: "new_entity_id"}, {Name: "candidate_entity_id"}, {Name: "evidence_id"}}, DoNothing: true}).Create(&suggestion).Error; err != nil {
				return nil, err
			}
		}
	}
	return ids, nil
}

func (s *KnowledgeContextService) recallContextEvents(ctx context.Context, tenantID uint64, item contextExtractedEvent, entityIDs types.StringArray) ([]types.KnowledgeEvent, error) {
	currentTime := verifiedContextTime(item.OccurredAt, item.Quote)
	if currentTime == nil {
		if evidence := contextResolutionEvidence(ctx); evidence != nil {
			currentTime = parseContextTime(evidence.PublishedAt)
		}
	}
	var candidates []types.KnowledgeEvent
	query := s.db.WithContext(ctx).Model(&types.KnowledgeEvent{}).Where("knowledge_context_events.tenant_id=? AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(knowledge_context_events.entity_ids::jsonb) AS related(id) WHERE related.id IN ?)", tenantID, []string(entityIDs))
	if currentTime != nil {
		// Publication strings are parsed by PostgreSQL only after a strict timestamp guard.
		query = query.Where(`(knowledge_context_events.occurred_at BETWEEN ? AND ?) OR (knowledge_context_events.occurred_at IS NULL AND (NOT EXISTS (
   SELECT 1 FROM knowledge_context_event_evidence links JOIN knowledge_context_evidence source ON source.id=links.evidence_id AND source.tenant_id=links.tenant_id
   WHERE links.tenant_id=knowledge_context_events.tenant_id AND links.event_id=knowledge_context_events.id AND source.published_at ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}($|T)' AND pg_input_is_valid(source.published_at, 'timestamptz')) OR EXISTS (
   SELECT 1 FROM knowledge_context_event_evidence links JOIN knowledge_context_evidence source ON source.id=links.evidence_id AND source.tenant_id=links.tenant_id
   WHERE links.tenant_id=knowledge_context_events.tenant_id AND links.event_id=knowledge_context_events.id AND
   CASE WHEN source.published_at ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}($|T)' AND pg_input_is_valid(source.published_at, 'timestamptz') THEN source.published_at::timestamptz END BETWEEN ? AND ?)))`, currentTime.Add(-7*24*time.Hour), currentTime.Add(7*24*time.Hour), currentTime.Add(-7*24*time.Hour), currentTime.Add(7*24*time.Hour))
	}
	err := query.Order("knowledge_context_events.occurred_at DESC NULLS LAST, knowledge_context_events.updated_at DESC, knowledge_context_events.id").Limit(contextRecallLimit("RELIQUARY_CONTEXT_EVENT_TOP_K", 10)).Find(&candidates).Error
	return candidates, err
}
func (s *KnowledgeContextService) resolveContextEvents(ctx context.Context, model chat.Chat, tenantID uint64, kbID string, input []contextExtractedEvent, mentions map[string]types.KnowledgeEntity) ([]contextEventResolution, error) {
	resolved := make([]contextEventResolution, 0, len(input))
	for _, item := range input {
		entityIDs := types.StringArray{}
		for _, mention := range item.EntityMentions {
			entity, ok := mentions[normalizeContextName(mention)]
			if !ok {
				return nil, errors.New("knowledge context event entity could not be resolved")
			}
			entityIDs = appendUniqueContextString(entityIDs, entity.ID)
		}
		if len(entityIDs) == 0 {
			return nil, errors.New("knowledge context event requires a linked object")
		}
		candidates, err := s.recallContextEvents(ctx, tenantID, item, entityIDs)
		if err != nil {
			return nil, err
		}
		matched, err := s.matchContextEvent(ctx, model, item, entityIDs, candidates)
		if err != nil {
			return nil, err
		}
		event := matched.Event
		relatedEventID := ""
		if matched.Relation == types.KnowledgeEventRelationSameStory {
			relatedEventID = event.ID
			event = types.KnowledgeEvent{}
		}
		if matched.Relation != types.KnowledgeEventRelationSameOccurrence || event.ID == "" {
			event = types.KnowledgeEvent{ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kbID, Title: strings.TrimSpace(item.Title), EventType: strings.TrimSpace(item.EventType), Description: strings.TrimSpace(item.Description), OccurredAt: verifiedContextTime(item.OccurredAt, item.Quote), EntityIDs: entityIDs, EvidenceIDs: types.StringArray{}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		} else {
			relatedEventID = event.ID
			event.EntityIDs = unionStringArrays(event.EntityIDs, entityIDs)
			if event.OccurredAt == nil {
				event.OccurredAt = verifiedContextTime(item.OccurredAt, item.Quote)
			}
		}
		event.SourceEntityIDs = entityIDs
		resolved = append(resolved, contextEventResolution{Event: event, Relation: matched.Relation, RelatedEventID: relatedEventID})
	}
	return resolved, nil
}
func contextEventCandidatesByEntity(candidates []types.KnowledgeEvent, ids types.StringArray) []types.KnowledgeEvent {
	out := []types.KnowledgeEvent{}
	for _, candidate := range candidates {
		if intersectsContextIDs(candidate.EntityIDs, ids) {
			out = append(out, candidate)
		}
	}
	return out
}
func intersectsContextIDs(left, right []string) bool {
	for _, id := range left {
		if id != "" && containsString(right, id) {
			return true
		}
	}
	return false
}
func normalizeContextEventRelation(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case types.KnowledgeEventRelationSameOccurrence:
		return types.KnowledgeEventRelationSameOccurrence
	case types.KnowledgeEventRelationSameStory:
		return types.KnowledgeEventRelationSameStory
	case types.KnowledgeEventRelationUnrelated:
		return types.KnowledgeEventRelationUnrelated
	}
	return ""
}
func (s *KnowledgeContextService) matchContextEvent(ctx context.Context, model chat.Chat, item contextExtractedEvent, entityIDs types.StringArray, candidates []types.KnowledgeEvent) (contextEventResolution, error) {
	ctx = types.WithLLMCallMetadata(ctx, "context_event_judgment", "")
	if limit := contextRecallLimit("RELIQUARY_CONTEXT_EVENT_TOP_K", 10); len(candidates) > limit {
		candidates = candidates[:limit]
	}
	if model == nil {
		return contextEventResolution{}, errors.New("event judgment model is unavailable")
	}
	payload := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		payload = append(payload, map[string]any{"id": candidate.ID, "title": candidate.Title, "type": candidate.EventType, "description": candidate.Description, "occurred_at": candidate.OccurredAt, "entity_ids": candidate.EntityIDs})
	}
	encoded, _ := json.Marshal(map[string]any{"new_event": item, "entity_ids": entityIDs, "candidates": payload})
	response, err := model.Chat(ctx, []chat.Message{{Role: "system", Content: `Determine whether new_event describes the same specific real-world change as a candidate, a distinct follow-up in the same story, or an unrelated event. SAME_OCCURRENCE reuses the candidate. SAME_STORY preserves a separate event linked to the candidate. A shared entity or topic alone is insufficient. Return {"relation":"SAME_OCCURRENCE|SAME_STORY|UNRELATED","event_id":"listed candidate ID or empty","confidence":0.0}. Empty candidates require UNRELATED. Use only listed IDs. If uncertain, return UNRELATED with low confidence.`}, {Role: "user", Content: string(encoded)}}, &chat.ChatOptions{Temperature: 0})
	if err != nil || response == nil {
		if err == nil {
			err = errors.New("empty event resolution response")
		}
		return contextEventResolution{}, err
	}
	var result contextEventMatch
	if err := common.ParseLLMJsonResponse(response.Content, &result); err != nil {
		return contextEventResolution{}, fmt.Errorf("invalid event resolution JSON: %w", err)
	}
	relation := normalizeContextEventRelation(result.Relation)
	if result.Confidence >= 0.9 && result.Confidence <= 1 && (relation == types.KnowledgeEventRelationSameOccurrence || relation == types.KnowledgeEventRelationSameStory) {
		for _, candidate := range candidates {
			if candidate.ID == result.EventID {
				return contextEventResolution{Event: candidate, Relation: relation}, nil
			}
		}
	}
	return contextEventResolution{Relation: types.KnowledgeEventRelationUnrelated}, nil
}

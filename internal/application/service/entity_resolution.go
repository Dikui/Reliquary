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
	"golang.org/x/text/unicode/norm"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func entityRecallLimit(variable string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(variable))
	if err != nil || value < 1 || value > 100 {
		return fallback
	}
	return value
}

func compatibleEntityTypes(entityType string) []string {
	return []string{types.NormalizeEntityType(entityType)}
}
func entityNameKey(entityType, name string) string {
	return fmt.Sprintf("%q:%q", types.NormalizeEntityType(entityType), normalizeEntityName(name))
}

var entityVersionNumber = regexp.MustCompile(`\d+(?:\.\d+)*`)

func entityIdentityVersionsCompatible(left, right string) bool {
	a, b := entityVersionNumber.FindAllString(left, -1), entityVersionNumber.FindAllString(right, -1)
	return len(a) == 0 || len(b) == 0 || strings.Join(a, ":") == strings.Join(b, ":")
}

func (s *EntityService) exactEntity(ctx context.Context, spaceID uint64, item entityResolutionInput) (types.Entity, error) {
	var entity types.Entity
	err := s.db.WithContext(ctx).Where("space_id = ? AND merged_into_id = '' AND lower(entity_type) IN ? AND normalized_name = ?", spaceID, compatibleEntityTypes(item.EntityType), normalizeEntityName(item.Mention)).Order("id").First(&entity).Error
	if err == nil {
		return entity, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return entity, err
	}
	err = s.db.WithContext(ctx).Model(&types.Entity{}).
		Joins("JOIN entity_aliases a ON a.space_id = entities.space_id AND a.entity_id = entities.id").
		Where("entities.space_id = ? AND entities.merged_into_id = '' AND lower(a.entity_type) IN ? AND a.normalized_alias = ?", spaceID, compatibleEntityTypes(item.EntityType), normalizeEntityName(item.Mention)).
		Order("entities.id").First(&entity).Error
	return entity, err
}

type entityScored struct {
	types.Entity
	RecallScore float64 `gorm:"column:recall_score"`
}

func (s *EntityService) recallEntities(ctx context.Context, spaceID uint64, item entityResolutionInput, vector types.EntityNameEmbedding, modelID string, limit int) ([]types.Entity, error) {
	names := []string{normalizeEntityName(item.Mention), normalizeEntityName(item.Canonical)}
	var namesResult []entityScored
	err := s.db.WithContext(ctx).Raw(`WITH scores AS (
 SELECT id AS entity_id, GREATEST(similarity(normalized_name, ?), similarity(normalized_name, ?)) AS score
 FROM entities WHERE space_id=? AND merged_into_id='' AND lower(entity_type) IN ? AND (normalized_name % ? OR normalized_name % ?)
 UNION ALL
 SELECT a.entity_id, GREATEST(similarity(a.normalized_alias, ?), similarity(a.normalized_alias, ?)) AS score
 FROM entity_aliases a JOIN entities matched ON matched.space_id=a.space_id AND matched.id=a.entity_id
 WHERE a.space_id=? AND matched.merged_into_id='' AND lower(matched.entity_type) IN ? AND (a.normalized_alias % ? OR a.normalized_alias % ?))
 SELECT e.*, MAX(scores.score) AS recall_score FROM scores JOIN entities e ON e.id=scores.entity_id AND e.space_id=?
 GROUP BY e.id ORDER BY recall_score DESC,e.id LIMIT ?`, names[0], names[1], spaceID, compatibleEntityTypes(item.EntityType), names[0], names[1], names[0], names[1], spaceID, compatibleEntityTypes(item.EntityType), names[0], names[1], spaceID, limit).Scan(&namesResult).Error
	if err != nil {
		return nil, err
	}
	scores := map[string]entityScored{}
	for _, candidate := range namesResult {
		scores[candidate.ID] = candidate
	}
	if len(vector) > 0 && modelID != "" {
		var vectorResult []entityScored
		encoded, _ := json.Marshal(vector)
		// MATERIALIZED isolates valid dimensions before evaluating pgvector distance.
		err = s.db.WithContext(ctx).Raw(`WITH compatible AS MATERIALIZED (
   SELECT * FROM entities WHERE space_id=? AND merged_into_id='' AND lower(entity_type) IN ?
    AND embedding_model_id=? AND embedding_dimension=? AND vector_dims(name_embedding)=? AND NOT embedding_needs_rebuild)
   SELECT compatible.*, 1 - (name_embedding <=> ?::vector) AS recall_score FROM compatible
   ORDER BY name_embedding <=> ?::vector,id LIMIT ?`, spaceID, compatibleEntityTypes(item.EntityType), modelID, len(vector), len(vector), string(encoded), string(encoded), limit).Scan(&vectorResult).Error
		if err != nil {
			return nil, err
		}
		for _, candidate := range vectorResult {
			if old, ok := scores[candidate.ID]; !ok || candidate.RecallScore > old.RecallScore {
				scores[candidate.ID] = candidate
			}
		}
	}
	merged := make([]entityScored, 0, len(scores))
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
	candidates := make([]types.Entity, 0, len(merged))
	ids := []string{}
	for _, candidate := range merged {
		candidates = append(candidates, candidate.Entity)
		ids = append(ids, candidate.ID)
	}
	if len(ids) > 0 {
		var aliases []types.EntityAlias
		if err := s.db.WithContext(ctx).Where("space_id=? AND entity_id IN ?", spaceID, ids).Find(&aliases).Error; err != nil {
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

func entityNameEmbeddingText(canonical, entityType, description string) string {
	return strings.Join([]string{strings.TrimSpace(canonical), normalizeEntityName(entityType), strings.TrimSpace(description)}, "\n")
}

func (s *EntityService) entityNameEmbedder(ctx context.Context, spaceID uint64) (embedding.Embedder, string, string, error) {
	if s.models == nil {
		return nil, "", "", nil
	}
	profile, err := NewEntityNameEmbeddingRebuilder(s.db, s.models, nil).profile(ctx, spaceID)
	if err != nil {
		return nil, "", "", nil
	}
	return profile.embedder, profile.modelID, profile.hash, nil
}

func (s *EntityService) resolveEntities(ctx context.Context, model chat.Chat, spaceID uint64, entryID string, input []entityResolutionInput) ([]types.Entity, map[string]types.Entity, error) {
	ctx = types.WithModelCallEntry(ctx, entryID)
	embedder, embeddingModelID, embeddingHash, err := s.entityNameEmbedder(ctx, spaceID)
	if err != nil {
		return nil, nil, err
	}
	touched := map[string]types.Entity{}
	localNames := map[string]string{}
	mentions := map[string]types.Entity{}
	limit := entityRecallLimit("RELIQUARY_ENTITY_TOP_K", 20)
	for _, item := range input {
		item.EntityType = types.NormalizeEntityType(item.EntityType)
		item.Mention = strings.TrimSpace(item.Mention)
		item.Canonical = strings.TrimSpace(item.Canonical)
		item.Description = strings.TrimSpace(item.Description)
		if item.Canonical == "" {
			item.Canonical = item.Mention
		}
		mentionKey := normalizeEntityName(item.Mention)
		if mentionKey == "" {
			continue
		}
		entity := types.Entity{}
		method := "exact"
		if id, ok := localNames[entityNameKey(item.EntityType, item.Mention)]; ok {
			entity = touched[id]
		}
		if entity.ID == "" {
			entity, err = s.exactEntity(ctx, spaceID, item)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil, err
			}
		}
		if entity.ID == "" {
			var vector types.EntityNameEmbedding
			if embedder != nil {
				embedCtx := types.WithLLMCallMetadata(types.WithModelTier(ctx, types.ModelTierEmbedding), "entity_embedding", "")
				types.RecordModelCall(embedCtx, embeddingModelID)
				value, embedErr := embedder.Embed(embedCtx, entityNameEmbeddingText(item.Canonical, item.EntityType, item.Description))
				if embedErr == nil && len(value) == embedder.GetDimensions() {
					vector = value
				}
			}
			candidates, recallErr := s.recallEntities(ctx, spaceID, item, vector, embeddingModelID, limit)
			if recallErr != nil {
				return nil, nil, recallErr
			}
			decision, judgeErr := s.judgeEntity(ctx, model, item, candidates)
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
				if !entityIdentityVersionsCompatible(item.Mention, canonical) {
					canonical = strings.TrimSpace(item.Mention)
				}
				for _, candidate := range candidates {
					if candidate.NormalizedName == normalizeEntityName(canonical) {
						canonical = strings.TrimSpace(item.Mention)
						break
					}
				}
				if embedder != nil && canonical != strings.TrimSpace(item.Canonical) {
					embedCtx := types.WithLLMCallMetadata(types.WithModelTier(ctx, types.ModelTierEmbedding), "entity_embedding", "")
					types.RecordModelCall(embedCtx, embeddingModelID)
					value, embedErr := embedder.Embed(embedCtx, entityNameEmbeddingText(canonical, item.EntityType, item.Description))
					vector = nil
					if embedErr == nil && len(value) == embedder.GetDimensions() {
						vector = value
					}
				}
				entity = types.Entity{ID: uuid.NewString(), SpaceID: spaceID, CanonicalName: canonical, NormalizedName: normalizeEntityName(canonical), EntityType: normalizeEntityName(item.EntityType), Description: strings.TrimSpace(item.Description), CreatedAt: time.Now(), UpdatedAt: time.Now(), NameEmbedding: vector, NameEmbeddingProfileHash: embeddingHash, EmbeddingModelID: embeddingModelID, EmbeddingDimension: len(vector), EmbeddingNeedsRebuild: len(vector) == 0}
				// A distinct source spelling still receives its own judgment. Equal
				// canonical identities in this message share one creation plan afterward.
				if id, ok := localNames[entityNameKey(item.EntityType, canonical)]; ok && decision.Decision == "new" && entityIdentityVersionsCompatible(item.Mention, canonical) {
					entity = touched[id]
				}
				if decision.Decision == "uncertain" {
					candidateIDs := decision.CandidateIDs
					if decision.EntityID != "" {
						candidateIDs = appendUniqueEntityString(candidateIDs, decision.EntityID)
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
						suggestion := types.EntityMergeSuggestion{ID: uuid.NewString(), SpaceID: spaceID, EntryID: entryID, NewEntityID: entity.ID, CandidateEntityID: candidate.ID, Reason: decision.Reason, Status: "pending", CreatedAt: time.Now()}
						if suggestion.Reason == "" {
							suggestion.Reason = "Entity identity remains uncertain after candidate judgment."
						}
						entity.MergeSuggestions = append(entity.MergeSuggestions, suggestion)
					}
				}
			}
		}
		if previous, ok := touched[entity.ID]; ok {
			entity.AliasResolutions = previous.AliasResolutions
			suggestions := append([]types.EntityMergeSuggestion{}, previous.MergeSuggestions...)
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
			entity.Aliases = unionEntityStrings(previous.Aliases, entity.Aliases)
		}
		aliases := []string{item.Mention}
		aliases = append(aliases, item.Aliases...)
		for _, aliasText := range aliases {
			alias := types.EntityAlias{Alias: strings.TrimSpace(aliasText), NormalizedAlias: normalizeEntityName(aliasText), ResolutionMethod: method}
			entity.AliasResolutions = append(entity.AliasResolutions, alias)
			entity.Aliases = appendUniqueEntityString(entity.Aliases, aliasText)
			localNames[entityNameKey(entity.EntityType, aliasText)] = entity.ID
		}
		localNames[entityNameKey(entity.EntityType, entity.CanonicalName)] = entity.ID
		mentions[mentionKey] = entity
		touched[entity.ID] = entity
	}
	resolved := make([]types.Entity, 0, len(touched))
	for _, entity := range touched {
		resolved = append(resolved, entity)
	}
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].ID < resolved[j].ID })
	return resolved, mentions, nil
}

func (s *EntityService) judgeEntity(ctx context.Context, model chat.Chat, item entityResolutionInput, candidates []types.Entity) (entityMatch, error) {
	ctx = types.WithLLMCallMetadata(ctx, "entity_judgment", "")
	if model != nil {
		types.RecordModelCall(ctx, model.GetModelID())
	}
	if limit := entityRecallLimit("RELIQUARY_ENTITY_TOP_K", 20); len(candidates) > limit {
		candidates = candidates[:limit]
	}
	if model == nil {
		return entityMatch{}, errors.New("entity judgment model is unavailable")
	}
	payload := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		payload = append(payload, map[string]any{"id": candidate.ID, "name": candidate.CanonicalName, "type": candidate.EntityType, "description": candidate.Description, "aliases": candidate.Aliases})
	}
	encoded, _ := json.Marshal(map[string]any{"mention": item.Mention, "canonical_name": item.Canonical, "entity_type": item.EntityType, "candidates": payload})
	response, err := model.Chat(ctx, []chat.Message{{Role: "system", Content: `Decide whether the source mention is the exact same real-world entity as one candidate. Similar names, different versions, or a shared topic do not prove identity. Return {"decision":"match|new|uncertain","entity_id":"listed ID for match, otherwise empty","candidate_ids":["listed IDs relevant to uncertainty"],"confidence":0.0,"reason":"brief reason"}. Match requires confidence at least 0.9. Empty candidates still require one decision and normally mean new.`}, {Role: "user", Content: string(encoded)}}, &chat.ChatOptions{Temperature: 0})
	if err != nil || response == nil {
		if err == nil {
			err = errors.New("empty entity resolution response")
		}
		return entityMatch{}, err
	}
	var result entityMatch
	if err := utils.ParseLLMJsonResponse(response.Content, &result); err != nil {
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
func persistEntities(tx *gorm.DB, entities []types.Entity) (map[string]string, error) {
	if len(entities) > 0 {
		if err := lockEntitySpaceShared(tx, entities[0].SpaceID); err != nil {
			return nil, err
		}
		keys := []string{}
		for _, entity := range entities {
			for _, name := range append([]string{entity.CanonicalName}, entity.Aliases...) {
				keys = append(keys, entityNameKey(entity.EntityType, name))
			}
		}
		if err := lockEntityIdentities(tx, entities[0].SpaceID, keys); err != nil {
			return nil, err
		}
	}
	ids := map[string]string{}
	for _, planned := range entities {
		entity := planned
		var existing types.Entity
		err := tx.Where("id=? AND space_id=?", entity.ID, entity.SpaceID).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = tx.Where("space_id=? AND lower(entity_type) IN ? AND normalized_name=?", entity.SpaceID, compatibleEntityTypes(entity.EntityType), entity.NormalizedName).Order("id").First(&existing).Error
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var alias types.EntityAlias
			names := []string{entity.NormalizedName}
			for _, name := range entity.Aliases {
				names = appendUniqueEntityString(names, normalizeEntityName(name))
			}
			aliasErr := tx.Where("space_id=? AND lower(entity_type) IN ? AND normalized_alias IN ?", entity.SpaceID, compatibleEntityTypes(entity.EntityType), names).Order("id").First(&alias).Error
			if aliasErr == nil {
				err = tx.Where("space_id=? AND id=?", entity.SpaceID, alias.EntityID).First(&existing).Error
			} else if !errors.Is(aliasErr, gorm.ErrRecordNotFound) {
				return nil, aliasErr
			}
		}
		if err == nil {
			for existing.MergedIntoID != "" {
				next := existing.MergedIntoID
				existing = types.Entity{}
				if err := tx.Where("space_id=? AND id=?", entity.SpaceID, next).First(&existing).Error; err != nil {
					return nil, err
				}
			}
			entity = existing
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		} else {
			if len(entity.NameEmbedding) > 0 {
				rebuilder := NewEntityNameEmbeddingRebuilder(tx, nil, nil)
				profile := entityEmbeddingProfile{modelID: entity.EmbeddingModelID, dimension: entity.EmbeddingDimension, hash: entity.NameEmbeddingProfileHash}
				if entity.NameEmbeddingProfileHash == "" || rebuilder.checkProfile(tx, entity.SpaceID, profile) != nil {
					entity.EmbeddingNeedsRebuild = true
				}
			}
			created := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "space_id"}, {Name: "entity_type"}, {Name: "normalized_name"}}, DoNothing: true}).Create(&entity)
			if created.Error != nil {
				return nil, created.Error
			}
			if created.RowsAffected > 0 {
				if err := notifyEntityProjection(tx, entity.SpaceID, entity.ID); err != nil {
					return nil, err
				}
			}
			if err := tx.Where("space_id=? AND entity_type=? AND normalized_name=?", entity.SpaceID, entity.EntityType, entity.NormalizedName).First(&existing).Error; err != nil {
				return nil, err
			}
			entity = existing
		}
		ids[planned.ID] = entity.ID
		resolutions := map[string]types.EntityAlias{}
		for _, alias := range planned.AliasResolutions {
			resolutions[alias.NormalizedAlias] = alias
		}
		seen := map[string]bool{}
		for _, name := range append([]string{planned.CanonicalName}, planned.Aliases...) {
			key := normalizeEntityName(name)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			var collision types.EntityAlias
			found := tx.Where("space_id=? AND lower(entity_type) IN ? AND normalized_alias=?", entity.SpaceID, compatibleEntityTypes(entity.EntityType), key).First(&collision).Error
			if found == nil {
				continue
			}
			if !errors.Is(found, gorm.ErrRecordNotFound) {
				return nil, found
			}
			alias := resolutions[key]
			alias.ID = uuid.NewString()
			alias.SpaceID = entity.SpaceID
			alias.EntityType = entity.EntityType
			alias.EntityID = entity.ID
			alias.Alias = strings.TrimSpace(name)
			alias.NormalizedAlias = key
			alias.CreatedAt = time.Now()
			if alias.ResolutionMethod == "" {
				alias.ResolutionMethod = "exact"
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "space_id"}, {Name: "entity_type"}, {Name: "normalized_alias"}}, DoNothing: true}).Create(&alias).Error; err != nil {
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
			var candidate types.Entity
			if err := tx.Where("space_id=? AND id=?", suggestion.SpaceID, suggestion.CandidateEntityID).First(&candidate).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			} else if err != nil {
				return nil, err
			}
			for candidate.MergedIntoID != "" {
				next := candidate.MergedIntoID
				if err := tx.Where("space_id=? AND id=?", suggestion.SpaceID, next).First(&candidate).Error; err != nil {
					return nil, err
				}
			}
			suggestion.CandidateEntityID = candidate.ID
			if suggestion.NewEntityID == suggestion.CandidateEntityID {
				continue
			}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "space_id"}, {Name: "new_entity_id"}, {Name: "candidate_entity_id"}, {Name: "entry_id"}}, DoNothing: true}).Create(&suggestion).Error; err != nil {
				return nil, err
			}
		}
	}
	return ids, nil
}

type entityResolutionInput struct {
	Mention     string
	Canonical   string
	EntityType  string
	Description string
	Aliases     []string
}
type entityMatch struct {
	Decision     string   `json:"decision"`
	EntityID     string   `json:"entity_id"`
	CandidateIDs []string `json:"candidate_ids"`
	Confidence   float64  `json:"confidence"`
	Reason       string   `json:"reason"`
}

func normalizeEntityName(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(value)), " "))
}
func appendUniqueEntityString(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}
func unionEntityStrings(left, right []string) []string {
	result := append([]string{}, left...)
	for _, value := range right {
		result = appendUniqueEntityString(result, value)
	}
	return result
}
func entityConfiguredPositive(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

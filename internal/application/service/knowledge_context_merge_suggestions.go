package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (s *KnowledgeContextService) ListMergeSuggestions(ctx context.Context, kbID string, q types.KnowledgeContextQuery, status string) ([]types.KnowledgeEntityMergeSuggestion, error) {
	scope, err := s.resolveContextQueryScope(ctx, kbID, q)
	if err != nil {
		return nil, err
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "accepted" && status != "rejected" && status != "all" {
		return nil, werrors.NewBadRequestError("status must be pending, accepted, rejected or all")
	}
	items := []types.KnowledgeEntityMergeSuggestion{}
	if len(scope.kbIDs) == 0 {
		return items, nil
	}
	query := s.db.WithContext(ctx).Model(&types.KnowledgeEntityMergeSuggestion{}).
		Select("knowledge_context_merge_suggestions.*, fresh.canonical_name AS new_entity_name, candidate.canonical_name AS candidate_entity_name").
		Joins("JOIN knowledge_context_entities fresh ON fresh.tenant_id=knowledge_context_merge_suggestions.tenant_id AND fresh.id=knowledge_context_merge_suggestions.new_entity_id").
		Joins("JOIN knowledge_context_entities candidate ON candidate.tenant_id=knowledge_context_merge_suggestions.tenant_id AND candidate.id=knowledge_context_merge_suggestions.candidate_entity_id").
		Where("knowledge_context_merge_suggestions.tenant_id=? AND knowledge_context_merge_suggestions.knowledge_base_id IN ?", scope.tenantID, scope.kbIDs)
	if status != "all" {
		query = query.Where("knowledge_context_merge_suggestions.status=?", status)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	err = query.Order("knowledge_context_merge_suggestions.created_at DESC,knowledge_context_merge_suggestions.id").Limit(limit).Offset(q.Offset).Scan(&items).Error
	return items, err
}

func (s *KnowledgeContextService) DecideMergeSuggestion(ctx context.Context, suggestionID string, accept bool) (*types.KnowledgeEntityMergeSuggestion, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated merge suggestion editor required")
	}
	var item types.KnowledgeEntityMergeSuggestion
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockContextTeam(tx, tenantID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", suggestionID, tenantID).First(&item).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return werrors.NewNotFoundError("merge suggestion not found")
		} else if err != nil {
			return err
		}

		target := "rejected"
		if accept {
			target = "accepted"
		}
		if item.Status == target {
			return nil
		}
		if item.Status != "pending" {
			return werrors.NewBadRequestError("merge suggestion has already been decided")
		}
		if accept {
			nested := *s
			nested.db = tx
			if err := nested.MergeEntities(ctx, "", item.CandidateEntityID, item.NewEntityID); err != nil {
				return err
			}
		}
		now := time.Now()
		item.Status = target
		item.DecidedAt = &now
		return tx.Model(&item).Updates(map[string]any{"status": target, "decided_at": now}).Error
	})
	if err == nil && accept && s.nameEmbeddingRebuilder != nil {
		err = s.nameEmbeddingRebuilder.Enqueue(ctx, tenantID, "entity_merge")
	}
	return &item, err
}

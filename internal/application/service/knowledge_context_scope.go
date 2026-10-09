package service

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type contextQueryScope struct {
	tenantID uint64
	kind     string
	kbIDs    []string
	domains  map[string]*types.KnowledgeDomain
	names    map[string]string
}

func (s *KnowledgeContextService) resolveContextQueryScope(ctx context.Context, kbID string, q types.KnowledgeContextQuery) (*contextQueryScope, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}
	scope := types.NormalizeKnowledgeContextScope(q.Scope)
	if scope == "" {
		if kbID != "" || q.KnowledgeBaseID != "" {
			scope = "knowledge_base"
		} else {
			scope = "all"
		}
	}
	query := s.db.WithContext(ctx).Model(&types.KnowledgeBase{}).Select("id", "name", "domain_id").Where("tenant_id = ?", tenantID)
	switch scope {
	case "knowledge_base":
		if q.KnowledgeBaseID != "" {
			kbID = q.KnowledgeBaseID
		}
		if _, err := s.scopedKnowledgeBase(ctx, kbID); err != nil {
			return nil, err
		}
		query = query.Where("id = ?", kbID)
	case "domain":
		if strings.TrimSpace(q.DomainID) == "" {
			return nil, werrors.NewBadRequestError("domain_id is required for domain scope")
		}
		var domain types.KnowledgeDomain
		if err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", q.DomainID, tenantID).First(&domain).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, werrors.NewNotFoundError("knowledge domain not found")
			}
			return nil, err
		}
		query = query.Where("domain_id = ?", q.DomainID)
	case "all":
	default:
		return nil, werrors.NewBadRequestError("scope must be knowledge_base, domain or all")
	}

	var bases []types.KnowledgeBase
	if err := query.Find(&bases).Error; err != nil {
		return nil, err
	}
	resolved := &contextQueryScope{tenantID: tenantID, kind: scope, domains: map[string]*types.KnowledgeDomain{}, names: map[string]string{}}
	byDomain := map[string]*types.KnowledgeDomain{}
	for _, base := range bases {
		domain := byDomain[base.DomainID]
		if domain == nil {
			domain, err = getKnowledgeDomain(ctx, s.db, tenantID, base.DomainID, false)
			if err != nil {
				return nil, err
			}
			byDomain[base.DomainID] = domain
		}
		resolved.kbIDs = append(resolved.kbIDs, base.ID)
		resolved.domains[base.ID] = domain
		resolved.names[base.ID] = base.Name
	}
	return resolved, nil
}

// A field's aliases belong to its source domain, even during an all-knowledge-base query.
func (scope *contextQueryScope) factFields(query *gorm.DB, q types.KnowledgeContextQuery) *gorm.DB {
	if q.Predicate == "" && q.Relation == "" {
		return query
	}
	var clauses []string
	var args []any
	for _, kbID := range scope.kbIDs {
		clause := "(knowledge_base_id = ?"
		args = append(args, kbID)
		if q.Predicate != "" {
			clause += " AND regexp_replace(lower(trim(predicate)), '[[:space:]]+', ' ', 'g') IN ?"
			args = append(args, contextFieldNames(scope.domains[kbID], q.Predicate))
		}
		if q.Relation != "" {
			clause += " AND kind = ? AND regexp_replace(lower(trim(predicate)), '[[:space:]]+', ' ', 'g') IN ?"
			args = append(args, types.KnowledgeFactRelation, contextFieldNames(scope.domains[kbID], q.Relation))
		}
		clauses = append(clauses, clause+")")
	}
	if len(clauses) == 0 {
		return query.Where("false")
	}
	return query.Where("("+strings.Join(clauses, " OR ")+")", args...)
}

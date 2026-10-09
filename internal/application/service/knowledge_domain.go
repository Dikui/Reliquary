package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type KnowledgeDomainService struct {
	db                     *gorm.DB
	nameEmbeddingRebuilder ContextNameEmbeddingRebuildTrigger
}

func NewKnowledgeDomainService(db *gorm.DB) *KnowledgeDomainService {
	return &KnowledgeDomainService{db: db}
}

func knowledgeDomainAccess(ctx context.Context, write bool) (uint64, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID != types.SingleTenantID {
		return 0, apperrors.NewUnauthorizedError("personal workspace context required")
	}
	return tenantID, nil
}

func (s *KnowledgeDomainService) List(ctx context.Context) ([]types.KnowledgeDomain, error) {
	tenantID, err := knowledgeDomainAccess(ctx, false)
	if err != nil {
		return nil, err
	}
	items := []types.KnowledgeDomain{}
	err = s.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("is_builtin DESC, name ASC, id ASC").Find(&items).Error
	return items, err
}

func getKnowledgeDomain(ctx context.Context, db *gorm.DB, tenantID uint64, id string, lock bool) (*types.KnowledgeDomain, error) {
	query := db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var domain types.KnowledgeDomain
	if err := query.First(&domain).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.NewNotFoundError("knowledge domain not found")
		}
		return nil, err
	}
	return &domain, nil
}

func (s *KnowledgeDomainService) Get(ctx context.Context, id string) (*types.KnowledgeDomain, error) {
	tenantID, err := knowledgeDomainAccess(ctx, false)
	if err != nil {
		return nil, err
	}
	return getKnowledgeDomain(ctx, s.db, tenantID, id, false)
}

// GetDomainForKnowledgeBase is used by extraction after its KB access check.
func (s *KnowledgeDomainService) GetDomainForKnowledgeBase(ctx context.Context, tenantID uint64, kbID string) (*types.KnowledgeDomain, error) {
	var domainID string
	if err := s.db.WithContext(ctx).Model(&types.KnowledgeBase{}).Where("tenant_id = ? AND id = ?", tenantID, kbID).Select("domain_id").Row().Scan(&domainID); err != nil {
		return nil, err
	}
	return getKnowledgeDomain(ctx, s.db, tenantID, domainID, false)
}

func (s *KnowledgeDomainService) Create(ctx context.Context, input types.KnowledgeDomain) (*types.KnowledgeDomain, error) {
	tenantID, err := knowledgeDomainAccess(ctx, true)
	if err != nil {
		return nil, err
	}
	if err := ValidateModelTierConfig(ctx, s.db, input.ModelTiers); err != nil {
		return nil, err
	}
	if err := validateKnowledgeDomain(&input); err != nil {
		return nil, err
	}
	input.ID, input.TenantID = uuid.NewString(), tenantID
	input.IsBuiltin = false
	input.CreatedBy = ""
	if userID, ok := types.UserIDFromContext(ctx); ok {
		input.CreatedBy = userID
	}
	input.CreatedAt, input.UpdatedAt = time.Time{}, time.Time{}
	input.DeletedAt = gorm.DeletedAt{}
	if err := s.db.WithContext(ctx).Create(&input).Error; err != nil {
		return nil, knowledgeDomainWriteError(err)
	}
	if s.nameEmbeddingRebuilder != nil && input.ModelTiers.Embedding != "" {
		if err := s.nameEmbeddingRebuilder.MarkAndEnqueue(ctx, tenantID, "domain_embedding_override_changed"); err != nil {
			return &input, fmt.Errorf("domain saved but name embedding rebuild could not be queued: %w", err)
		}
	}
	return &input, nil
}

func domainUpdateColumns() []string {
	return []string{"name", "description", "entity_types", "fields", "relations", "unit_families", "extraction_hints", "model_tiers", "updated_at"}
}

func (s *KnowledgeDomainService) Update(ctx context.Context, id string, input types.KnowledgeDomain) (*types.KnowledgeDomain, error) {
	tenantID, err := knowledgeDomainAccess(ctx, true)
	if err != nil {
		return nil, err
	}
	if err := ValidateModelTierConfig(ctx, s.db, input.ModelTiers); err != nil {
		return nil, err
	}
	if err := validateKnowledgeDomain(&input); err != nil {
		return nil, err
	}
	var output *types.KnowledgeDomain
	embeddingChanged := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, err := getKnowledgeDomain(ctx, tx, tenantID, id, true)
		if err != nil {
			return err
		}
		embeddingChanged = existing.ModelTiers.ModelID(types.ModelTierEmbedding) != input.ModelTiers.ModelID(types.ModelTierEmbedding)
		input.ID, input.TenantID = existing.ID, existing.TenantID
		input.IsBuiltin, input.CreatedBy, input.CreatedAt = existing.IsBuiltin, existing.CreatedBy, existing.CreatedAt
		input.UpdatedAt, input.DeletedAt = time.Time{}, existing.DeletedAt
		if err := tx.Model(existing).Select(domainUpdateColumns()).Updates(&input).Error; err != nil {
			return knowledgeDomainWriteError(err)
		}
		output = &input
		return nil
	})
	if err == nil && embeddingChanged && s.nameEmbeddingRebuilder != nil {
		if enqueueErr := s.nameEmbeddingRebuilder.MarkAndEnqueue(ctx, tenantID, "domain_embedding_override_changed"); enqueueErr != nil {
			return output, fmt.Errorf("domain saved but name embedding rebuild could not be queued: %w", enqueueErr)
		}
	}
	return output, err
}

func (s *KnowledgeDomainService) Delete(ctx context.Context, id string) error {
	tenantID, err := knowledgeDomainAccess(ctx, true)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		domain, err := getKnowledgeDomain(ctx, tx, tenantID, id, true)
		if err != nil {
			return err
		}
		if domain.IsBuiltin {
			return apperrors.NewConflictError("built-in knowledge domains cannot be deleted")
		}
		var count int64
		if err := tx.Model(&types.KnowledgeBase{}).Where("tenant_id = ? AND domain_id = ?", tenantID, id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return apperrors.NewKnowledgeDomainReferencedError(
				fmt.Sprintf("knowledge domain is referenced by %d knowledge bases", count),
				types.KnowledgeDomainReferenceDetails{KnowledgeBaseCount: count},
			)
		}
		return tx.Delete(domain).Error
	})
}

func knowledgeDomainWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return apperrors.NewConflictError("knowledge domain name already exists in the space")
	}
	return err
}

func validateKnowledgeDomain(domain *types.KnowledgeDomain) error {
	domain.Name = strings.TrimSpace(domain.Name)
	if domain.Name == "" || len([]rune(domain.Name)) > 255 {
		return apperrors.NewBadRequestError("domain name is required and must contain at most 255 characters")
	}
	if domain.EntityTypes == nil {
		domain.EntityTypes = []string{}
	}
	if domain.Fields == nil {
		domain.Fields = []types.DomainField{}
	}
	if domain.Relations == nil {
		domain.Relations = []types.DomainRelation{}
	}
	if domain.UnitFamilies == nil {
		domain.UnitFamilies = []types.DomainUnitFamily{}
	}
	entityTypes := map[string]bool{}
	for i, value := range domain.EntityTypes {
		value = strings.TrimSpace(value)
		key := normalizeContextName(value)
		if key == "" || entityTypes[key] {
			return apperrors.NewBadRequestError("object types must be nonempty and unique")
		}
		entityTypes[key], domain.EntityTypes[i] = true, value
	}
	families := map[string]bool{}
	unitNames := map[string]string{}
	for i := range domain.UnitFamilies {
		family := &domain.UnitFamilies[i]
		family.Key, family.CanonicalUnit = strings.TrimSpace(family.Key), strings.TrimSpace(family.CanonicalUnit)
		if family.Key == "" || families[family.Key] || family.CanonicalUnit == "" {
			return apperrors.NewBadRequestError("unit families require unique keys and a canonical unit")
		}
		families[family.Key] = true
		canonical := false
		for j := range family.Units {
			unit := &family.Units[j]
			unit.Name = strings.TrimSpace(unit.Name)
			if unit.Name == "" || unit.Factor <= 0 || math.IsNaN(unit.Factor) || math.IsInf(unit.Factor, 0) {
				return apperrors.NewBadRequestError("unit names and finite positive conversion factors are required")
			}
			if normalizeContextName(unit.Name) == normalizeContextName(family.CanonicalUnit) && unit.Factor == 1 {
				canonical = true
			}
			for _, name := range append([]string{unit.Name}, unit.Aliases...) {
				key := normalizeContextName(name)
				identity := fmt.Sprintf("%s/%d", family.Key, j)
				if key == "" || (unitNames[key] != "" && unitNames[key] != identity) {
					return apperrors.NewBadRequestError("unit names and aliases must identify one unit")
				}
				unitNames[key] = identity
			}
			if unit.Aliases == nil {
				unit.Aliases = []string{}
			}
		}
		if !canonical {
			return apperrors.NewBadRequestError("canonical unit must have conversion factor 1")
		}
	}
	keys, names := map[string]bool{}, map[string]string{}
	claim := func(key, label string, aliases []string) error {
		if key == "" || keys[normalizeContextName(key)] {
			return apperrors.NewBadRequestError("field and relation keys must be nonempty and unique")
		}
		keys[normalizeContextName(key)] = true
		for _, name := range append([]string{key, label}, aliases...) {
			name = normalizeContextName(name)
			if name == "" {
				continue
			}
			if previous := names[name]; previous != "" && previous != key {
				return apperrors.NewBadRequestError("field and relation aliases must identify one template key")
			}
			names[name] = key
		}
		return nil
	}
	validateTypes := func(values []string) error {
		for _, value := range values {
			if !entityTypes[normalizeContextName(value)] {
				return apperrors.NewBadRequestError("field or relation references an unknown object type")
			}
		}
		return nil
	}
	for i := range domain.Fields {
		field := &domain.Fields[i]
		field.Key, field.Label, field.UnitFamily = strings.TrimSpace(field.Key), strings.TrimSpace(field.Label), strings.TrimSpace(field.UnitFamily)
		if field.Label == "" {
			field.Label = field.Key
		}
		if err := claim(field.Key, field.Label, field.Aliases); err != nil {
			return err
		}
		switch field.ValueType {
		case "number", "text", "date", "boolean":
		default:
			return apperrors.NewBadRequestError("field value_type must be number, text, date or boolean")
		}
		if field.UnitFamily != "" && (!families[field.UnitFamily] || field.ValueType != "number") {
			return apperrors.NewBadRequestError("numeric fields must reference an existing unit family")
		}
		if err := validateTypes(field.EntityTypes); err != nil {
			return err
		}
		if field.Aliases == nil {
			field.Aliases = []string{}
		}
		if field.EntityTypes == nil {
			field.EntityTypes = []string{}
		}
	}
	for i := range domain.Relations {
		relation := &domain.Relations[i]
		relation.Key, relation.Label = strings.TrimSpace(relation.Key), strings.TrimSpace(relation.Label)
		if relation.Label == "" {
			relation.Label = relation.Key
		}
		if err := claim(relation.Key, relation.Label, relation.Aliases); err != nil {
			return err
		}
		if err := validateTypes(relation.SubjectTypes); err != nil {
			return err
		}
		if err := validateTypes(relation.ObjectTypes); err != nil {
			return err
		}
		if relation.Aliases == nil {
			relation.Aliases = []string{}
		}
		if relation.SubjectTypes == nil {
			relation.SubjectTypes = []string{}
		}
		if relation.ObjectTypes == nil {
			relation.ObjectTypes = []string{}
		}
	}
	return nil
}

func (s *KnowledgeDomainService) OpenFields(ctx context.Context, id string) ([]types.DomainOpenField, error) {
	domain, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	mapped := map[string]bool{}
	for _, field := range domain.Fields {
		for _, name := range append([]string{field.Key, field.Label}, field.Aliases...) {
			mapped[normalizeContextName(name)] = true
		}
	}
	base := func() *gorm.DB {
		query := s.db.WithContext(ctx).Table("knowledge_context_facts AS f").
			Joins("JOIN knowledge_bases AS kb ON kb.id = f.knowledge_base_id AND kb.tenant_id = f.tenant_id AND kb.deleted_at IS NULL").
			Joins("JOIN knowledge_context_evidence AS ev ON ev.id = f.evidence_id AND ev.tenant_id = f.tenant_id").
			Where("f.tenant_id = ? AND kb.domain_id = ? AND f.kind = ? AND f.status = ? AND ev.status = ?", domain.TenantID, id, types.KnowledgeFactAttribute, types.KnowledgeFactCurrent, types.KnowledgeEvidenceCurrent)

		return query
	}
	const nameSQL = "COALESCE(NULLIF(f.original_predicate, ''), f.predicate)"
	var counts []struct {
		Name  string
		Count int64
	}
	if err := base().Select(nameSQL + " AS name, COUNT(*) AS count").Group(nameSQL).Order("count DESC, name ASC").Scan(&counts).Error; err != nil {
		return nil, err
	}
	items := []types.DomainOpenField{}
	for _, count := range counts {
		if mapped[normalizeContextName(count.Name)] {
			continue
		}
		item := types.DomainOpenField{Name: count.Name, Count: count.Count, ExampleFacts: []types.KnowledgeFact{}}
		if err := base().Where(nameSQL+" = ?", count.Name).Select("f.*").Order("f.created_at DESC, f.id ASC").Limit(3).Scan(&item.ExampleFacts).Error; err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *KnowledgeDomainService) editField(ctx context.Context, id string, edit func(*types.KnowledgeDomain) error) (*types.KnowledgeDomain, error) {
	tenantID, err := knowledgeDomainAccess(ctx, true)
	if err != nil {
		return nil, err
	}
	var output *types.KnowledgeDomain
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		domain, err := getKnowledgeDomain(ctx, tx, tenantID, id, true)
		if err != nil {
			return err
		}
		if err := edit(domain); err != nil {
			return err
		}
		if err := validateKnowledgeDomain(domain); err != nil {
			return err
		}
		if err := tx.Model(domain).Select("fields", "updated_at").Updates(domain).Error; err != nil {
			return err
		}
		output = domain
		return nil
	})
	return output, err
}

func (s *KnowledgeDomainService) MapField(ctx context.Context, id string, mapping types.DomainFieldMapping) (*types.KnowledgeDomain, error) {
	mapping.OpenField = strings.TrimSpace(mapping.OpenField)
	if mapping.OpenField == "" {
		return nil, apperrors.NewBadRequestError("open_field is required")
	}
	return s.editField(ctx, id, func(domain *types.KnowledgeDomain) error {
		for i := range domain.Fields {
			field := &domain.Fields[i]
			if field.Key != mapping.FieldKey {
				continue
			}
			for _, alias := range append([]string{field.Key, field.Label}, field.Aliases...) {
				if normalizeContextName(alias) == normalizeContextName(mapping.OpenField) {
					return nil
				}
			}
			field.Aliases = append(field.Aliases, mapping.OpenField)
			return nil
		}
		return apperrors.NewBadRequestError("target template field does not exist")
	})
}

func (s *KnowledgeDomainService) AddField(ctx context.Context, id string, field types.DomainField) (*types.KnowledgeDomain, error) {
	return s.editField(ctx, id, func(domain *types.KnowledgeDomain) error { domain.Fields = append(domain.Fields, field); return nil })
}

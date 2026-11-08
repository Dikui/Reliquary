package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// ShelfGet shares composition and certification semantics with entity detail.
func (s *EntityService) ShelfGet(ctx context.Context, libraryID, id string, options types.ShelfReadOptions) (*types.ShelfNodeDetail, error) {
	var result *types.ShelfNodeDetail
	err := s.readShelfSnapshot(ctx, func(reader *EntityService) error {
		var err error
		result, err = reader.shelfGet(ctx, libraryID, id, options)
		return err
	})
	return result, err
}

func (s *EntityService) shelfGet(ctx context.Context, libraryID, id string, options types.ShelfReadOptions) (*types.ShelfNodeDetail, error) {
	library, err := s.shelfLibraryForRead(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	at := time.Now()
	if options.AsOf != nil {
		at = *options.AsOf
	}
	if at.IsZero() {
		return nil, apperrors.NewBadRequestError("as_of must be a nonzero time")
	}
	var entity types.Entity
	if err := s.db.WithContext(ctx).Where("id=? AND library_id=? AND space_id=?", id, library.ID, library.SpaceID).First(&entity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.NewNotFoundError("shelf node not found")
		}
		return nil, err
	}
	entity.KindLabel = managedKindLabel(library, entity.Kind)
	entities := []types.Entity{entity}
	if err := s.loadAliases(ctx, library.SpaceID, entities); err != nil {
		return nil, err
	}
	entity = entities[0]
	if err := hydrateManagedEntityAttributes(ctx, s.db, []*types.Entity{&entity}, at); err != nil {
		return nil, err
	}
	result := &types.ShelfNodeDetail{Entity: entity, AsOf: at, Facts: []types.ShelfFact{}}
	var count int64
	if err := s.db.WithContext(ctx).Model(&types.EntityAttributeVersion{}).Where("space_id=? AND entity_id=? AND effective_at<=?", library.SpaceID, id, at).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return result, nil
	}
	result.Recorded = true
	result.Shelf, err = s.shelfModelDetail(ctx, &result.Entity, at, options.Combination)
	if err != nil {
		return nil, err
	}
	var composition []types.ShelfComposition
	if result.Shelf != nil {
		composition = result.Shelf.Composition
	}
	ids := []string{id}
	for _, group := range composition {
		for _, part := range group.Parts {
			ids = append(ids, part.ID)
		}
	}
	if result.Shelf != nil {
		for _, cert := range result.Shelf.Certifications {
			ids = append(ids, cert.Entity.ID)
		}
	}
	var versions []types.EntityAttributeVersion
	if err := s.db.WithContext(ctx).Where("space_id=? AND entity_id IN ? AND effective_at<=?", library.SpaceID, ids, at).Order("effective_at DESC,revision DESC").Find(&versions).Error; err != nil {
		return nil, err
	}
	var logs []types.OperationLog
	if err := s.db.WithContext(ctx).Where("space_id=? AND object_id IN ?", library.SpaceID, ids).Order("created_at DESC,id").Find(&logs).Error; err != nil {
		return nil, err
	}
	appendFact := func(owner types.Entity, field string, value any, conditions []types.ShelfCondition, origin string) {
		label := "自身"
		if origin == "component" {
			label = "来自部件"
		}
		if origin == "ancestor" {
			label = "来自上层"
		}
		if owner.Kind == "part" && (field == "cluster_model" || field == "container_model") {
			label = "历史部件值，需逐型号确认"
		}
		if conditions == nil {
			conditions = []types.ShelfCondition{}
		}
		region := ""
		if status, ok := value.(types.ShelfSaleStatus); ok && field == "sale_statuses" {
			region = status.Region
		}
		result.Facts = append(result.Facts, types.ShelfFact{EntityID: owner.ID, Field: field, Value: value, Conditions: conditions,
			PartialOptions: options.Combination == nil && shelfConditionsPartial(conditions, composition), Origin: origin, OriginLabel: label,
			Evidence: shelfAttributeEvidence(owner.ID, field, region, versions, logs)})
	}
	keys := make([]string, 0, len(result.Entity.Attributes))
	for key := range result.Entity.Attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, field := range keys {
		if field == "certification" && result.Entity.Kind == "certification" {
			var record types.ShelfCertification
			if err := types.DecodeShelfValue(result.Entity.Attributes[field], &record); err != nil {
				return nil, err
			}
			appendFact(result.Entity, field, record, record.Conditions, "self")
		} else if result.Shelf != nil && field == "sale_statuses" {
			for _, row := range result.Shelf.SaleStatuses {
				appendFact(result.Entity, field, row, row.Conditions, "self")
			}
		} else if result.Shelf != nil && result.Shelf.Parameters[field] != nil {
			for _, row := range result.Shelf.Parameters[field] {
				appendFact(result.Entity, field, row.Value, row.Conditions, "self")
			}
		} else {
			appendFact(result.Entity, field, result.Entity.Attributes[field], nil, "self")
		}
	}
	for _, group := range composition {
		for _, part := range group.Parts {
			if selected := options.Combination[group.ModuleType]; selected != "" && selected != part.ID {
				continue
			}
			keys = keys[:0]
			for key := range part.Attributes {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, field := range keys {
				appendFact(part, field, part.Attributes[field], []types.ShelfCondition{{ModuleType: group.ModuleType, PartIDs: []string{part.ID}}}, "component")
			}
		}
	}
	certifications := map[string]types.ShelfCertification{}
	if result.Shelf != nil {
		for _, cert := range result.Shelf.Certifications {
			certifications[cert.Entity.ID] = cert.Certification
			conditions := shelfCertificationConditions(cert.Certification, composition)
			appendFact(cert.Entity, "certification", cert.Certification, conditions, cert.Origin)
		}
	}
	// Relationships are facts too, with their own effective interval and source.
	var relations []types.Relation
	if err := s.db.WithContext(ctx).Where("library_id=? AND space_id=? AND subject_id=? AND relation_type IN ('belongs_to','composed_of','has_certification') AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", library.ID, library.SpaceID, id, at, at).Order("relation_type,object_id,id").Find(&relations).Error; err != nil {
		return nil, err
	}
	if len(relations) > 0 {
		relationIDs := make([]string, len(relations))
		for i := range relations {
			relationIDs[i] = relations[i].ID
		}
		var relationLogs []types.OperationLog
		if err := s.db.WithContext(ctx).Where("space_id=? AND object_id IN ?", library.SpaceID, relationIDs).Order("created_at DESC,id").Find(&relationLogs).Error; err != nil {
			return nil, err
		}
		for _, relation := range relations {
			conditions := []types.ShelfCondition{}
			if relation.RelationType == "has_certification" {
				record, ok := certifications[relation.ObjectID]
				if !ok {
					continue
				}
				conditions = shelfCertificationConditions(record, composition)
			}
			if relation.RelationType == "composed_of" {
				module, _ := relation.Attributes["module_type"].(string)
				if options.Combination != nil && options.Combination[module] != "" && options.Combination[module] != relation.ObjectID {
					continue
				}
				conditions = append(conditions, types.ShelfCondition{ModuleType: module, PartIDs: []string{relation.ObjectID}})
			}
			evidence := types.ShelfEvidence{EffectiveAt: relation.ValidFrom, RecordedAt: relation.CreatedAt}
			for _, log := range relationLogs {
				if log.ObjectID == relation.ID && (log.OperationType == "create_relation" || log.OperationType == "update_relation") {
					evidence.OperationLogID, evidence.Source, evidence.ChangesetID = log.ID, log.Source, log.ChangesetID
					break
				}
			}
			result.Facts = append(result.Facts, types.ShelfFact{EntityID: id, RelationID: relation.ID, Field: relation.RelationType, Value: relation, Conditions: conditions,
				PartialOptions: options.Combination == nil && shelfConditionsPartial(conditions, composition), Origin: "self", OriginLabel: "自身", Evidence: evidence})
		}
	}
	return result, nil
}

func shelfConditionsPartial(conditions []types.ShelfCondition, composition []types.ShelfComposition) bool {
	for _, condition := range conditions {
		for _, group := range composition {
			if group.ModuleType == condition.ModuleType && slices.ContainsFunc(group.Parts, func(part types.Entity) bool { return !slices.Contains(condition.PartIDs, part.ID) }) {
				return true
			}
		}
	}
	return false
}

// Component ownership and the cell key impose conditions even if the submitter
// did not repeat them in the explicit condition list.
func shelfCertificationConditions(record types.ShelfCertification, composition []types.ShelfComposition) []types.ShelfCondition {
	conditions := slices.Clone(record.Conditions)
	for _, group := range composition {
		for _, part := range group.Parts {
			if part.ID == record.OwnerID || part.ID == record.CellPartID {
				conditions = append(conditions, types.ShelfCondition{ModuleType: group.ModuleType, PartIDs: []string{part.ID}})
			}
		}
	}
	return conditions
}

// Compare regional records as sets, including their condition sets. Array order
// and nil versus empty conditions do not change the source of a regional fact.
func shelfEvidenceValue(attributes map[string]any, field, region string) any {
	if region == "" {
		return attributes[field]
	}
	var statuses []types.ShelfSaleStatus
	if types.DecodeShelfValue(attributes[field], &statuses) != nil {
		return attributes[field]
	}
	rows := []types.ShelfSaleStatus{}
	for _, row := range statuses {
		if row.Region != region {
			continue
		}
		if row.Conditions == nil {
			row.Conditions = []types.ShelfCondition{}
		}
		for _, condition := range row.Conditions {
			slices.Sort(condition.PartIDs)
		}
		slices.SortFunc(row.Conditions, func(a, b types.ShelfCondition) int { return strings.Compare(a.ModuleType, b.ModuleType) })
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b types.ShelfSaleStatus) int {
		left, _ := json.Marshal(a)
		right, _ := json.Marshal(b)
		return bytes.Compare(left, right)
	})
	return rows
}

// Walk back unchanged fields or regions so an unrelated later edit cannot
// claim their provenance. Backfilled snapshots carry their own operation log.
func shelfAttributeEvidence(id, field, region string, versions []types.EntityAttributeVersion, logs []types.OperationLog) types.ShelfEvidence {
	equal := func(a, b map[string]any) bool {
		return equalManagedAttributes(map[string]any{field: shelfEvidenceValue(a, field, region)}, map[string]any{field: shelfEvidenceValue(b, field, region)})
	}
	var chosen *types.EntityAttributeVersion
	for i := range versions {
		version := &versions[i]
		if version.EntityID != id {
			continue
		}
		if chosen != nil && !equal(chosen.Attributes, version.Attributes) {
			break
		}
		chosen = version
	}
	if chosen == nil {
		return types.ShelfEvidence{}
	}
	evidence := types.ShelfEvidence{SnapshotID: chosen.ID, EffectiveAt: chosen.EffectiveAt, RecordedAt: chosen.RecordedAt}
	for _, log := range logs {
		if log.ObjectID != id {
			continue
		}
		var after, before struct {
			ID         string         `json:"id"`
			Revision   int64          `json:"revision"`
			Attributes map[string]any `json:"attributes"`
		}
		if json.Unmarshal(log.After, &after) != nil {
			continue
		}
		if log.OperationType == "update_pending_entity_attributes" && after.ID == chosen.ID {
			if json.Unmarshal(log.Before, &before) != nil || equal(before.Attributes, after.Attributes) {
				continue
			}
			evidence.RecordedAt = log.CreatedAt
		} else if after.ID != id || after.Revision != chosen.Revision {
			continue
		}
		evidence.OperationLogID, evidence.Source, evidence.ChangesetID = log.ID, log.Source, log.ChangesetID
		break
	}
	return evidence
}

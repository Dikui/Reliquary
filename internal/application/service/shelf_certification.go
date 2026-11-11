package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func shelfCertificationOwner(tx *gorm.DB, library *types.Library, id string) (*types.Entity, error) {
	var owner types.Entity
	err := tx.Where("id=? AND library_id=? AND space_id=? AND archived_at IS NULL AND merged_into_id=''", id, library.ID, library.SpaceID).First(&owner).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.NewBadRequestError("uncertain: certification owner cannot be identified")
	}
	if err != nil {
		return nil, err
	}
	if !slices.Contains([]string{"model", "part", "product", "platform"}, owner.Kind) {
		return nil, apperrors.NewBadRequestError("uncertain: certification owner must be a model, part, product or platform")
	}
	return &owner, nil
}

// Only explicit composition and hierarchy edges establish the scope of a fact.
func shelfCertificationModels(tx *gorm.DB, library *types.Library, owner *types.Entity, at time.Time) ([]string, error) {
	if owner.Kind == "model" {
		return []string{owner.ID}, nil
	}
	ids := []string{}
	var err error
	if owner.Kind == "part" {
		err = tx.Model(&types.Relation{}).Where("library_id=? AND space_id=? AND object_id=? AND relation_type='composed_of' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", library.ID, library.SpaceID, owner.ID, at, at).Distinct("subject_id").Pluck("subject_id", &ids).Error
	} else {
		err = tx.Raw(`WITH RECURSIVE descendants(id) AS (
 SELECT ?::varchar UNION SELECT r.subject_id FROM relations r JOIN descendants d ON r.object_id=d.id
 WHERE r.library_id=? AND r.space_id=? AND r.relation_type='belongs_to' AND r.valid_from<=? AND (r.valid_to IS NULL OR r.valid_to>?))
 SELECT e.id FROM descendants d JOIN entities e ON e.id=d.id WHERE e.kind='model' AND e.library_id=?`, owner.ID, library.ID, library.SpaceID, at, at, library.ID).Scan(&ids).Error
	}
	return ids, err
}

func validateShelfCertificationRecord(tx *gorm.DB, library *types.Library, record types.ShelfCertification, at time.Time) error {
	if err := types.ValidateShelfCertification(record); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	standard, ok := library.ShelfConfig.Standard(record.Standard)
	if !ok || standard.Key != record.Standard {
		return apperrors.NewBadRequestError("uncertain: certification standard must resolve to a configured canonical standard")
	}
	if record.Region != "" && !shelfOptionExists(library.ShelfConfig.Regions, record.Region) {
		return apperrors.NewBadRequestError("uncertain: certification region must be a configured region key or empty").WithDetails(map[string]any{"region": record.Region})
	}
	owner, err := shelfCertificationOwner(tx, library, record.OwnerID)
	if err != nil {
		return err
	}
	models, err := shelfCertificationModels(tx, library, owner, at)
	if err != nil {
		return err
	}
	checkPart := func(id, module string) error {
		var part types.Entity
		if err := tx.Where("id=? AND library_id=? AND space_id=? AND kind='part' AND archived_at IS NULL AND merged_into_id=''", id, library.ID, library.SpaceID).First(&part).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.NewBadRequestError("uncertain: certification part cannot be identified")
			}
			return err
		}
		if owner.Kind == "part" && owner.ID == id && module == "" {
			return nil
		}
		var count int64
		query := tx.Model(&types.Relation{}).Where("library_id=? AND subject_id IN ? AND object_id=? AND relation_type='composed_of' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", library.ID, models, id, at, at)
		if module != "" {
			query = query.Where("attributes->>'module_type'=?", module)
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return apperrors.NewBadRequestError("uncertain: certification part does not match its owner scope")
		}
		return nil
	}
	if record.CellPartID != "" {
		if err := checkPart(record.CellPartID, ""); err != nil {
			return err
		}
	}

	cellModule := ""
	if record.CellPartID != "" {
		if err := tx.Model(&types.Relation{}).Select("attributes->>'module_type'").Where("library_id=? AND object_id=? AND relation_type='composed_of' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", library.ID, record.CellPartID, at, at).Limit(1).Scan(&cellModule).Error; err != nil {
			return err
		}
	}
	for _, condition := range record.Conditions {
		if condition.ModuleType == cellModule && !slices.Contains(condition.PartIDs, record.CellPartID) {
			return apperrors.NewBadRequestError("uncertain: certification cell and conditions select different parts of the same module")
		}
		if !shelfOptionExists(library.ShelfConfig.ModuleTypes, condition.ModuleType) {
			return apperrors.NewBadRequestError("condition module type is not configured")
		}
		for _, id := range condition.PartIDs {
			if err := checkPart(id, condition.ModuleType); err != nil {
				return err
			}
		}
	}
	if owner.Kind == "model" {
		return validateShelfConditions(tx, library, owner.ID, record.Conditions, at)
	}
	if len(record.Conditions) > 0 {
		var relations []types.Relation
		if err := tx.Where("library_id=? AND subject_id IN ? AND relation_type='composed_of' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", library.ID, models, at, at).Find(&relations).Error; err != nil {
			return err
		}
		for _, modelID := range models {
			modules := map[string][]types.Entity{}
			for _, relation := range relations {
				if relation.SubjectID == modelID {
					module, _ := relation.Attributes["module_type"].(string)
					modules[module] = append(modules[module], types.Entity{ID: relation.ObjectID})
				}
			}
			composition := []types.ShelfComposition{}
			for module, parts := range modules {
				composition = append(composition, types.ShelfComposition{ModuleType: module, Parts: parts})
			}
			if shelfCertificationFitsComposition(record, composition) {
				return nil
			}
		}
		return apperrors.NewBadRequestError("uncertain: certification conditions cannot be satisfied by one model in the owner scope")
	}
	return nil
}

func shelfCertificationKeyEqual(a, b types.ShelfCertification) bool {
	return a.OwnerID == b.OwnerID && a.Standard == b.Standard && a.CertificationObject == b.CertificationObject && a.CellPartID == b.CellPartID && a.Region == b.Region
}

func (s *EntityService) shelfCertificationEntities(ctx context.Context, library *types.Library, at time.Time) ([]types.Entity, error) {
	rows := []types.Entity{}
	if err := s.db.WithContext(ctx).Where("library_id=? AND space_id=? AND kind='certification' AND archived_at IS NULL AND merged_into_id=''", library.ID, library.SpaceID).Order("created_at,id").Find(&rows).Error; err != nil {
		return nil, err
	}
	pointers := make([]*types.Entity, len(rows))
	for i := range rows {
		pointers[i] = &rows[i]
	}
	if err := hydrateManagedEntityAttributes(ctx, s.db, pointers, at); err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *EntityService) resolveShelfCertification(ctx context.Context, draft *types.ShelfChangeset, op types.ShelfOperation, at time.Time, decision *types.ShelfDecision) (*types.Entity, error) {
	library, err := managedLibrary(s.db, draft.SpaceID, draft.LibraryID)
	if err != nil {
		return nil, err
	}
	record := op.Certification.ShelfCertification
	standard, ok := library.ShelfConfig.Standard(record.Standard)
	if !ok {
		return nil, apperrors.NewBadRequestError("uncertain: certification standard is unknown")
	}
	record.Standard = standard.Key
	if err := validateShelfCertificationRecord(s.db, library, record, at); err != nil {
		return nil, err
	}
	rows, err := s.shelfCertificationEntities(ctx, library, at)
	if err != nil {
		return nil, err
	}
	matches := []types.Entity{}
	for _, entity := range rows {
		var existing types.ShelfCertification
		if raw, ok := entity.Attributes["certification"]; ok {
			if err := types.DecodeShelfValue(raw, &existing); err != nil {
				return nil, err
			}
			if shelfCertificationKeyEqual(existing, record) {
				matches = append(matches, entity)
			}
		}
	}
	// An explicit legacy record may replace its unconfigured region. Retain all
	// other identity fields and the usual revision check; never infer this match.
	if len(matches) == 0 && op.EntityID != "" {
		for _, entity := range rows {
			if entity.ID != op.EntityID {
				continue
			}
			var existing types.ShelfCertification
			if err := types.DecodeShelfValue(entity.Attributes["certification"], &existing); err != nil {
				return nil, err
			}
			if existing.Region == "" || shelfOptionExists(library.ShelfConfig.Regions, existing.Region) {
				continue
			}
			existing.Region = record.Region
			if shelfCertificationKeyEqual(existing, record) {
				matches = append(matches, entity)
			}
		}
	}

	selected := -1
	if len(matches) == 1 {
		selected = 0
	}
	if len(matches) > 0 && decision != nil && decision.TargetEntityID != "" {
		if decision.Action != "apply" || decision.TargetRevision < 1 {
			return nil, apperrors.NewBadRequestError("select a certification with its target revision")
		}
		selected = slices.IndexFunc(matches, func(entity types.Entity) bool { return entity.ID == decision.TargetEntityID })
	}
	if selected < 0 {
		return nil, apperrors.NewBadRequestError("uncertain: certification key must match exactly one record or an explicitly selected candidate").WithDetails(map[string]any{"match_count": len(matches), "candidates": matches})
	}
	if op.EntityID != "" && matches[selected].ID != op.EntityID {
		return nil, apperrors.NewBadRequestError("uncertain: certification key does not match the selected record")
	}
	return &matches[selected], nil
}

func shelfCertificationTime(draft *types.ShelfChangeset, op types.ShelfOperation) time.Time {
	if draft.Source.Type == "import" {
		return *draft.Source.FileDate
	}
	if op.EffectiveAt != nil {
		return *op.EffectiveAt
	}
	return time.Now()
}

func (s *EntityService) writeShelfCertification(ctx context.Context, draft *types.ShelfChangeset, item *types.ShelfChangesetItem, op types.ShelfOperation) (*types.Entity, error) {
	library, err := managedLibrary(s.db, draft.SpaceID, draft.LibraryID)
	if err != nil {
		return nil, err
	}
	record := op.Certification.ShelfCertification
	standard, ok := library.ShelfConfig.Standard(record.Standard)
	if !ok {
		return nil, apperrors.NewBadRequestError("uncertain: certification standard is unknown")
	}
	record.Standard = standard.Key
	attrs := map[string]any{"certification": record}
	confirmations := []types.ManagedNameConfirmation{}
	if item.Decision != nil {
		confirmations = item.Decision.NameConfirmations
	}
	at := shelfCertificationTime(draft, op)
	if op.Type == "create_certification" {
		if op.Certification.Name == "" {
			return nil, apperrors.NewBadRequestError("certification name is required")
		}
		entity, err := s.createManagedEntity(ctx, draft.LibraryID, types.ManagedEntityInput{Name: op.Certification.Name, Kind: "certification", Attributes: attrs, EffectiveAt: &at, NameConfirmations: confirmations}, item.EntityID)
		if err != nil {
			return nil, err
		}
		relationAt := at
		if relationAt.After(time.Now()) {
			relationAt = time.Now()
		}
		if _, err := s.SaveManagedRelation(ctx, draft.LibraryID, "", types.ManagedRelationInput{SubjectID: record.OwnerID, ObjectID: entity.ID, RelationType: "has_certification", ValidFrom: &relationAt}); err != nil {
			return nil, err
		}
		if err := hydrateManagedEntityAttributes(ctx, s.db, []*types.Entity{entity}, at); err != nil {
			return nil, err
		}
		return entity, nil
	}
	entity, err := s.resolveShelfCertification(ctx, draft, op, at, item.Decision)
	if err != nil {
		return nil, err
	}
	item.EntityID = entity.ID
	// Replace the complete record explicitly, leaving other configured fields intact.
	for key, value := range entity.Attributes {
		if key != "certification" {
			attrs[key] = value
		}
	}
	patch := types.ManagedEntityPatch{Revision: op.Revision, Attributes: &attrs, EffectiveAt: &at, NameConfirmations: confirmations}
	if item.Decision != nil && item.Decision.TargetEntityID != "" {
		patch.Revision = item.Decision.TargetRevision
	}
	if op.Certification.Name != "" {
		patch.Name = &op.Certification.Name
	}
	result, err := s.UpdateManagedEntity(ctx, entity.ID, patch)
	if err != nil {
		return nil, err
	}
	if err := hydrateManagedEntityAttributes(ctx, s.db, []*types.Entity{result}, at); err != nil {
		return nil, err
	}
	return result, nil
}

// ShelfCertifications uses the same standard resolution as writes and node reads.
func (s *EntityService) ShelfCertifications(ctx context.Context, libraryID, standard string, at time.Time) ([]types.ShelfCertificationView, error) {
	library, err := s.shelfLibrary(ctx, s.db, libraryID)
	if err != nil {
		return nil, err
	}
	return s.shelfCertificationViews(ctx, library, standard, at)
}

func (s *EntityService) shelfCertificationViews(ctx context.Context, library *types.Library, standardFilter string, at time.Time) ([]types.ShelfCertificationView, error) {
	result := []types.ShelfCertificationView{}
	filterKey := ""
	if standardFilter != "" {
		standard, ok := library.ShelfConfig.Standard(standardFilter)
		if !ok {
			return result, nil
		}
		filterKey = standard.Key
	}
	rows, err := s.shelfCertificationEntities(ctx, library, at)
	if err != nil {
		return nil, err
	}
	for _, entity := range rows {
		raw, ok := entity.Attributes["certification"]
		if !ok {
			continue
		}
		var record types.ShelfCertification
		if err := types.DecodeShelfValue(raw, &record); err != nil {
			return nil, err
		}
		if filterKey != "" && record.Standard != filterKey {
			continue
		}
		standard, ok := library.ShelfConfig.Standard(record.Standard)
		if !ok {
			return nil, apperrors.NewBadRequestError("certification standard is no longer configured")
		}
		entity.KindLabel = managedKindLabel(library, entity.Kind)
		result = append(result, types.ShelfCertificationView{Entity: entity, Certification: record, StandardName: standard.Name, RegionName: library.ShelfConfig.RegionName(record.Region), Origin: "self"})
	}
	return result, nil
}

func (s *EntityService) shelfNodeCertifications(ctx context.Context, library *types.Library, entity *types.Entity, at time.Time, composition []types.ShelfComposition, combination map[string]string) ([]types.ShelfCertificationView, error) {
	owners := map[string]string{entity.ID: "self"}
	if entity.Kind == "model" {
		partCount := 0
		for _, group := range composition {
			partCount += len(group.Parts)
		}
		for _, group := range composition {
			for _, part := range group.Parts {
				if combination != nil && combination[group.ModuleType] != part.ID {
					continue
				}
				origin := "component"
				if partCount == 1 {
					origin = "self"
				}
				owners[part.ID] = origin
			}
		}
	}
	if entity.Kind == "model" || entity.Kind == "product" {
		var ancestors []string
		err := s.db.WithContext(ctx).Raw(`WITH RECURSIVE ancestors(id,depth) AS (
 SELECT ?::varchar,0 UNION ALL SELECT r.object_id,a.depth+1 FROM relations r JOIN ancestors a ON r.subject_id=a.id
 WHERE r.library_id=? AND r.space_id=? AND r.relation_type='belongs_to' AND r.valid_from<=? AND (r.valid_to IS NULL OR r.valid_to>?) AND a.depth<3)
 SELECT e.id FROM ancestors a JOIN entities e ON e.id=a.id WHERE a.depth>0 AND e.kind IN ('platform','product')`, entity.ID, library.ID, library.SpaceID, at, at).Scan(&ancestors).Error
		if err != nil {
			return nil, err
		}
		for _, id := range ancestors {
			owners[id] = "ancestor"
		}
	}
	rows, err := s.shelfCertificationViews(ctx, library, "", at)
	if err != nil {
		return nil, err
	}
	result := []types.ShelfCertificationView{}
	for _, row := range rows {
		origin, ok := owners[row.Certification.OwnerID]

		if entity.Kind == "model" && !shelfCertificationFitsComposition(row.Certification, composition) {
			continue
		}
		if !ok || !shelfConditionsMatch(row.Certification.Conditions, combination) {
			continue
		}
		if row.Certification.CellPartID != "" && combination != nil && !slices.ContainsFunc(composition, func(group types.ShelfComposition) bool {
			return combination[group.ModuleType] == row.Certification.CellPartID
		}) {
			continue
		}
		// A relation is required at this business time; a future or ended owner link
		// cannot make an otherwise valid snapshot appear on the shelf.
		var count int64
		if err := s.db.WithContext(ctx).Model(&types.Relation{}).Where("library_id=? AND space_id=? AND subject_id=? AND object_id=? AND relation_type='has_certification' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", library.ID, library.SpaceID, row.Certification.OwnerID, row.Entity.ID, at, at).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			continue
		}
		row.Origin = origin
		result = append(result, row)
	}
	return result, nil
}

func validateShelfCertificationStructure(tx *gorm.DB, library *types.Library, entity *types.Entity, at time.Time) error {
	var record types.ShelfCertification
	if err := types.DecodeShelfValue(entity.Attributes["certification"], &record); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	// Region has no structural scope. Legacy text must not block unrelated
	// hierarchy or composition edits; certification writes validate it separately.
	record.Region = ""
	if err := validateShelfCertificationRecord(tx, library, record, at); err != nil {
		return err
	}
	var count int64
	if err := tx.Model(&types.Relation{}).Where("library_id=? AND space_id=? AND subject_id=? AND object_id=? AND relation_type='has_certification' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", library.ID, library.SpaceID, record.OwnerID, entity.ID, at, at).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return apperrors.NewBadRequestError("uncertain: certification must retain its recorded owner link")
	}
	return nil
}

func shelfCertificationFitsComposition(record types.ShelfCertification, composition []types.ShelfComposition) bool {
	containsPart := func(module, id string) bool {
		for _, group := range composition {
			if module != "" && group.ModuleType != module {
				continue
			}
			if slices.ContainsFunc(group.Parts, func(part types.Entity) bool { return part.ID == id }) {
				return true
			}
		}
		return false
	}
	if record.CellPartID != "" && !containsPart("", record.CellPartID) {
		return false
	}
	for _, condition := range record.Conditions {
		if !slices.ContainsFunc(condition.PartIDs, func(id string) bool { return containsPart(condition.ModuleType, id) }) {
			return false
		}
	}
	return true
}

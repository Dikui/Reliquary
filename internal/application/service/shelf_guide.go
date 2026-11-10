package service

import (
	"cmp"
	"context"
	"slices"
	"time"

	"reliquary/internal/types"
)

type ShelfGuide struct {
	LibraryID        string                 `json:"library_id"`
	LibraryUpdatedAt time.Time              `json:"library_updated_at"`
	AsOf             time.Time              `json:"as_of"`
	MaxFileBytes     int64                  `json:"max_file_bytes"`
	Values           types.ShelfGuideValues `json:"values"`
	ImportPlans      []ShelfImportPlanView  `json:"import_plans"`
	Gaps             *ShelfGuideGaps        `json:"gaps,omitempty"`
}

// ShelfGuide reads configuration and identities in one read-only snapshot.
func (s *EntityService) ShelfGuide(ctx context.Context, libraryID, nodeID string) (*ShelfGuide, error) {
	ctx = context.WithValue(ctx, shelfChecklistCacheKey{}, &shelfChecklistCache{})
	var result *ShelfGuide
	err := s.readShelfSnapshot(ctx, func(reader *EntityService) error {
		library, err := reader.shelfLibraryForRead(ctx, libraryID)
		if err != nil {
			return err
		}
		at := time.Now()
		result = &ShelfGuide{LibraryID: library.ID, LibraryUpdatedAt: library.UpdatedAt, AsOf: at, MaxFileBytes: library.ShelfConfig.FileSizeLimit()}
		result.Values = types.ShelfGuideValues{
			Kinds: library.EntitySchema.Kinds, Fields: library.EntitySchema.Fields,
			ProductLines: []types.Entity{}, RequirementLists: []types.ShelfGuideRequirementList{},
			Regions: library.ShelfConfig.Regions, Markets: library.ShelfConfig.Markets, Stages: library.ShelfConfig.Stages,
			ModuleTypes: library.ShelfConfig.ModuleTypes, Standards: library.ShelfConfig.Standards,
			SaleStatuses: []string{"GA", "DEV", "Planning", "EOM", "待定"},
		}
		var entities []types.Entity
		if err := reader.db.WithContext(ctx).Where("library_id=? AND space_id=? AND kind IN ('product_line','document_requirement') AND archived_at IS NULL AND merged_into_id=''", library.ID, library.SpaceID).Order("canonical_name,id").Find(&entities).Error; err != nil {
			return err
		}
		pointers := []*types.Entity{}
		for i := range entities {
			pointers = append(pointers, &entities[i])
		}
		if err := hydrateManagedEntityAttributes(ctx, reader.db, pointers, at); err != nil {
			return err
		}
		if err := reader.loadAliases(ctx, library.SpaceID, entities); err != nil {
			return err
		}
		var bindings []types.Relation
		if err := reader.db.WithContext(ctx).Where("library_id=? AND space_id=? AND relation_type='applies_to' AND valid_from<=? AND (valid_to IS NULL OR valid_to>?)", library.ID, library.SpaceID, at, at).Find(&bindings).Error; err != nil {
			return err
		}
		lines := map[string]types.Entity{}
		for _, entity := range entities {
			if entity.Kind == "product_line" {
				lines[entity.ID] = entity
			}
		}
		lists := map[string]*types.ShelfGuideRequirementList{}
		for _, entity := range entities {
			entity.KindLabel = managedKindLabel(library, entity.Kind)
			if entity.Kind == "product_line" {
				result.Values.ProductLines = append(result.Values.ProductLines, entity)
			} else {
				var requirement types.ShelfRequirement
				if err := types.DecodeShelfValue(entity.Attributes["requirement"], &requirement); err != nil {
					return err
				}
				summary := lists[requirement.ListName]
				if summary == nil {
					summary = &types.ShelfGuideRequirementList{Name: requirement.ListName, ProductLines: []types.ShelfGuideProductLine{}}
					lists[requirement.ListName] = summary
				}
				summary.Total++
				for _, binding := range bindings {
					line, ok := lines[binding.ObjectID]
					if binding.SubjectID == entity.ID && ok && !slices.ContainsFunc(summary.ProductLines, func(item types.ShelfGuideProductLine) bool { return item.ID == line.ID }) {
						summary.ProductLines = append(summary.ProductLines, types.ShelfGuideProductLine{ID: line.ID, Name: line.CanonicalName})
					}
				}
			}
		}
		for _, summary := range lists {
			slices.SortFunc(summary.ProductLines, func(a, b types.ShelfGuideProductLine) int {
				if n := cmp.Compare(a.Name, b.Name); n != 0 {
					return n
				}
				return cmp.Compare(a.ID, b.ID)
			})
			result.Values.RequirementLists = append(result.Values.RequirementLists, *summary)
		}
		slices.SortFunc(result.Values.RequirementLists, func(a, b types.ShelfGuideRequirementList) int { return cmp.Compare(a.Name, b.Name) })
		result.ImportPlans, err = reader.ShelfImportPlans(ctx, libraryID)
		if err != nil {
			return err
		}
		if nodeID != "" {
			requirementIDs := map[string]bool{}
			result.Gaps, err = reader.shelfGuideGaps(ctx, library, nodeID, at, requirementIDs)
			if err != nil {
				return err
			}
			result.Values.Requirements = &types.ShelfGuidePage[types.ShelfGuideRequirement]{Items: []types.ShelfGuideRequirement{}}
			for _, entity := range entities {
				if !requirementIDs[entity.ID] {
					continue
				}
				requirement, err := shelfGuideRequirement(entity)
				if err != nil {
					return err
				}
				result.Values.Requirements.Add(requirement)
			}
		}
		return err
	})
	return result, err
}

func shelfGuideRequirement(entity types.Entity) (types.ShelfGuideRequirement, error) {
	var requirement types.ShelfRequirement
	if err := types.DecodeShelfValue(entity.Attributes["requirement"], &requirement); err != nil {
		return types.ShelfGuideRequirement{}, err
	}
	return types.ShelfGuideRequirement{ID: entity.ID, Name: entity.CanonicalName, ListName: requirement.ListName, NameZH: requirement.NameZH, NameEN: requirement.NameEN, DraftDueStage: requirement.DraftDueStage, FinalDueStage: requirement.FinalDueStage}, nil
}

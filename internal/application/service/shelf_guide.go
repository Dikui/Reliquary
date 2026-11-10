package service

import (
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
			ProductLines: []types.Entity{}, Requirements: []types.Entity{}, RequirementLists: []string{},
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
		for _, entity := range entities {
			entity.KindLabel = managedKindLabel(library, entity.Kind)
			if entity.Kind == "product_line" {
				result.Values.ProductLines = append(result.Values.ProductLines, entity)
			} else {
				result.Values.Requirements = append(result.Values.Requirements, entity)
				var requirement types.ShelfRequirement
				if err := types.DecodeShelfValue(entity.Attributes["requirement"], &requirement); err != nil {
					return err
				}
				if !slices.Contains(result.Values.RequirementLists, requirement.ListName) {
					result.Values.RequirementLists = append(result.Values.RequirementLists, requirement.ListName)
				}
			}
		}
		slices.Sort(result.Values.RequirementLists)
		result.ImportPlans, err = reader.ShelfImportPlans(ctx, libraryID)
		if err != nil {
			return err
		}
		if nodeID != "" {
			result.Gaps, err = reader.shelfGuideGaps(ctx, library, nodeID, at)
		}
		return err
	})
	return result, err
}

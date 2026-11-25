package service

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"reliquary/internal/types"
)

var (
	ErrShowcaseLibraryNotFound  = errors.New("showcase_library_not_found")
	ErrShowcaseLibraryAmbiguous = errors.New("showcase_library_ambiguous")
)

// ShowcaseShelf reads every model and its hierarchy in one read-only snapshot.
func (s *EntityService) ShowcaseShelf(ctx context.Context, selector string) (*types.ShowcaseShelf, error) {
	ctx = context.WithValue(ctx, shelfChecklistCacheKey{}, &shelfChecklistCache{})
	var result *types.ShowcaseShelf
	err := s.readShelfSnapshot(ctx, func(reader *EntityService) error {
		library, err := reader.showcaseLibrary(ctx, selector)
		if err != nil {
			return err
		}
		at := time.Now().UTC()
		rows, err := reader.shelfQueryAll(ctx, library.ID, types.ShelfQueryRequest{AsOf: &at})
		if err != nil {
			return err
		}
		data, err := reader.loadShelfChecklist(ctx, library, at)
		if err != nil {
			return err
		}
		result = &types.ShowcaseShelf{
			Library:      types.ShowcaseNamedEntity{ID: library.ID, Name: library.Name},
			AsOf:         at,
			Regions:      make([]types.ShowcaseRegion, 0, len(library.ShelfConfig.Regions)),
			ProductLines: []types.ShowcaseNamedEntity{},
			Models:       make([]types.ShowcaseModel, 0, len(rows)),
		}
		for _, region := range library.ShelfConfig.Regions {
			result.Regions = append(result.Regions, types.ShowcaseRegion{Key: region.Key, Name: region.Name})
		}
		for _, node := range data.nodes {
			if node.Kind == "product_line" && node.ArchivedAt == nil && node.MergedIntoID == "" {
				result.ProductLines = append(result.ProductLines, types.ShowcaseNamedEntity{ID: node.ID, Name: node.CanonicalName})
			}
		}
		slices.SortFunc(result.ProductLines, func(a, b types.ShowcaseNamedEntity) int {
			return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
		})
		for _, row := range rows {
			result.Models = append(result.Models, showcaseModel(row, data.ancestors(row.Entity.ID), library.ShelfConfig))
		}
		return nil
	})
	return result, err
}

func (s *EntityService) showcaseLibrary(ctx context.Context, selector string) (*types.Library, error) {
	spaceID, err := s.requireSpace(ctx)
	if err != nil {
		return nil, err
	}
	query := s.db.WithContext(ctx).Where("space_id=? AND shelf_enabled", spaceID)
	if selector != "" {
		query = query.Where("(id=? OR directory=?)", selector, selector)
	}
	var libraries []types.Library
	if err := query.Limit(2).Find(&libraries).Error; err != nil {
		return nil, err
	}
	switch len(libraries) {
	case 0:
		return nil, ErrShowcaseLibraryNotFound
	case 1:
		return &libraries[0], nil
	}
	if selector != "" {
		// Each identifier is unique. If both match, the library ID takes precedence.
		for i := range libraries {
			if libraries[i].ID == selector {
				return &libraries[i], nil
			}
		}
	}
	return nil, ErrShowcaseLibraryAmbiguous
}

func showcaseModel(row types.ShelfQueryItem, ancestors []types.Entity, config types.ShelfConfig) types.ShowcaseModel {
	model := types.ShowcaseModel{
		ID:             row.Entity.ID,
		Name:           row.Entity.CanonicalName,
		CurrentStage:   row.Shelf.CurrentStage,
		SaleStatuses:   make([]types.ShowcaseSaleStatus, 0, len(row.Shelf.SaleStatuses)),
		Checklist:      make([]types.ShowcaseChecklistItem, 0, len(row.Shelf.Checklist)),
		Certifications: make([]types.ShowcaseCertification, 0, len(row.Shelf.Certifications)),
	}
	for _, ancestor := range ancestors {
		switch ancestor.Kind {
		case "product":
			model.ProductName = ancestor.CanonicalName
		case "platform":
			model.Platform = ancestor.CanonicalName
		case "product_line":
			model.ProductLine = types.ShowcaseNamedEntity{ID: ancestor.ID, Name: ancestor.CanonicalName}
		}
	}
	for _, status := range row.Shelf.SaleStatuses {
		model.SaleStatuses = append(model.SaleStatuses, types.ShowcaseSaleStatus{
			Region: status.Region, RegionName: config.RegionName(status.Region), Status: status.Status,
			Raw: status.Raw, Notes: status.Notes, PlannedEOM: status.PlannedEOM, Conditional: len(status.Conditions) > 0,
		})
	}
	for _, item := range row.Shelf.Checklist {
		model.Checklist = append(model.Checklist, types.ShowcaseChecklistItem{
			Name: item.Requirement.CanonicalName, Status: item.Status, StatusLabel: item.StatusLabel,
			Gap: item.Gap, HasFile: item.HasFile,
		})
	}
	for _, cert := range row.Shelf.Certifications {
		model.Certifications = append(model.Certifications, types.ShowcaseCertification{
			StandardName: cert.StandardName, Status: cert.Status, RegionName: cert.RegionName, Origin: cert.Origin,
		})
	}
	return model
}

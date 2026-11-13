package service

import (
	"context"
	"sort"
	"strings"
	"time"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

// ExportShelf reads every worksheet at one business time in one database snapshot.
func (s *EntityService) ExportShelf(ctx context.Context, libraryID string, input types.ShelfExportRequest) ([]byte, error) {
	ctx = context.WithValue(ctx, shelfChecklistCacheKey{}, &shelfChecklistCache{})
	var sheets []shelfExportSheet
	err := s.readShelfSnapshot(ctx, func(reader *EntityService) error {
		library, err := reader.shelfLibraryForRead(ctx, libraryID)
		if err != nil {
			return err
		}
		var lines []types.Entity
		query := reader.db.WithContext(ctx).Where("library_id=? AND space_id=? AND kind='product_line' AND archived_at IS NULL AND merged_into_id=''", library.ID, library.SpaceID)
		if input.ProductLineID != "" {
			query = query.Where("id=?", input.ProductLineID)
		}
		if err := query.Order("canonical_name,id").Find(&lines).Error; err != nil {
			return err
		}
		if len(lines) == 0 {
			return apperrors.NewBadRequestError("export requires an active product line in this shelf")
		}
		at := time.Now()
		if input.Query.AsOf != nil {
			at = *input.Query.AsOf
		}
		for _, line := range lines {
			all, err := reader.shelfExportModels(ctx, libraryID, types.ShelfQueryRequest{AncestorID: line.ID, AsOf: &at})
			if err != nil {
				return err
			}
			filter := input.Query
			filter.AncestorID, filter.AsOf = line.ID, &at
			rows := all
			if len(filter.SaleStatuses)+len(filter.Parameters)+len(filter.Composition) > 0 || filter.Certification != nil || filter.DocumentGaps || filter.PartID != "" {
				rows, err = reader.shelfExportModels(ctx, libraryID, filter)
				if err != nil {
					return err
				}
			}
			sheet, err := reader.shelfExportLine(ctx, library, line, all, rows, at)
			if err != nil {
				return err
			}
			sheets = append(sheets, sheet)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return writeShelfWorkbook(sheets)
}

func (s *EntityService) shelfExportModels(ctx context.Context, libraryID string, query types.ShelfQueryRequest) ([]types.ShelfQueryItem, error) {
	query.Limit, query.Offset, query.Detail = 100, 0, types.ShelfQueryDetailFull
	rows := []types.ShelfQueryItem{}
	for {
		page, err := s.shelfQuery(ctx, libraryID, query)
		if err != nil {
			return nil, err
		}
		rows = append(rows, page.Items...)
		if int64(len(rows)) >= page.Total {
			return rows, nil
		}
		query.Offset = len(rows)
	}
}

func (s *EntityService) shelfExportLine(ctx context.Context, library *types.Library, line types.Entity, all, rows []types.ShelfQueryItem, at time.Time) (shelfExportSheet, error) {
	sheet := shelfExportSheet{Name: line.CanonicalName, Rows: [][]shelfExportCell{{{Text: line.CanonicalName}}}}
	used := map[string]bool{}
	for _, row := range all {
		for _, status := range row.Shelf.SaleStatuses {
			used[status.Region] = true
		}
	}
	regions := []types.ShelfOption{}
	head := []shelfExportCell{{Text: "产品平台"}, {Text: "产品名称"}, {Text: "产品型号"}, {Text: "描述"}}
	for _, region := range library.ShelfConfig.Regions {
		if used[region.Key] {
			regions = append(regions, region)
			head = append(head, shelfExportCell{Text: region.Name})
		}
	}
	sheet.Rows = append(sheet.Rows, head)
	// Cache upper nodes shared by many models while retaining the query's time.
	nodes := map[string]*types.ShelfNodeDetail{}
	parent := func(node *types.ShelfNodeDetail) (*types.ShelfNodeDetail, error) {
		for _, fact := range node.Facts {
			if fact.Field != "belongs_to" {
				continue
			}
			relation := fact.Value.(types.Relation)
			if cached, ok := nodes[relation.ObjectID]; ok {
				return cached, nil
			}
			upper, err := s.shelfGet(ctx, library.ID, relation.ObjectID, types.ShelfReadOptions{AsOf: &at})
			if err != nil {
				return nil, err
			}
			nodes[relation.ObjectID] = upper
			return upper, nil
		}
		return nil, nil
	}
	for _, row := range rows {
		product, err := parent(&row.ShelfNodeDetail)
		if err != nil {
			return sheet, err
		}
		platformName, productName := "", ""
		if product != nil {
			productName = product.Entity.CanonicalName
			platform, err := parent(product)
			if err != nil {
				return sheet, err
			}
			if platform != nil {
				platformName = platform.Entity.CanonicalName
			}
		}
		cells := []shelfExportCell{{Text: platformName}, {Text: productName}, {Text: row.Entity.CanonicalName}, {Text: row.Entity.Description}}
		for _, region := range regions {
			cell := shelfExportCell{}
			values := []string{}
			for _, status := range row.Shelf.SaleStatuses {
				if status.Region == region.Key {
					values = append(values, shelfExportStatus(library, row.Shelf, status))
					cell.PlannedEOM = cell.PlannedEOM || status.PlannedEOM
				}
			}
			cell.Text = strings.Join(values, "\n")
			cells = append(cells, cell)
		}
		sheet.Rows = append(sheet.Rows, cells)
	}
	// Keep models within their platform and product, with stable literal names.
	sort.SliceStable(sheet.Rows[2:], func(i, j int) bool {
		a, b := sheet.Rows[i+2], sheet.Rows[j+2]
		for col := 0; col < 3; col++ {
			if a[col].Text != b[col].Text {
				return a[col].Text < b[col].Text
			}
		}
		return false
	})
	return sheet, nil
}

func shelfExportStatus(library *types.Library, model *types.ShelfModelDetail, status types.ShelfSaleStatus) string {
	text := status.Status
	if status.PlannedEOM {
		text += " [计划退市]"
	}
	if status.Notes != "" {
		text += "（" + status.Notes + "）"
	}
	if status.Raw != "" {
		text += " [原文：" + status.Raw + "]"
	}
	for _, condition := range status.Conditions {
		module := condition.ModuleType
		for _, option := range library.ShelfConfig.ModuleTypes {
			if option.Key == condition.ModuleType {
				module = option.Name
			}
		}
		parts := []string{}
		for _, id := range condition.PartIDs {
			name := id
			for _, group := range model.Composition {
				for _, part := range group.Parts {
					if part.ID == id {
						name = part.CanonicalName
					}
				}
			}
			parts = append(parts, name)
		}
		text += " [" + module + " ∈ {" + strings.Join(parts, ", ") + "}]"
	}
	return text
}

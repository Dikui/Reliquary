package service

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

var shelfImportKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var shelfImportColumn = regexp.MustCompile(`^[A-Z]{1,3}$`)

func (s *EntityService) ShelfImportPlans(ctx context.Context, libraryID string) ([]types.ShelfImportPlan, error) {
	if _, err := s.shelfLibrary(ctx, s.db, libraryID); err != nil {
		return nil, err
	}
	rows := []types.ShelfImportPlan{}
	err := s.db.WithContext(ctx).Where("space_id=? AND library_id=?", types.MustSpaceIDFromContext(ctx), libraryID).Order("key").Find(&rows).Error
	return rows, err
}

func (s *EntityService) shelfImportPlan(ctx context.Context, libraryID, key string) (*types.ShelfImportPlan, error) {
	var plan types.ShelfImportPlan
	err := s.db.WithContext(ctx).Where("space_id=? AND library_id=? AND key=?", types.MustSpaceIDFromContext(ctx), libraryID, key).First(&plan).Error
	return &plan, err
}

func (s *EntityService) writeShelfImportPlan(ctx context.Context, draft *types.ShelfChangeset, input types.ShelfImportPlanInput) (*types.ShelfImportPlan, error) {
	if !shelfImportKey.MatchString(input.Key) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 200 || !slices.Contains([]string{"product_map", "certification_progress", "certification_report", "overseas_sales", "atq"}, input.FileKind) || len(input.Sheets) == 0 || len(input.Sheets) > 100 {
		return nil, apperrors.NewBadRequestError("import plan requires key, name, a supported file_kind and worksheets")
	}
	library, err := s.shelfLibrary(ctx, s.db, draft.LibraryID)
	if err != nil {
		return nil, err
	}
	worksheets := map[string]bool{}
	for raw, deadline := range input.Deadlines {
		if input.FileKind != "atq" || strings.TrimSpace(raw) == "" || !shelfOptionExists(library.ShelfConfig.Stages, deadline.FinalStage) || deadline.DraftStage != "" && !shelfOptionExists(library.ShelfConfig.Stages, deadline.DraftStage) {
			return nil, apperrors.NewBadRequestError("deadline mappings require ATQ source text and configured stage keys")
		}
	}
	for _, sheet := range input.Sheets {
		if sheet.Worksheet == "" || worksheets[sheet.Worksheet] || sheet.HeaderRows < 0 || sheet.HeaderRows > 100 {
			return nil, apperrors.NewBadRequestError("invalid worksheet or header_rows")
		}
		worksheets[sheet.Worksheet] = true
		if sheet.Ignore {
			continue
		}
		if input.FileKind != "product_map" {
			if err := s.validateRecordImportSheet(ctx, library, input.FileKind, sheet); err != nil {
				return nil, err
			}
			continue
		}
		line, err := s.shelfEntity(ctx, library.ID, sheet.ProductLineID)
		if err != nil {
			return nil, err
		}
		if line.Kind != "product_line" || line.ArchivedAt != nil {
			return nil, apperrors.NewBadRequestError("worksheet requires an active product line")
		}
		fields, columns := map[string]bool{}, map[string]bool{}
		for _, c := range sheet.Columns {
			if !shelfImportColumn.MatchString(c.Column) || importColumnNumber(c.Column) > 16384 || columns[c.Column] || fields[c.Field+":"+c.Key] {
				return nil, apperrors.NewBadRequestError("import columns and mapped fields must be distinct")
			}
			columns[c.Column], fields[c.Field+":"+c.Key] = true, true
			switch c.Field {
			case "platform", "product", "model", "description":
				if c.Key != "" {
					return nil, apperrors.NewBadRequestError("identity columns do not take a field key")
				}
			case "sale_status":
				if !slices.ContainsFunc(library.ShelfConfig.Regions, func(v types.ShelfOption) bool { return v.Key == c.Key }) {
					return nil, apperrors.NewBadRequestError("status column requires a configured region")
				}
			case "parameter":
				if !slices.ContainsFunc(library.EntitySchema.Fields, func(v types.EntityFieldDefinition) bool {
					return v.Key == c.Key && slices.Contains(v.ProductLineIDs, line.ID) && slices.Contains([]string{"text", "number", "boolean", "date", "select"}, v.Type) && !v.Archived
				}) {
					return nil, apperrors.NewBadRequestError("parameter column requires a supported field for its product line")
				}
			default:
				return nil, apperrors.NewBadRequestError("unknown import column field")
			}
		}
		for _, field := range []string{"platform:", "product:", "model:"} {
			if !fields[field] {
				return nil, apperrors.NewBadRequestError("platform, product and model columns are required")
			}
		}
	}
	for _, separator := range input.ModelSeparators {
		if separator == "" || len(separator) > 100 {
			return nil, apperrors.NewBadRequestError("model separators must be nonblank")
		}
	}
	for raw, status := range input.StatusValues {
		if strings.TrimSpace(raw) == "" {
			return nil, apperrors.NewBadRequestError("uncertain: blank regional cells need an explicit per-row decision")
		}
		if !slices.Contains([]string{"GA", "DEV", "Planning", "EOM", "待定"}, status.Status) {
			return nil, apperrors.NewBadRequestError("invalid mapped sale status")
		}
	}
	for color, meaning := range input.FillMeanings {
		if color == "" || !slices.Contains([]string{"ignore", "planned_eom", "uncertain", "GA", "DEV", "Planning", "EOM", "待定"}, meaning) {
			return nil, apperrors.NewBadRequestError("fill meaning must be ignore, planned_eom, uncertain or a sale status")
		}
	}
	old, err := s.shelfImportPlan(ctx, library.ID, input.Key)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if (err == nil && old.Revision != input.Revision) || (errors.Is(err, gorm.ErrRecordNotFound) && input.Revision != 0) {
		return nil, apperrors.NewConflictError("import plan revision changed")
	}
	now := time.Now()
	plan := &types.ShelfImportPlan{SpaceID: library.SpaceID, LibraryID: library.ID, Key: input.Key, Name: input.Name, FileKind: input.FileKind, Revision: input.Revision + 1, Definition: input.ShelfImportDefinition, CreatedAt: now, UpdatedAt: now}
	if err == nil {
		plan.CreatedAt = old.CreatedAt
	}
	if err := s.db.WithContext(ctx).Save(plan).Error; err != nil {
		return nil, err
	}
	return plan, NewOperationLogService(s.db).RecordTx(ctx, s.db, "save_import_plan", library.ID, old, plan)
}

func importColumnNumber(column string) int {
	n := 0
	for _, c := range column {
		n = n*26 + int(c-'A'+1)
	}
	return n
}

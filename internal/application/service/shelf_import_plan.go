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

// ShelfImportFields is shared by plan validation and library-wide shelf_guide.
// column_keys names the configured identity required by a keyed column.
type ShelfImportFields struct {
	Columns         []string          `json:"columns"`
	ColumnKeys      map[string]string `json:"column_keys,omitempty"`
	RequiredColumns []string          `json:"required_columns"`
	RequiredOneOf   [][]string        `json:"required_one_of,omitempty"`
	SheetFields     map[string]string `json:"sheet_fields"`
}

var shelfImportFields = map[string]ShelfImportFields{
	"product_map": {
		Columns:         []string{"platform", "product", "model", "description", "sale_status", "parameter"},
		ColumnKeys:      map[string]string{"sale_status": "region", "parameter": "product_line_field"},
		RequiredColumns: []string{"platform", "product", "model"},
		SheetFields:     map[string]string{"product_line_id": "required"},
	},
	"certification_progress": {
		Columns:         []string{"name", "owner", "standard", "year", "certification_object", "cell_part", "region", "agency", "progress", "start_date", "obtained_date", "issued_date", "validity", "number", "cloud_url", "sample_description"},
		RequiredColumns: []string{"owner", "standard", "certification_object", "region"},
		SheetFields:     map[string]string{"owner_kind": "required", "product_line_id": "optional"},
	},
	"certification_report": {
		Columns:         []string{"name", "owner", "standard", "year", "certification_object", "cell_part", "region", "agency", "progress", "start_date", "obtained_date", "issued_date", "validity", "number", "cloud_url", "sample_description", "model", "pack_model", "cluster_model", "container_model"},
		RequiredColumns: []string{"owner", "standard", "certification_object", "region"},
		SheetFields:     map[string]string{"owner_kind": "required", "product_line_id": "optional", "progress": "required", "module_type": "required_with_cell_part"},
	},
	"overseas_sales": {
		Columns:         []string{"owner", "standard", "certification_object", "cell_part", "region", "agency", "progress", "validity", "number", "cloud_url", "sale_status", "certification"},
		ColumnKeys:      map[string]string{"sale_status": "region", "certification": "standard"},
		RequiredColumns: []string{"owner"},
		RequiredOneOf:   [][]string{{"standard", "sale_status", "certification"}},
		SheetFields:     map[string]string{"owner_kind": "required", "product_line_id": "optional", "progress": "required_with_certification", "check_values": "required_with_certification"},
	},
	"atq": {
		Columns:         []string{"name", "name_zh", "name_en", "file_category", "stage_label", "due_stages", "owner", "priority", "source_list", "source_version", "market"},
		ColumnKeys:      map[string]string{"market": "market"},
		RequiredColumns: []string{"due_stages"},
		RequiredOneOf:   [][]string{{"name", "name_zh", "name_en"}},
		SheetFields:     map[string]string{"product_line_id": "required"},
	},
}

func validateShelfImportColumns(kind string, sheet types.ShelfImportSheet) error {
	schema := shelfImportFields[kind]
	fields, columns := map[string]bool{}, map[string]bool{}
	for _, c := range sheet.Columns {
		if !shelfImportColumn.MatchString(c.Column) || importColumnNumber(c.Column) > 16384 || columns[c.Column] || fields[c.Field+":"+c.Key] && !(kind == "atq" && c.Field == "market") {
			return apperrors.NewBadRequestError("import columns and mapped fields must be distinct")
		}
		columns[c.Column], fields[c.Field+":"+c.Key] = true, true
		_, keyed := schema.ColumnKeys[c.Field]
		if !slices.Contains(schema.Columns, c.Field) || keyed != (c.Key != "") {
			return apperrors.NewBadRequestError("unknown field or invalid key for this import file_kind")
		}
	}
	has := func(field string) bool {
		return slices.ContainsFunc(sheet.Columns, func(c types.ShelfImportColumn) bool { return c.Field == field })
	}
	for _, field := range schema.RequiredColumns {
		if !has(field) {
			return apperrors.NewBadRequestError("worksheet is missing required field " + field)
		}
	}
	for _, group := range schema.RequiredOneOf {
		if !slices.ContainsFunc(group, has) {
			return apperrors.NewBadRequestError("worksheet requires one of " + strings.Join(group, ", "))
		}
	}
	return nil
}

type ShelfImportPlanView struct {
	types.ShelfImportPlan
	LastFileName string                       `json:"last_file_name,omitempty"`
	LastFileDate *time.Time                   `json:"last_file_date,omitempty"`
	LastHeaders  map[string]map[string]string `json:"last_headers,omitempty"`
}

func (s *EntityService) ShelfImportPlans(ctx context.Context, libraryID string) ([]ShelfImportPlanView, error) {
	if _, err := s.shelfLibraryForRead(ctx, libraryID); err != nil {
		return nil, err
	}
	rows := []types.ShelfImportPlan{}
	spaceID := types.MustSpaceIDFromContext(ctx)
	if err := s.db.WithContext(ctx).Where("space_id=? AND library_id=?", spaceID, libraryID).Order("key").Find(&rows).Error; err != nil {
		return nil, err
	}
	var files []types.ShelfChangeset
	// A deferred piece shares its original batch. Reviewing it later must not
	// move that upload ahead of files that actually arrived more recently.
	batches := s.db.WithContext(ctx).Model(&types.ShelfChangeset{}).
		Select("DISTINCT ON (source->>'import_batch_id') source,created_at,id").
		Where("space_id=? AND library_id=? AND source->>'type'='import'", spaceID, libraryID).
		Order("source->>'import_batch_id',created_at,id")
	if err := s.db.WithContext(ctx).Table("(?) AS batches", batches).
		Select("DISTINCT ON (source->>'import_plan_key') source").
		Order("source->>'import_plan_key', created_at DESC, id DESC").Find(&files).Error; err != nil {
		return nil, err
	}
	latest := make(map[string]types.OperationSource, len(files))
	for _, file := range files {
		latest[file.Source.ImportPlanKey] = file.Source
	}
	result := make([]ShelfImportPlanView, 0, len(rows))
	for _, plan := range rows {
		source := latest[plan.Key]
		result = append(result, ShelfImportPlanView{ShelfImportPlan: plan, LastFileName: source.FileName, LastFileDate: source.FileDate, LastHeaders: source.ImportHeaders})
	}
	return result, nil
}

func (s *EntityService) shelfImportPlan(ctx context.Context, libraryID, key string) (*types.ShelfImportPlan, error) {
	var plan types.ShelfImportPlan
	err := s.db.WithContext(ctx).Where("space_id=? AND library_id=? AND key=?", types.MustSpaceIDFromContext(ctx), libraryID, key).First(&plan).Error
	return &plan, err
}

func (s *EntityService) writeShelfImportPlan(ctx context.Context, draft *types.ShelfChangeset, input types.ShelfImportPlanInput) (*types.ShelfImportPlan, error) {
	if !shelfImportKey.MatchString(input.Key) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 200 || shelfImportFields[input.FileKind].Columns == nil || len(input.Sheets) == 0 || len(input.Sheets) > 100 {
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
	for raw, value := range input.MarketValues {
		if input.FileKind != "atq" || strings.TrimSpace(raw) == "" || !slices.Contains([]string{"required", "optional", "not_needed"}, value) {
			return nil, apperrors.NewBadRequestError("market_values require nonblank ATQ source text and required, optional or not_needed")
		}
	}
	for _, sheet := range input.Sheets {
		if sheet.Worksheet == "" || worksheets[sheet.Worksheet] || sheet.HeaderRows < 0 || sheet.HeaderRows > 100 {
			return nil, apperrors.NewBadRequestError("invalid worksheet or header_rows")
		}
		worksheets[sheet.Worksheet] = true
		if sheet.Ignore {
			if sheet.Range != "" || sheet.Transpose {
				return nil, apperrors.NewBadRequestError("ignored worksheets take no range or transpose")
			}
			continue
		}
		if sheet.Range != "" {
			if _, err := parseImportRange(sheet.Range); err != nil {
				return nil, apperrors.NewBadRequestError(err.Error())
			}
		}
		if err := validateShelfImportColumns(input.FileKind, sheet); err != nil {
			return nil, err
		}
		schema := shelfImportFields[input.FileKind]
		if schema.SheetFields["product_line_id"] == "required" && sheet.ProductLineID == "" {
			return nil, apperrors.NewBadRequestError("worksheet requires a product_line_id")
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
		for _, c := range sheet.Columns {
			switch c.Field {
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

func (s *EntityService) validateRecordImportSheet(ctx context.Context, library *types.Library, kind string, sheet types.ShelfImportSheet) error {
	schema := shelfImportFields[kind]
	if schema.SheetFields["owner_kind"] == "required" && !slices.Contains([]string{"model", "part", "product", "platform"}, sheet.OwnerKind) {
		return apperrors.NewBadRequestError("record worksheet requires an explicit owner_kind")
	}
	if sheet.ModuleType != "" && !shelfOptionExists(library.ShelfConfig.ModuleTypes, sheet.ModuleType) {
		return apperrors.NewBadRequestError("worksheet module_type must be configured")
	}
	if schema.SheetFields["progress"] == "required" && strings.TrimSpace(sheet.Progress) == "" {
		return apperrors.NewBadRequestError("report worksheet requires a confirmed completed progress spelling")
	}
	if kind == "atq" {
		if sheet.ProductLineID == "" || sheet.ModuleType != "" {
			return apperrors.NewBadRequestError("ATQ worksheet requires a product_line_id and does not support module_type")
		}
	}
	if sheet.ProductLineID != "" {
		line, err := s.shelfEntity(ctx, library.ID, sheet.ProductLineID)
		if err != nil {
			return err
		}
		if line.Kind != "product_line" || line.ArchivedAt != nil {
			return apperrors.NewBadRequestError("worksheet requires an active product line")
		}
	}

	for raw := range sheet.CheckValues {
		if strings.TrimSpace(raw) == "" || schema.SheetFields["check_values"] == "" {
			return apperrors.NewBadRequestError("check_values require nonblank overseas flags")
		}
	}
	fields := map[string]bool{}
	for _, c := range sheet.Columns {
		fields[c.Field+":"] = true
		if kind == "overseas_sales" && c.Field == "certification" {
			standard, ok := library.ShelfConfig.Standard(c.Key)
			if !ok || standard.Key != c.Key || strings.TrimSpace(sheet.Progress) == "" || len(sheet.CheckValues) == 0 {
				return apperrors.NewBadRequestError("certification checks require a configured standard key, completed progress spelling and check_values")
			}
			continue
		}
		if kind == "atq" && c.Field == "market" {
			if !shelfOptionExists(library.ShelfConfig.Markets, c.Key) {
				return apperrors.NewBadRequestError("market column requires a configured market key")
			}
			continue
		}
		if kind == "overseas_sales" && c.Field == "sale_status" {
			if !shelfOptionExists(library.ShelfConfig.Regions, c.Key) {
				return apperrors.NewBadRequestError("sales check requires a configured region")
			}
			continue
		}
	}
	if kind == "overseas_sales" {
		metadata := slices.ContainsFunc(sheet.Columns, func(c types.ShelfImportColumn) bool {
			return slices.Contains([]string{"agency", "progress", "number", "validity", "cloud_url", "cell_part"}, c.Field)
		})
		if metadata && !fields["standard:"] {
			return apperrors.NewBadRequestError("certification metadata checks require a standard column")
		}
	}

	return nil
}

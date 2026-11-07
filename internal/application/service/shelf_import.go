package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

var importDatePattern = regexp.MustCompile(`([12][0-9]{3})[-._年]?([0-9]{2})[-._月]?([0-9]{2})日?`)

// ImportShelfWorkbook persists only a reviewable changeset, never the upload.
func (s *EntityService) ImportShelfWorkbook(ctx context.Context, libraryID, planKey, filename string, fileDate *time.Time, data []byte) (*types.ShelfChangeset, error) {
	if _, err := s.requireSpace(ctx); err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > shelfImportMaxBytes || strings.ToLower(filepath.Ext(filename)) != ".xlsx" {
		return nil, apperrors.NewBadRequestError("provide an xlsx file up to 32 MiB")
	}
	filename = filepath.Base(filename)
	if fileDate == nil {
		matches := importDatePattern.FindAllStringSubmatchIndex(filename, -1)
		if len(matches) == 1 && (matches[0][0] == 0 || filename[matches[0][0]-1] < '0' || filename[matches[0][0]-1] > '9') && (matches[0][1] == len(filename) || filename[matches[0][1]] < '0' || filename[matches[0][1]] > '9') {
			parts := importDatePattern.FindStringSubmatch(filename)
			parsed, err := time.Parse("2006-01-02", parts[1]+"-"+parts[2]+"-"+parts[3])
			if err == nil {
				fileDate = &parsed
			}
		}
	}
	if fileDate == nil || fileDate.IsZero() || fileDate.After(time.Now()) {
		return nil, apperrors.NewBadRequestError("file_date is required when the filename has no valid unambiguous date; provide a nonfuture RFC3339 time")
	}
	workbook, err := readImportWorkbook(data)
	if err != nil {
		return nil, apperrors.NewBadRequestError(err.Error())
	}
	var result *types.ShelfChangeset
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpace(tx, types.MustSpaceIDFromContext(ctx)); err != nil {
			return err
		}
		bound := s.shelfBound(tx)
		library, err := bound.shelfLibrary(ctx, tx, libraryID)
		if err != nil {
			return err
		}
		plan, err := bound.shelfImportPlan(ctx, libraryID, planKey)
		if err != nil {
			return managedWriteError(err)
		}
		hash := sha256.Sum256(data)
		source := types.OperationSource{Type: "import", FileDate: fileDate, FileName: filename, FileSHA256: hex.EncodeToString(hash[:]), ImportPlanKey: plan.Key, PlanRevision: plan.Revision}
		source.ImportBatchID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%d/%s/%s/%d/%s/%s", library.SpaceID, library.ID, plan.Key, plan.Revision, source.FileSHA256, fileDate.UTC().Format(time.RFC3339Nano)))).String()
		var existing []types.ShelfChangeset
		if err := tx.Where("space_id=? AND library_id=? AND source->>'import_batch_id'=?", library.SpaceID, library.ID, source.ImportBatchID).Order("created_at, id").Limit(1).Find(&existing).Error; err != nil {
			return err
		}
		if len(existing) > 0 {
			result = &existing[0]
			return nil
		}
		builder, err := bound.newImportMapBuilder(ctx, library, plan, *fileDate)
		if err != nil {
			return err
		}
		if err := builder.parse(workbook); err != nil {
			return err
		}
		if err := builder.disappeared(source.ImportBatchID); err != nil {
			return err
		}
		if len(builder.ops) == 0 {
			return apperrors.NewBadRequestError("workbook has no importable rows or conflicts")
		}
		result, err = bound.SubmitShelfChangeset(ctx, libraryID, types.ShelfChangesetInput{Source: source, Operations: builder.ops})
		if err != nil {
			return err
		}
		result, err = bound.ShelfChangeset(ctx, libraryID, result.ID)
		return err
	})
	return result, managedWriteError(err)
}

type importMapNode struct {
	Ref, Parent string
	Entity      types.Entity
}
type importMapBuilder struct {
	ctx           context.Context
	service       *EntityService
	library       *types.Library
	plan          *types.ShelfImportPlan
	at            time.Time
	entities      []types.Entity
	parents       map[string]types.Relation
	nodes         map[string]importMapNode
	seenModels    map[string]bool
	present       map[string]bool
	skippedSheets map[string]bool
	ops           []types.ShelfOperation
	location      *types.ShelfImportLocation
}

func (s *EntityService) newImportMapBuilder(ctx context.Context, library *types.Library, plan *types.ShelfImportPlan, at time.Time) (*importMapBuilder, error) {
	b := &importMapBuilder{ctx: ctx, service: s, library: library, plan: plan, at: at, parents: map[string]types.Relation{}, nodes: map[string]importMapNode{}, seenModels: map[string]bool{}, present: map[string]bool{}, skippedSheets: map[string]bool{}}
	if err := s.db.WithContext(ctx).Where("space_id=? AND library_id=? AND archived_at IS NULL AND merged_into_id=''", library.SpaceID, library.ID).Order("id").Find(&b.entities).Error; err != nil {
		return nil, err
	}
	if err := s.loadAliases(ctx, library.SpaceID, b.entities); err != nil {
		return nil, err
	}
	models := []*types.Entity{}
	for i := range b.entities {
		if b.entities[i].Kind == "model" {
			models = append(models, &b.entities[i])
		}
	}
	if err := hydrateManagedEntityAttributes(ctx, s.db, models, at); err != nil {
		return nil, err
	}
	var relations []types.Relation
	if err := s.db.WithContext(ctx).Where("space_id=? AND library_id=? AND relation_type='belongs_to' AND valid_to IS NULL", library.SpaceID, library.ID).Find(&relations).Error; err != nil {
		return nil, err
	}
	for _, r := range relations {
		b.parents[r.SubjectID] = r
	}
	return b, nil
}

func (b *importMapBuilder) append(op types.ShelfOperation) string {
	op.ID = fmt.Sprintf("import_%04d", len(b.ops)+1)
	op.ImportLocation = b.location
	b.ops = append(b.ops, op)
	return "$" + op.ID
}
func (b *importMapBuilder) issue(kind, message string, details any) {
	b.append(types.ShelfOperation{Type: "create_entity", Entity: &types.ManagedEntityInput{Kind: "model"}, Issues: []types.ShelfConflict{{Type: kind, Message: message, Details: details}}})
}
func (b *importMapBuilder) node(kind, name, parent string) (importMapNode, error) {
	key := kind + ":" + normalizeEntityName(name)
	if node, ok := b.nodes[key]; ok {
		if node.Parent != parent {
			return node, fmt.Errorf("same %s name belongs to different parents; choose identity explicitly", kind)
		}
		return node, nil
	}
	node := importMapNode{Parent: parent}
	for _, e := range b.entities {
		if e.Kind != kind {
			continue
		}
		for _, alias := range append([]string{e.CanonicalName, e.Code}, e.Aliases...) {
			if alias != "" && normalizeEntityName(alias) == normalizeEntityName(name) {
				if node.Ref != "" && node.Ref != e.ID {
					return node, fmt.Errorf("name has multiple candidates")
				}
				node.Ref = e.ID
				node.Entity = e
			}
		}
	}
	if node.Ref == "" {
		node.Entity = types.Entity{Kind: kind, CanonicalName: name, Revision: 1}
		node.Ref = b.append(types.ShelfOperation{Type: "create_entity", Entity: &types.ManagedEntityInput{Kind: kind, Name: name}})
	}
	if relation, exists := b.parents[node.Ref]; exists {
		if relation.ObjectID != parent || relation.ValidFrom.After(b.at) {
			return node, fmt.Errorf("existing %s parent differs at file date; choose identity or hierarchy explicitly", kind)
		}
	} else {
		b.append(types.ShelfOperation{Type: "save_relation", Relation: &types.ManagedRelationInput{SubjectID: node.Ref, ObjectID: parent, RelationType: "belongs_to"}})
	}
	b.nodes[key] = node
	return node, nil
}

func (b *importMapBuilder) parse(workbook *importWorkbookData) error {
	mapped := map[string]bool{}
	for _, mapping := range b.plan.Definition.Sheets {
		mapped[mapping.Worksheet] = true
		if mapping.Ignore {
			b.skippedSheets[mapping.Worksheet] = true
			continue
		}
		b.location = &types.ShelfImportLocation{Worksheet: mapping.Worksheet, Cells: []string{}}
		sheet, exists := workbook.Sheets[mapping.Worksheet]
		if !exists {
			b.skippedSheets[mapping.Worksheet] = true
			b.issue("unparseable", "configured worksheet is missing", nil)
			continue
		}
		if sheet.ConditionalFormatting {
			b.skippedSheets[mapping.Worksheet] = true
			b.issue("uncertain", "conditional formatting requires explicit values and fills", nil)
			continue
		}
		for _, row := range sheet.Rows {
			if len(b.ops) > 500 {
				return apperrors.NewBadRequestError("import exceeds 500 changeset operations; split the workbook")
			}
			if row <= mapping.HeaderRows {
				continue
			}
			cells := map[string]importCell{}
			refs := []string{}
			nonempty := false
			values := map[string]string{}
			for _, column := range mapping.Columns {
				ref := column.Column + strconv.Itoa(row)
				cell := sheet.cell(ref)
				cells[column.Field+":"+column.Key] = cell
				refs = append(refs, ref)
				values[ref] = cell.Value
				if cell.Formula != nil {
					values[ref+".formula"] = *cell.Formula
				}
				nonempty = nonempty || cell.Value != "" || cell.Formula != nil
			}
			if !nonempty {
				continue
			}
			b.location = &types.ShelfImportLocation{Worksheet: mapping.Worksheet, Cells: refs}
			model := cells["model:"].Value
			models := []string{model}
			for _, separator := range b.plan.Definition.ModelSeparators {
				next := []string{}
				for _, name := range models {
					next = append(next, strings.Split(name, separator)...)
				}
				models = next
			}
			for _, name := range models {
				b.present[normalizeEntityName(name)] = true
			}
			missing := false
			for _, field := range []string{"platform:", "product:", "model:"} {
				raw := cells[field].Value
				missing = missing || strings.TrimSpace(raw) == "" || slices.Contains(b.plan.Definition.MissingValues, raw)
			}
			for _, name := range models {
				missing = missing || strings.TrimSpace(name) == "" || slices.Contains(b.plan.Definition.MissingValues, name)
			}
			if missing {
				kind := "required_missing"
				if model == "" && cells["product:"].Value == "" {
					kind = "uncertain"
				}
				b.issue(kind, "row has missing identity fields or is a group title; submitter must resolve", values)
				continue
			}
			for _, name := range models {
				if len(b.ops) > 500 {
					return apperrors.NewBadRequestError("import exceeds 500 changeset operations; split the workbook")
				}
				if b.seenModels[normalizeEntityName(name)] {
					b.issue("uncertain", "model appears in multiple rows; submitter must decide whether to merge", values)
					continue
				}
				b.seenModels[normalizeEntityName(name)] = true
				start := len(b.ops)
				saved := map[string]importMapNode{}
				for key, node := range b.nodes {
					saved[key] = node
				}
				err := b.modelRow(mapping, cells, name)
				if err != nil {
					b.ops = b.ops[:start]
					b.nodes = saved
					kind := "unparseable"
					if strings.HasPrefix(err.Error(), "uncertain:") {
						kind = "uncertain"
					}
					b.issue(kind, err.Error(), values)
				}
			}
		}
	}
	names := []string{}
	for name := range workbook.Sheets {
		if !mapped[name] {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		b.location = &types.ShelfImportLocation{Worksheet: name, Cells: []string{}}
		b.issue("uncertain", "worksheet has no mapping; explicitly map or ignore it", nil)
	}
	if len(b.ops) > 500 {
		return apperrors.NewBadRequestError("import exceeds 500 changeset operations; split the workbook")
	}
	return nil
}

func (b *importMapBuilder) modelRow(mapping types.ShelfImportSheet, cells map[string]importCell, name string) error {
	for _, cell := range cells {
		if cell.Formula != nil || cell.Type == "e" || cell.FormattedNumber {
			return fmt.Errorf("formula, error or formatted numeric cells require explicit values")
		}
	}
	platform, err := b.node("platform", cells["platform:"].Value, mapping.ProductLineID)
	if err != nil {
		return fmt.Errorf("uncertain: %w", err)
	}
	product, err := b.node("product", cells["product:"].Value, platform.Ref)
	if err != nil {
		return fmt.Errorf("uncertain: %w", err)
	}
	model, err := b.node("model", name, product.Ref)
	if err != nil {
		return fmt.Errorf("uncertain: %w", err)
	}
	if b.seenModels["entity:"+model.Ref] {
		return fmt.Errorf("uncertain: multiple rows resolve to the same model; submitter must decide whether to merge")
	}
	attrs := map[string]any{}
	for key, value := range model.Entity.Attributes {
		attrs[key] = value
	}
	statuses := []types.ShelfSaleStatus{}
	if raw, exists := attrs["sale_statuses"]; exists {
		if err := types.DecodeShelfValue(raw, &statuses); err != nil {
			return err
		}
	}
	planned := false
	for _, column := range mapping.Columns {
		cell := cells[column.Field+":"+column.Key]
		if cell.Fill != "" {
			meaning := b.plan.Definition.FillMeanings[cell.Fill]
			if meaning == "" || meaning == "uncertain" {
				return fmt.Errorf("uncertain: cell fill %s requires a confirmed meaning", cell.Fill)
			}
			if slices.Contains([]string{"GA", "DEV", "Planning", "EOM", "待定"}, meaning) && column.Field != "sale_status" {
				return fmt.Errorf("uncertain: a status color on an identity or parameter cell needs an explicit interpretation")
			}
			if meaning == "planned_eom" && column.Field != "sale_status" {
				planned = true
			}
		}
	}
	for _, column := range mapping.Columns {
		cell := cells[column.Field+":"+column.Key]
		switch column.Field {
		case "sale_status":
			mapped, exists := b.plan.Definition.StatusValues[cell.Value]
			if !exists {
				if cell.Value != "" && !slices.Contains([]string{"GA", "DEV", "Planning", "EOM", "待定"}, cell.Value) {
					return fmt.Errorf("sale status %q has no mapping", cell.Value)
				}
				mapped.Status = cell.Value
			}
			meaning := b.plan.Definition.FillMeanings[cell.Fill]
			if slices.Contains([]string{"GA", "DEV", "Planning", "EOM", "待定"}, meaning) && mapped.Status != "" && mapped.Status != meaning {
				return fmt.Errorf("uncertain: status color contradicts regional text")
			}
			statuses = slices.DeleteFunc(statuses, func(v types.ShelfSaleStatus) bool { return v.Region == column.Key })
			statuses = append(statuses, types.ShelfSaleStatus{Region: column.Key, Status: mapped.Status, Notes: mapped.Notes, Raw: cell.Value, PlannedEOM: planned || b.plan.Definition.FillMeanings[cell.Fill] == "planned_eom"})
		case "parameter":
			if cell.Value == "" {
				continue
			}
			var field types.EntityFieldDefinition
			for _, candidate := range b.library.EntitySchema.Fields {
				if candidate.Key == column.Key {
					field = candidate
				}
			}
			value, err := importParameter(field, cell.Value)
			if err != nil {
				return err
			}
			attrs[column.Key] = []types.ShelfParameterValue{{Value: value}}
		}
	}
	if len(statuses) > 0 {
		attrs["sale_statuses"] = statuses
	}
	patch := types.ManagedEntityPatch{Revision: model.Entity.Revision, Attributes: &attrs}
	if description, ok := cells["description:"]; ok {
		patch.Description = &description.Value
	}
	b.append(types.ShelfOperation{Type: "update_entity", EntityID: model.Ref, Patch: &patch})
	b.seenModels["entity:"+model.Ref] = true
	return nil
}

func importParameter(field types.EntityFieldDefinition, raw string) (any, error) {
	switch field.Type {
	case "number":
		for _, separator := range []string{"~", "～"} {
			if parts := strings.Split(raw, separator); len(parts) == 2 {
				min, e1 := strconv.ParseFloat(parts[0], 64)
				max, e2 := strconv.ParseFloat(parts[1], 64)
				if e1 == nil && e2 == nil && !math.IsInf(min, 0) && !math.IsInf(max, 0) && !math.IsNaN(min) && !math.IsNaN(max) && min <= max {
					return map[string]any{"min": min, "max": max}, nil
				}
				return nil, fmt.Errorf("invalid numeric range %q", raw)
			}
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("invalid numeric parameter %q", raw)
		}
		return value, nil
	case "boolean":
		if raw == "true" || raw == "1" {
			return true, nil
		}
		if raw == "false" || raw == "0" {
			return false, nil
		}
		return nil, fmt.Errorf("invalid boolean parameter %q", raw)
	case "date":
		if _, err := time.Parse("2006-01-02", raw); err != nil {
			return nil, fmt.Errorf("invalid date parameter %q", raw)
		}
	}
	return raw, nil
}

func (b *importMapBuilder) disappeared(batchID string) error {
	// Compare the newest file's complete batch, including deferred pieces. A
	// later confirmation of an older piece does not make that an updated file.
	var previous struct{ BatchID string }
	err := b.service.db.WithContext(b.ctx).Model(&types.ShelfChangeset{}).
		Select("source->>'import_batch_id' AS batch_id").
		Where("space_id=? AND library_id=? AND status='confirmed' AND source->>'import_plan_key'=? AND source->>'import_batch_id'<>?", b.library.SpaceID, b.library.ID, b.plan.Key, batchID).
		Group("source->>'import_batch_id'").Order("min(created_at) DESC, source->>'import_batch_id'").Limit(1).Scan(&previous).Error
	if err != nil {
		return err
	}
	if previous.BatchID == "" {
		return nil
	}

	var parts []types.ShelfChangeset
	if err := b.service.db.WithContext(b.ctx).Where("space_id=? AND library_id=? AND status='confirmed' AND source->>'import_batch_id'=?", b.library.SpaceID, b.library.ID, previous.BatchID).Order("created_at, id").Find(&parts).Error; err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, part := range parts {
		for _, item := range part.Items {
			if item.Operation.ImportLocation != nil && b.skippedSheets[item.Operation.ImportLocation.Worksheet] {
				continue
			}
			if item.Decision == nil {
				continue
			}
			id := shelfReference(&part, item.EntityID)
			if item.Decision.Action == "same_entity" {
				id = item.Decision.TargetEntityID
			}
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			for _, entity := range b.entities {
				if entity.ID != id || entity.Kind != "model" {
					continue
				}
				present := false
				for _, name := range append([]string{entity.CanonicalName, entity.Code}, entity.Aliases...) {
					present = present || b.present[normalizeEntityName(name)]
				}
				if !present {
					b.location = item.Operation.ImportLocation
					b.append(types.ShelfOperation{Type: "missing_entity", EntityID: entity.ID, Revision: entity.Revision})
				}
			}
		}
	}
	return nil
}
